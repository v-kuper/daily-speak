import type { FeedbackFocus, FocusedFeedback } from "./data";

export const parseFocusedFeedback = (value: unknown): FocusedFeedback | undefined => {
 if (!value || typeof value !== "object") return undefined;
 const source = value as Record<string, unknown>;
 if (source.version !== 1 || !Array.isArray(source.answers)) return undefined;
 const answers: FocusedFeedback["answers"] = [];
 for (const raw of source.answers) {
  if (!raw || typeof raw !== "object") return undefined;
  const answer = raw as Record<string, unknown>;
  const sequence = answer.turnSequence ?? 0;
  if (!Number.isSafeInteger(sequence) || Number(sequence) < 0 || !Array.isArray(answer.items)) return undefined;
  const items: FeedbackFocus[] = [];
  for (const rawItem of answer.items) {
   if (!rawItem || typeof rawItem !== "object") return undefined;
   const item = rawItem as Record<string, unknown>;
   const span = item.span as Record<string, unknown> | undefined;
   if (!["praise", "blocker", "native_tip"].includes(String(item.kind)) || !span ||
    !Number.isSafeInteger(span.start) || !Number.isSafeInteger(span.end) || Number(span.start) < 0 || Number(span.end) <= Number(span.start) ||
    !Number.isSafeInteger(item.occurrence) || Number(item.occurrence) < 1 ||
    !["id", "originalFragment", "title", "explanation", "ruleId"].every(key => typeof item[key] === "string" && Boolean(item[key]))) return undefined;
   const lesson = item.microLesson as { title?: unknown; points?: unknown } | undefined;
   items.push({ id: String(item.id), kind: item.kind as FeedbackFocus["kind"], originalFragment: String(item.originalFragment),
    occurrence: Number(item.occurrence), title: String(item.title), explanation: String(item.explanation), ruleId: String(item.ruleId),
    span: { start: Number(span.start), end: Number(span.end), turnSequence: Number(sequence) },
    correctedFragment: typeof item.correctedFragment === "string" ? item.correctedFragment : undefined,
    practiceText: typeof item.practiceText === "string" ? item.practiceText : undefined,
    microLesson: lesson && typeof lesson.title === "string" && Array.isArray(lesson.points) && lesson.points.length === 3 && lesson.points.every(point => typeof point === "string")
     ? { title: lesson.title, points: lesson.points as [string, string, string] } : undefined });
  }
  if (items.filter(item => item.kind === "praise").length > 1 ||
   items.filter(item => item.kind === "native_tip").length > 1) return undefined;
  answers.push({ turnSequence: Number(sequence), items });
 }
 return { version: 1, answers };
};

export type FocusSegment = { text: string; focus?: FeedbackFocus };
export const focusedSegments = (text: string, items: FeedbackFocus[]): FocusSegment[] => {
 const segments: FocusSegment[] = []; let cursor = 0;
 for (const focus of [...items].sort((a, b) => a.span.start - b.span.start)) {
  const { start, end } = focus.span;
  if (start < cursor || end > text.length || text.slice(start, end) !== focus.originalFragment) continue;
  if (start > cursor) segments.push({ text: text.slice(cursor, start) });
  segments.push({ text: text.slice(start, end), focus }); cursor = end;
 }
 if (cursor < text.length) segments.push({ text: text.slice(cursor) });
 return segments;
};
