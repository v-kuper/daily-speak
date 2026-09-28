export type SavedInterviewTurn = {
  sequence: number;
  question: string;
  askedAtMs: number;
  endedAtMs: number | null;
  answerText: string;
  answerSource: "final" | "provisional" | "none";
  answerAlignment: "approximate" | null;
};

export const parseInterviewTurns = (value: unknown): SavedInterviewTurn[] => {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item): SavedInterviewTurn[] => {
    if (!item || typeof item !== "object") return [];
    const turn = item as Record<string, unknown>;
    if (!Number.isSafeInteger(turn.sequence) || Number(turn.sequence) < 1 || typeof turn.question !== "string" || !turn.question.trim()) return [];
    const answerSource = turn.answerSource === "final" || turn.answerSource === "provisional" ? turn.answerSource : "none";
    return [{
      sequence: Number(turn.sequence),
      question: turn.question.trim(),
      askedAtMs: Number.isFinite(turn.askedAtMs) ? Math.max(0, Number(turn.askedAtMs)) : 0,
      endedAtMs: Number.isFinite(turn.endedAtMs) ? Math.max(0, Number(turn.endedAtMs)) : null,
      answerText: typeof turn.answerText === "string" ? turn.answerText : "",
      answerSource,
      answerAlignment: turn.answerAlignment === "approximate" ? "approximate" : null,
    }];
  }).sort((a, b) => a.sequence - b.sequence);
};
