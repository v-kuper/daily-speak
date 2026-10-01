export type ActivityKind = "speaking" | "review";
export type ActivityInterval = { id: string; kind: ActivityKind; startedAt: string; endedAt: string };
export type ActivityDay = { date: string; speakingMilliseconds: number; reviewMilliseconds: number; level: number };
export type ActivitySummary = {
  totalSpeakingMilliseconds: number;
  historicalSpeakingMilliseconds: number;
  timezone: string; from: string; to: string; days: ActivityDay[];
};

export const REVIEW_IDLE_MS = 120_000;
export const ACTIVITY_CHUNK_MS = 30_000;

export const activityIntensity = (milliseconds: number): number =>
  milliseconds <= 0 ? 0 : milliseconds <= 300_000 ? 1 : milliseconds < 900_000 ? 2 : 3;

export const activityTimeLabel = (milliseconds: number): string => {
  const minutes = Math.floor(milliseconds / 60_000);
  return `${Math.floor(minutes / 60)} ч. ${minutes % 60} мин.`;
};

export const activityMinutesLabel = (milliseconds: number): string =>
  milliseconds <= 0 ? "0 мин" : milliseconds < 60_000 ? "меньше минуты" : `${Math.floor(milliseconds / 60_000)} мин`;

// Date keys are calendar dates, never parsed in the browser's UTC timezone.
const keyDate = (key: string): Date => new Date(`${key}T12:00:00Z`);
const dayKey = (date: Date): string => date.toISOString().slice(0, 10);
const moveDate = (date: Date, days: number): Date => new Date(date.getTime() + days * 86_400_000);

export const heatmapWeeks = (days: ActivityDay[], mobile = false): (ActivityDay | null)[][] => {
  if (!days.length) return [];
  const last = keyDate(days[days.length - 1].date);
  const monday = (date: Date) => moveDate(date, -((date.getUTCDay() + 6) % 7));
  const first = mobile ? moveDate(monday(last), -23 * 7) : monday(keyDate(days[0].date));
  const byDate = new Map(days.map(day => [day.date, day]));
  const weeks: (ActivityDay | null)[][] = [];
  for (let date = first; date <= last; date = moveDate(date, 7)) {
    weeks.push(Array.from({ length: 7 }, (_, row) => byDate.get(dayKey(moveDate(date, row))) ?? null));
  }
  return weeks;
};

// One clock unions all local sources. A repetition on the feedback screen is
// speaking time, not a second simultaneous credit for reading the screen.
export class ActivityClock {
  private sources = new Map<string, ActivityKind>();
  private cursor: number;
  private lastInteraction: number;
  private visible = true;
  constructor(now: number, private emit: (kind: ActivityKind, from: number, to: number) => void) {
    this.cursor = now; this.lastInteraction = now;
  }
  settle(now: number): void {
    const kinds = Array.from(this.sources.values());
    const kind = kinds.includes("speaking") ? "speaking" : kinds.includes("review") ? "review" : null;
    const end = kind === "review" ? Math.min(now, this.lastInteraction + REVIEW_IDLE_MS) : now;
    // A delayed timer after sleep/freeze cannot credit unattended wall time.
    if (this.visible && kind && now > this.cursor && now - this.cursor <= ACTIVITY_CHUNK_MS && end > this.cursor) {
      this.emit(kind, this.cursor, end);
    }
    this.cursor = now;
  }
  start(id: string, kind: ActivityKind, now: number): void {
    this.settle(now); this.sources.set(id, kind); this.lastInteraction = now;
  }
  stop(id: string, now: number): void { this.settle(now); this.sources.delete(id); }
  interact(now: number): void {
    if (now > this.lastInteraction + REVIEW_IDLE_MS) this.settle(now);
    this.lastInteraction = now;
  }
  setVisible(visible: boolean, now: number): void {
    this.settle(now); this.visible = visible;
    // Returning to a tab does not itself resume an idle review session.
  }
}

export const parseActivitySummary = (value: unknown): ActivitySummary => {
  if (!value || typeof value !== "object") throw new Error("Не удалось прочитать статистику.");
  const item = value as ActivitySummary;
  const nonnegative = (number: unknown) => typeof number === "number" && Number.isSafeInteger(number) && number >= 0;
  const date = (key: unknown) => typeof key === "string" && /^\d{4}-\d{2}-\d{2}$/.test(key);
  if (!nonnegative(item.totalSpeakingMilliseconds) || !nonnegative(item.historicalSpeakingMilliseconds) ||
    typeof item.timezone !== "string" || !date(item.from) || !date(item.to) || !Array.isArray(item.days) ||
    item.days.length === 0 || item.days.length > 366 || item.days.some((day, index) =>
      !day || !date(day.date) || !nonnegative(day.speakingMilliseconds) || !nonnegative(day.reviewMilliseconds) ||
      day.level !== activityIntensity(day.speakingMilliseconds + day.reviewMilliseconds) ||
      (index > 0 && day.date <= item.days[index - 1].date))) {
    throw new Error("Не удалось прочитать статистику.");
  }
  return item;
};
