export type CartesiaTranscriptionSocketConfig = {
  token: string;
  expiresAt: string;
  websocketUrl: string;
  model: string;
  encoding: string;
  sampleRate: number;
};

type SocketFactory = (url: string) => WebSocket;

export type CartesiaTranscriptSnapshot = {
  turnSeq: number;
  finalText: string;
  interimText: string;
  /** Latest provider delta for the temporary on-screen subtitle. */
  captionText: string;
};

type TranscriptListener = (snapshot: CartesiaTranscriptSnapshot) => void;

type Finalization = {
  turnSeq: number;
  text: string;
  interimText: string;
  resolve: (text: string) => void;
  reject: (error: Error) => void;
};

type QueuedFinalization = Finalization & {
  audio: ArrayBuffer[];
};

export type RealtimeFailureReason =
  | "connect_timeout"
  | "connect_failed"
  | "finalize_timeout"
  | "transport_error"
  | "unexpected_close"
  | "provider_error"
  | "audio_send_failed"
  | "unknown";

export class RealtimeTranscriptionError extends Error {
  constructor(message: string, readonly reason: RealtimeFailureReason, readonly closeCode?: number) {
    super(message || "Realtime transcription failed.");
    this.name = "RealtimeTranscriptionError";
  }
}

const socketError = (message: string, reason: RealtimeFailureReason = "unknown", closeCode?: number): RealtimeTranscriptionError =>
  new RealtimeTranscriptionError(message, reason, closeCode);

const copyPCM = (pcm: Int16Array): ArrayBuffer => {
  const copy = new Int16Array(pcm.length);
  copy.set(pcm);
  return copy.buffer;
};

export const buildCartesiaWebSocketURL = (config: CartesiaTranscriptionSocketConfig): string => {
  const url = new URL(config.websocketUrl);
  if (url.protocol !== "wss:" && url.protocol !== "ws:") {
    throw new Error("The transcription service returned an invalid WebSocket URL.");
  }
  url.searchParams.set("access_token", config.token);
  url.searchParams.set("model", config.model);
  url.searchParams.set("encoding", config.encoding);
  url.searchParams.set("sample_rate", String(config.sampleRate));
  return url.toString();
};

export class CartesiaRealtimeTranscriber {
  private active: Finalization | null = null;
  private queued: QueuedFinalization[] = [];
  private bufferedAudio: ArrayBuffer[] = [];
  private currentText = "";
  private currentInterimText = "";
  private currentTurnSeq: number | null = null;
  private failure: Error | null = null;
  private finalizeTimer: ReturnType<typeof setTimeout> | null = null;
  private closeSent = false;
  private closed = false;
  private readonly opened: Promise<void>;
  private resolveOpened: (() => void) | null = null;
  private rejectOpened: ((error: Error) => void) | null = null;

  private constructor(
    private readonly socket: WebSocket,
    private readonly onFailure?: (error: Error) => void,
    private readonly onTranscript?: TranscriptListener,
    private readonly finalizeTimeoutMs = 5000,
  ) {
    this.opened = new Promise<void>((resolve, reject) => {
      this.resolveOpened = resolve;
      this.rejectOpened = reject;
    });
    socket.binaryType = "arraybuffer";
    socket.addEventListener("open", this.handleOpen);
    socket.addEventListener("message", this.handleMessage);
    socket.addEventListener("error", this.handleTransportError);
    socket.addEventListener("close", this.handleClose);
  }

  static async connect(
    config: CartesiaTranscriptionSocketConfig,
    createSocket: SocketFactory = (url) => new WebSocket(url),
    onFailure?: (error: Error) => void,
    onTranscript?: TranscriptListener,
    timeoutMs = 5000,
    finalizeTimeoutMs = 5000,
  ): Promise<CartesiaRealtimeTranscriber> {
    if (config.encoding !== "pcm_s16le" || config.sampleRate !== 16000) {
      throw new Error("The transcription service returned an unsupported audio format.");
    }
    const transcriber = new CartesiaRealtimeTranscriber(
      createSocket(buildCartesiaWebSocketURL(config)),
      onFailure,
      onTranscript,
      finalizeTimeoutMs,
    );
    let timer: ReturnType<typeof setTimeout> | null = null;
    try {
      await Promise.race([
        transcriber.opened,
        new Promise<never>((_, reject) => {
          timer = setTimeout(() => reject(socketError("Realtime transcription connection timed out.", "connect_timeout")), timeoutMs);
        }),
      ]);
      return transcriber;
    } catch (error) {
      const failure = error instanceof RealtimeTranscriptionError
        ? error
        : socketError(error instanceof Error ? error.message : "Realtime transcription connection failed.", "connect_failed");
      transcriber.fail(failure);
      transcriber.socket.close();
      throw failure;
    } finally {
      if (timer) clearTimeout(timer);
    }
  }

  sendPCM(pcm: Int16Array): void {
    if (this.failure || this.closed || this.closeSent || pcm.length === 0) return;
    const audio = copyPCM(pcm);
    if (this.active) {
      this.bufferedAudio.push(audio);
      return;
    }
    this.sendRaw(audio);
  }

  beginTurn(turnSeq: number): void {
    if (!Number.isSafeInteger(turnSeq) || turnSeq < 1) {
      throw new Error("The live transcript requires a valid interview turn.");
    }
    this.currentTurnSeq = turnSeq;
    this.currentText = "";
    this.currentInterimText = "";
    this.emitCurrentTranscript();
  }

  finalizeTurn(turnSeq = this.currentTurnSeq): Promise<string> {
    if (this.failure) return Promise.reject(this.failure);
    if (this.closed || this.closeSent || this.socket.readyState !== 1) {
      return Promise.reject(socketError("Realtime transcription is unavailable."));
    }
    if (turnSeq === null || !Number.isSafeInteger(turnSeq) || turnSeq < 1 || turnSeq !== this.currentTurnSeq) {
      return Promise.reject(socketError("Realtime transcription lost the interview turn boundary."));
    }
    let resolveFinalization!: (text: string) => void;
    let rejectFinalization!: (error: Error) => void;
    const promise = new Promise<string>((resolve, reject) => {
      resolveFinalization = resolve;
      rejectFinalization = reject;
    });
    // Attach a rejection handler immediately because the caller may not consume
    // this promise until its matching interview advance request completes.
    void promise.catch(() => undefined);
    const finalization: Finalization = {
      turnSeq,
      text: this.currentText,
      interimText: this.currentInterimText,
      resolve: resolveFinalization,
      reject: rejectFinalization,
    };
    this.currentText = "";
    this.currentInterimText = "";
    this.currentTurnSeq = null;
    if (this.active) {
      this.queued.push({ ...finalization, audio: this.bufferedAudio });
      this.bufferedAudio = [];
    } else {
      this.active = finalization;
      this.startFinalization();
    }
    return promise;
  }

  async close(): Promise<void> {
    if (this.closeSent || this.closed) return;
    try {
      await this.opened;
    } catch {
      return;
    }
    if (this.active || this.queued.length > 0) {
      const drained = await new Promise<boolean>((resolve) => {
        const timer = setTimeout(() => resolve(false), 5000);
        const check = () => {
          if (!this.active && this.queued.length === 0) {
            clearTimeout(timer);
            resolve(true);
          }
          else setTimeout(check, 10);
        };
        check();
      });
      if (!drained) {
        this.fail(socketError("Realtime transcription did not finish the last answer.", "finalize_timeout"));
        this.socket.close();
        return;
      }
    }
    if (this.failure || this.closed || this.socket.readyState !== 1) return;
    this.flushBufferedAudio();
    if (this.failure) return;
    this.closeSent = true;
    try {
      this.socket.send("close");
    } catch {
      this.fail(socketError("Realtime transcription close could not be sent.", "transport_error"));
      this.socket.close();
    }
  }

  private readonly handleOpen = () => {
    this.resolveOpened?.();
    this.resolveOpened = null;
    this.rejectOpened = null;
  };

  private readonly handleMessage = (event: MessageEvent) => {
    if (typeof event.data !== "string") return;
    let message: Record<string, unknown>;
    try {
      message = JSON.parse(event.data) as Record<string, unknown>;
    } catch {
      return;
    }
    if (message.type === "transcript") {
      if (typeof message.text !== "string") return;
      if (this.active) {
        if (message.is_final === true) {
          this.active.text += message.text;
          this.active.interimText = "";
        } else {
          this.active.interimText = message.text;
        }
        this.emitTranscript(this.active, message.text);
      } else if (this.currentTurnSeq !== null) {
        if (message.is_final === true) {
          this.currentText += message.text;
          this.currentInterimText = "";
        } else {
          this.currentInterimText = message.text;
        }
        this.emitCurrentTranscript(message.text);
      }
      return;
    }
    if (message.type === "flush_done") {
      this.finishActiveTurn();
      return;
    }
    if (message.type === "error") {
      const detail = typeof message.message === "string"
        ? message.message
        : typeof message.title === "string" ? message.title : "Realtime transcription failed.";
      this.fail(socketError(detail, "provider_error"));
      return;
    }
    if (message.type === "done") {
      this.closed = true;
    }
  };

  private readonly handleTransportError = () => {
    this.fail(socketError("Realtime transcription connection failed.", "transport_error"));
  };

  private readonly handleClose = (event: CloseEvent) => {
    this.closed = true;
    if (!this.closeSent && !this.failure) {
      this.fail(socketError("Realtime transcription connection closed unexpectedly.", "unexpected_close", event.code));
    }
  };

  private finishActiveTurn(): void {
    const completed = this.active;
    if (!completed) return;
    this.clearFinalizeTimeout();
    this.active = null;
    completed.interimText = "";
    this.emitTranscript(completed);
    completed.resolve(completed.text);
    const next = this.queued.shift();
    if (next) {
      const { audio: _audio, ...finalization } = next;
      this.active = finalization;
      for (const audio of next.audio) {
        this.sendRaw(audio);
        if (this.failure) return;
      }
      this.startFinalization();
      return;
    }
    this.flushBufferedAudio();
  }

  private flushBufferedAudio(): void {
    const audio = this.bufferedAudio;
    this.bufferedAudio = [];
    for (const chunk of audio) this.sendRaw(chunk);
  }

  private sendRaw(audio: ArrayBuffer): void {
    if (this.failure || this.closed || this.socket.readyState !== 1) return;
    try {
      this.socket.send(audio);
    } catch {
      this.fail(socketError("Realtime transcription audio could not be sent.", "audio_send_failed"));
    }
  }

  private startFinalization(): void {
    try {
      this.socket.send("finalize");
      this.armFinalizeTimeout();
    } catch {
      this.fail(socketError("Realtime transcription finalize could not be sent.", "transport_error"));
    }
  }

  private emitCurrentTranscript(captionText = ""): void {
    if (this.currentTurnSeq === null) return;
    this.onTranscript?.({
      turnSeq: this.currentTurnSeq,
      finalText: this.currentText,
      interimText: this.currentInterimText,
      captionText,
    });
  }

  private emitTranscript(
    transcript: Pick<Finalization, "turnSeq" | "text" | "interimText">,
    captionText = "",
  ): void {
    this.onTranscript?.({
      turnSeq: transcript.turnSeq,
      finalText: transcript.text,
      interimText: transcript.interimText,
      captionText,
    });
  }

  private armFinalizeTimeout(): void {
    this.clearFinalizeTimeout();
    this.finalizeTimer = setTimeout(() => {
      if (!this.active || this.failure) return;
      this.fail(socketError("Realtime transcription did not finish the answer.", "finalize_timeout"));
      this.socket.close();
    }, this.finalizeTimeoutMs);
  }

  private clearFinalizeTimeout(): void {
    if (this.finalizeTimer !== null) clearTimeout(this.finalizeTimer);
    this.finalizeTimer = null;
  }

  private fail(error: Error): void {
    if (this.failure) return;
    this.failure = error;
    this.clearFinalizeTimeout();
    this.rejectOpened?.(error);
    this.resolveOpened = null;
    this.rejectOpened = null;
    this.active?.reject(error);
    this.active = null;
    for (const pending of this.queued) pending.reject(error);
    this.queued = [];
    this.bufferedAudio = [];
    this.currentText = "";
    this.currentInterimText = "";
    this.currentTurnSeq = null;
    this.onFailure?.(error);
  }
}
