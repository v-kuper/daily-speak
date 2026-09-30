export type GuestPreviewCorrection = { wrong: string; right: string; explanation: string };

export const parseGuestPreviewCorrections = (value: unknown): GuestPreviewCorrection[] => {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw) => {
    if (!raw || typeof raw !== "object") return [];
    const item = raw as Record<string, unknown>;
    const wrong = typeof item.wrong === "string" ? item.wrong.trim() : "";
    const right = typeof item.right === "string" ? item.right.trim() : "";
    const explanation = typeof item.explanation === "string" ? item.explanation.trim() : "";
    return wrong && right && explanation ? [{ wrong, right, explanation }] : [];
  }).slice(0, 2);
};

// Guest previews have their own limited correction contract, without the
// account feedback spans. Mark only evidence unique across learner answers.
export const guestPreviewSegments = (text: string, corrections: GuestPreviewCorrection[], answers = [text]) => {
  const word = /[\p{L}\p{N}\p{M}_]/u;
  const occurrences = (answer: string, phrase: string): number[] => {
    const found: number[] = [];
    for (let from = 0; from < answer.length;) {
      const start = answer.indexOf(phrase, from);
      if (start < 0) break;
      const end = start + phrase.length;
      const before = Array.from(answer.slice(Math.max(0, start - 2), start)).at(-1) ?? "";
      const after = Array.from(answer.slice(end, end + 2))[0] ?? "";
      if (!(word.test(Array.from(phrase)[0]) && word.test(before))
        && !(word.test(Array.from(phrase).at(-1) ?? "") && word.test(after))) found.push(start);
      from = start + 1;
    }
    return found;
  };
  const ranges = corrections.flatMap(({ wrong }) => {
    if (!wrong || answers.reduce((count, answer) => count + occurrences(answer, wrong).length, 0) !== 1) return [];
    return occurrences(text, wrong).map(start => ({ start, end: start + wrong.length }));
  }).sort((a, b) => a.start - b.start || b.end - a.end);
  const segments: { text: string; isCorrection: boolean }[] = [];
  let cursor = 0;
  for (const { start, end } of ranges) {
    if (start < cursor) continue;
    if (start > cursor) segments.push({ text: text.slice(cursor, start), isCorrection: false });
    segments.push({ text: text.slice(start, end), isCorrection: true });
    cursor = end;
  }
  if (cursor < text.length) segments.push({ text: text.slice(cursor), isCorrection: false });
  return segments;
};
