import type { FeedbackSpan } from "./data";

const wordCharacter = /[\p{L}\p{N}\p{M}_]/u;

export const parseFeedbackSpan = (value: unknown): FeedbackSpan | undefined => {
  if (!value || typeof value !== "object") return undefined;
  const span = value as Record<string, unknown>;
  if (!Number.isSafeInteger(span.start) || !Number.isSafeInteger(span.end)
    || (span.start as number) < 0 || (span.end as number) <= (span.start as number)
    || (span.turnSequence !== undefined && (!Number.isSafeInteger(span.turnSequence) || (span.turnSequence as number) < 1))) return undefined;
  return { start: span.start as number, end: span.end as number,
    ...(span.turnSequence !== undefined ? { turnSequence: span.turnSequence as number } : {}) };
};

export const exactPhraseRanges = (text: string, phrase: string): FeedbackSpan[] => {
  if (!phrase) return [];
  const ranges: FeedbackSpan[] = [];
  const first = Array.from(phrase)[0];
  const last = Array.from(phrase).at(-1) ?? "";
  for (let from = 0; from < text.length;) {
    const start = text.indexOf(phrase, from);
    if (start < 0) break;
    const end = start + phrase.length;
    const before = Array.from(text.slice(Math.max(0, start - 2), start)).at(-1) ?? "";
    const after = Array.from(text.slice(end, end + 2))[0] ?? "";
    if (!(wordCharacter.test(first) && wordCharacter.test(before))
      && !(wordCharacter.test(last) && wordCharacter.test(after))) ranges.push({ start, end });
    from = start + 1;
  }
  return ranges;
};

export const feedbackRanges = (text: string, phrase: string, span?: FeedbackSpan, turnSequence = 0): FeedbackSpan[] => {
  if (span) {
    if (!Number.isSafeInteger(span.start) || !Number.isSafeInteger(span.end)
      || (span.turnSequence ?? 0) !== turnSequence || span.start < 0 || span.end > text.length
      || span.end <= span.start || text.slice(span.start, span.end) !== phrase) return [];
    return [span];
  }
  const ranges = exactPhraseRanges(text, phrase);
  return ranges.length === 1 ? ranges : [];
};
