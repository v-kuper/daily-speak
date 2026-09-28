/** Captures independent WAV answers while the normal MediaRecorder keeps the
 * complete interview as one uninterrupted audio file. */
export type CapturedInterviewTurn = {
  blob: Blob | null;
  transcript: Promise<string> | null;
};

type PendingBoundary = {
  resolve: (turn: CapturedInterviewTurn) => void;
  reject: (error: Error) => void;
  delayTimer: number | null;
  timeoutTimer: number;
  finalizeTranscript?: () => Promise<string>;
};

const DEFAULT_BOUNDARY_DELAY_MS = 2000;
const DEFAULT_BOUNDARY_TIMEOUT_MS = 6000;

/**
 * Reject low-level room noise and isolated microphone spikes while still
 * accepting a short spoken answer. Browser voice processing normally leaves
 * silence well below these levels, including when realtime STT is offline.
 */
export const pcmHasSpeechActivity = (pcm: Int16Array): boolean => {
  if (pcm.length === 0) return false;
  let squareSum = 0;
  let peak = 0;
  let activeSamples = 0;
  for (const sample of pcm) {
    const amplitude = Math.abs(sample);
    squareSum += amplitude * amplitude;
    peak = Math.max(peak, amplitude);
    if (amplitude >= 500) activeSamples += 1;
  }
  const rms = Math.sqrt(squareSum / pcm.length);
  const requiredActiveSamples = Math.min(80, Math.max(1, Math.ceil(pcm.length * 0.03)));
  return rms >= 300 && peak >= 900 && activeSamples >= requiredActiveSamples;
};

export class InterviewTurnCapture {
  private chunks: Int16Array[] = [];
  private pending = new Map<number, PendingBoundary>();
  private nextBoundary = 1;
  private closed = false;
  private terminalError: Error | null = null;
  private onPCM: ((pcm: Int16Array) => void) | null = null;
  private speechDetected = false;

  private readonly handleProcessorError = () => {
    this.fail(new Error("Live answer capture stopped unexpectedly. Please retry the recording."));
  };

  private constructor(
    private readonly context: AudioContext,
    private readonly source: MediaStreamAudioSourceNode,
    private readonly processor: AudioWorkletNode,
    private readonly sink: GainNode,
    private readonly onFailure?: (error: Error) => void,
    private readonly onSpeechActivity?: (active: boolean) => void,
    private readonly boundaryDelayMs = DEFAULT_BOUNDARY_DELAY_MS,
    private readonly boundaryTimeoutMs = DEFAULT_BOUNDARY_TIMEOUT_MS,
  ) {
    processor.addEventListener?.("processorerror", this.handleProcessorError);
    processor.port.onmessage = (event: MessageEvent<{ type?: string; id?: number; pcm?: Int16Array }>) => {
      if (event.data?.type === "samples" && event.data.pcm instanceof Int16Array) {
        this.chunks.push(event.data.pcm);
        if (!this.speechDetected && pcmHasSpeechActivity(event.data.pcm)) {
          this.speechDetected = true;
          try { this.onSpeechActivity?.(true); } catch { /* UI activity reporting must not interrupt capture. */ }
        }
        this.onPCM?.(event.data.pcm);
        return;
      }
      if (event.data?.type !== "boundary" || typeof event.data.id !== "number") return;
      const request = this.pending.get(event.data.id);
      // A stale or foreign boundary must never partition the current answer.
      if (!request) return;
      // Port messages are ordered. Keep waiting for the actual boundary so a
      // delayed main thread cannot discard or mix either answer partition.
      const chunks = this.chunks;
      this.chunks = [];
      this.pending.delete(event.data.id);
      if (request.delayTimer !== null) window.clearTimeout(request.delayTimer);
      window.clearTimeout(request.timeoutTimer);
      if (this.speechDetected) {
        this.speechDetected = false;
        try { this.onSpeechActivity?.(false); } catch { /* UI activity reporting must not interrupt capture. */ }
      }
      let transcript: Promise<string> | null = null;
      if (request.finalizeTranscript) {
        try {
          transcript = request.finalizeTranscript();
          void transcript.catch(() => undefined);
        } catch (error) {
          transcript = Promise.reject(error);
          void transcript.catch(() => undefined);
        }
      }
      request.resolve({
        blob: chunks.length ? encodeWav(chunks, Math.min(16000, this.context.sampleRate)) : null,
        transcript,
      });
    };
  }

  static async start(
    stream: MediaStream,
    onFailure?: (error: Error) => void,
    onSpeechActivity?: (active: boolean) => void,
  ): Promise<InterviewTurnCapture> {
    if (typeof AudioContext === "undefined") {
      throw new Error("Live transcription is unavailable in this browser.");
    }
    const context = new AudioContext();
    try {
      if (!context.audioWorklet) throw new Error("Live transcription is unavailable in this browser.");
      await context.audioWorklet.addModule("/interview-capture-worklet.js");
      const source = context.createMediaStreamSource(stream);
      const processor = new AudioWorkletNode(context, "interview-capture", {
        numberOfInputs: 1,
        numberOfOutputs: 1,
        outputChannelCount: [1],
      });
      const sink = context.createGain();
      sink.gain.value = 0;
      source.connect(processor);
      processor.connect(sink);
      sink.connect(context.destination);
      await context.resume();
      if (context.state !== "running") throw new Error("Live transcription could not start.");
      return new InterviewTurnCapture(context, source, processor, sink, onFailure, onSpeechActivity);
    } catch (error) {
      await context.close();
      throw error;
    }
  }

  setPCMListener(listener: ((pcm: Int16Array) => void) | null): void {
    this.onPCM = listener;
  }

  hasSpeechActivity(): boolean {
    return this.speechDetected;
  }

  closeTurn(
    finalizeTranscript?: () => Promise<string>,
    onDelayed?: () => void,
  ): Promise<CapturedInterviewTurn> {
    if (this.closed) {
      return this.terminalError
        ? Promise.reject(this.terminalError)
        : Promise.resolve({ blob: null, transcript: null });
    }
    if (this.pending.size > 0) {
      const error = new Error("Answer capture received overlapping turn boundaries.");
      this.fail(error);
      return Promise.reject(error);
    }
    const id = this.nextBoundary++;
    return new Promise((resolve, reject) => {
      const delayTimer = onDelayed ? window.setTimeout(() => {
        if (!this.pending.has(id)) return;
        try { onDelayed(); } catch { /* A UI notice must never lose captured audio. */ }
      }, this.boundaryDelayMs) : null;
      const timeoutTimer = window.setTimeout(() => {
        if (!this.pending.has(id)) return;
        this.fail(new Error("Answer capture did not finish its boundary."));
      }, this.boundaryTimeoutMs);
      this.pending.set(id, { resolve, reject, delayTimer, timeoutTimer, finalizeTranscript });
      try {
        this.processor.port.postMessage({ type: "boundary", id });
      } catch (error) {
        this.fail(error instanceof Error ? error : new Error("Answer capture could not request its boundary."));
      }
    });
  }

  async stop(
    finalizeTranscript?: () => Promise<string>,
    onDelayed?: () => void,
  ): Promise<CapturedInterviewTurn> {
    if (this.closed) {
      if (this.terminalError) throw this.terminalError;
      return { blob: null, transcript: null };
    }
    let last: CapturedInterviewTurn = { blob: null, transcript: null };
    try {
      if (this.context.state === "suspended") {
        // Resume is best-effort: browsers may leave this promise pending while
        // the document is hidden. Post the boundary immediately so its own
        // timeout still provides a bounded terminal outcome.
        void this.context.resume().catch((error: unknown) => {
          this.fail(error instanceof Error ? error : new Error("Answer capture could not resume."));
        });
      }
      last = await this.closeTurn(finalizeTranscript, onDelayed);
    } catch (error) {
      const failure = this.terminalError
        ?? (error instanceof Error ? error : new Error("Answer capture could not finish."));
      this.fail(failure);
      throw failure;
    } finally {
      this.shutdown(new Error("Answer capture stopped."));
      await this.context.close().catch(() => undefined);
    }
    return last;
  }

  private fail(error: Error): void {
    if (this.terminalError || this.closed) return;
    this.terminalError = error;
    this.shutdown(error);
    try { this.onFailure?.(error); } catch { /* Capture failure reporting must not throw from the audio event. */ }
  }

  private shutdown(error: Error): void {
    if (!this.closed) {
      this.closed = true;
      this.onPCM = null;
      this.processor.port.onmessage = null;
      this.processor.removeEventListener?.("processorerror", this.handleProcessorError);
      try { this.source.disconnect(); } catch { /* Already disconnected. */ }
      try { this.processor.disconnect(); } catch { /* Already disconnected. */ }
      try { this.sink.disconnect(); } catch { /* Already disconnected. */ }
      void this.context.close().catch(() => undefined);
    }
    for (const request of this.pending.values()) {
      if (request.delayTimer !== null) window.clearTimeout(request.delayTimer);
      window.clearTimeout(request.timeoutTimer);
      request.reject(error);
    }
    this.pending.clear();
    this.chunks = [];
  }
}

export const encodeWav = (chunks: Int16Array[], sampleRate: number): Blob => {
  const sampleCount = chunks.reduce((total, chunk) => total + chunk.length, 0);
  const buffer = new ArrayBuffer(44 + sampleCount * 2);
  const view = new DataView(buffer);
  const label = (at: number, value: string) => {
    for (let index = 0; index < value.length; index += 1) view.setUint8(at + index, value.charCodeAt(index));
  };
  label(0, "RIFF");
  view.setUint32(4, 36 + sampleCount * 2, true);
  label(8, "WAVE");
  label(12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  label(36, "data");
  view.setUint32(40, sampleCount * 2, true);
  let offset = 44;
  for (const chunk of chunks) {
    for (let index = 0; index < chunk.length; index += 1) {
      view.setInt16(offset, chunk[index], true);
      offset += 2;
    }
  }
  return new Blob([buffer], { type: "audio/wav" });
};
