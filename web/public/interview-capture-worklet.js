/* A second, low-bandwidth capture path for interview answers. The final
 * MediaRecorder is independent of this processor. */
class InterviewCaptureProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.targetRate = Math.min(16000, sampleRate);
    this.sourceFramesPerSample = sampleRate / this.targetRate;
    this.sourceFrames = 0;
    this.sum = 0;
    this.count = 0;
    // Cartesia recommends roughly 100 ms of realtime audio per message.
    // Keep the resampler continuous across messages; only a question boundary
    // flushes a shorter tail so adjacent sounds are not dropped.
    this.buffer = new Int16Array(Math.round(this.targetRate / 10));
    this.length = 0;
    this.port.onmessage = (event) => {
      if (event.data?.type !== "boundary") return;
      if (this.count > 0) {
        this.push(this.sum / this.count);
        this.sourceFrames = 0;
        this.sum = 0;
        this.count = 0;
      }
      this.flush();
      this.port.postMessage({ type: "boundary", id: event.data.id });
    };
  }

  push(sample) {
    this.buffer[this.length++] = Math.round(Math.max(-1, Math.min(1, sample)) * 32767);
    if (this.length === this.buffer.length) this.flush();
  }

  flush() {
    if (this.length === 0) return;
    const pcm = this.buffer.slice(0, this.length);
    this.port.postMessage({ type: "samples", pcm }, [pcm.buffer]);
    this.length = 0;
  }

  process(inputs) {
    const input = inputs[0];
    if (!input || input.length === 0) return true;
    const channel = input[0];
    for (let index = 0; index < channel.length; index += 1) {
      let sample = 0;
      for (let channelIndex = 0; channelIndex < input.length; channelIndex += 1) {
        sample += input[channelIndex][index] || 0;
      }
      this.sum += sample / input.length;
      this.count += 1;
      this.sourceFrames += 1;
      if (this.sourceFrames >= this.sourceFramesPerSample) {
        this.push(this.sum / this.count);
        this.sourceFrames -= this.sourceFramesPerSample;
        this.sum = 0;
        this.count = 0;
      }
    }
    return true;
  }
}

registerProcessor("interview-capture", InterviewCaptureProcessor);
