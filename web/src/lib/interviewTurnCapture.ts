/** Captures independent WAV answers while the normal MediaRecorder keeps the
 * complete interview as one uninterrupted audio file. */
export class InterviewTurnCapture {
  private chunks: Int16Array[] = [];
  private pending = new Map<number, { resolve: (blob: Blob | null) => void; reject: (error: Error) => void; timer: number }>();
  private nextBoundary = 1;
  private closed = false;

  private constructor(
    private readonly context: AudioContext,
    private readonly source: MediaStreamAudioSourceNode,
    private readonly processor: AudioWorkletNode,
    private readonly sink: GainNode,
  ) {
    processor.port.onmessage = (event: MessageEvent<{ type?: string; id?: number; pcm?: Int16Array }>) => {
      if (event.data?.type === "samples" && event.data.pcm instanceof Int16Array) {
        this.chunks.push(event.data.pcm);
        return;
      }
      if (event.data?.type !== "boundary" || typeof event.data.id !== "number") return;
      // A boundary can arrive after its two second caller timeout. Always
      // consume that partition so stale PCM cannot leak into the next answer.
      const chunks = this.chunks;
      this.chunks = [];
      const request = this.pending.get(event.data.id);
      if (!request) return;
      this.pending.delete(event.data.id);
      window.clearTimeout(request.timer);
      request.resolve(chunks.length ? encodeWav(chunks, Math.min(16000, this.context.sampleRate)) : null);
    };
  }

  static async start(stream: MediaStream): Promise<InterviewTurnCapture> {
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
      return new InterviewTurnCapture(context, source, processor, sink);
    } catch (error) {
      await context.close();
      throw error;
    }
  }

  closeTurn(): Promise<Blob | null> {
    if (this.closed) return Promise.resolve(null);
    const id = this.nextBoundary++;
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(id);
        reject(new Error("Answer capture timed out."));
      }, 2000);
      this.pending.set(id, { resolve, reject, timer });
      this.processor.port.postMessage({ type: "boundary", id });
    });
  }

  async stop(): Promise<Blob | null> {
    if (this.closed) return null;
    let last: Blob | null = null;
    try {
      last = await this.closeTurn();
    } finally {
      this.closed = true;
      this.source.disconnect();
      this.processor.disconnect();
      this.sink.disconnect();
      await this.context.close().catch(() => undefined);
      for (const request of this.pending.values()) {
        window.clearTimeout(request.timer);
        request.reject(new Error("Answer capture stopped."));
      }
      this.pending.clear();
      this.chunks = [];
    }
    return last;
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
