export const DEFAULT_LIVE_CAPTION_IDLE_MS = 1600;

export type CaptionScheduler = {
  schedule: (callback: () => void, delayMs: number) => number;
  cancel: (handle: number) => void;
};

export type LiveCaptionSnapshot = {
  turnSeq: number;
  finalText: string;
  interimText: string;
  captionText: string;
};

const browserScheduler: CaptionScheduler = {
  schedule: (callback, delayMs) => window.setTimeout(callback, delayMs),
  cancel: (handle) => window.clearTimeout(handle),
};

/** Publishes only the latest recognized phrase and clears it after inactivity. */
export class EphemeralCaptionController {
  private timer: number | null = null;
  private disposed = false;
  private activeTurnSeq: number | null = null;
  private lastFinalText = "";
  private lastInterimText = "";
  private phraseFinalText = "";

  constructor(
    private readonly publish: (caption: string | null) => void,
    private readonly scheduler: CaptionScheduler = browserScheduler,
    private readonly idleMs = DEFAULT_LIVE_CAPTION_IDLE_MS,
  ) {}

  beginTurn(turnSeq: number): void {
    if (this.disposed) return;
    this.reset(turnSeq);
    this.publish(null);
  }

  update(snapshot: LiveCaptionSnapshot): void {
    if (this.disposed || snapshot.turnSeq !== this.activeTurnSeq) return;

    let changed = false;
    if (snapshot.finalText !== this.lastFinalText) {
      if (snapshot.finalText.startsWith(this.lastFinalText)) {
        this.phraseFinalText += snapshot.finalText.slice(this.lastFinalText.length);
      } else {
        // A provider correction replaced the stable prefix. Keep the visible
        // phrase coherent without affecting the full transcript stored by the caller.
        this.phraseFinalText = snapshot.finalText;
      }
      this.lastFinalText = snapshot.finalText;
      changed = true;
    }
    if (snapshot.interimText !== this.lastInterimText) {
      this.lastInterimText = snapshot.interimText;
      changed = true;
    }
    if (!changed && !snapshot.captionText.trim()) return;

    this.cancelTimer();
    const caption = `${this.phraseFinalText}${snapshot.interimText}`.trim();
    this.publish(caption || null);
    if (!caption) return;
    this.timer = this.scheduler.schedule(() => {
      this.timer = null;
      if (this.disposed) return;
      this.phraseFinalText = "";
      this.lastInterimText = "";
      this.publish(null);
    }, this.idleMs);
  }

  clear(): void {
    if (this.disposed) return;
    this.reset(null);
    this.publish(null);
  }

  dispose(): void {
    this.cancelTimer();
    this.disposed = true;
  }

  private cancelTimer(): void {
    if (this.timer !== null) this.scheduler.cancel(this.timer);
    this.timer = null;
  }

  private reset(turnSeq: number | null): void {
    this.cancelTimer();
    this.activeTurnSeq = turnSeq;
    this.lastFinalText = "";
    this.lastInterimText = "";
    this.phraseFinalText = "";
  }
}
