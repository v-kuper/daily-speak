export type ShadowingStatus = "pending" | "processing" | "ready" | "failed";

export type ShadowingScript = {
  englishLevel: string;
  text: string;
  turns: { sequence: number; question: string; answerText: string }[];
};

export const parseShadowingScript = (value: unknown): ShadowingScript | null => {
  if (!value || typeof value !== "object") return null;
  const input = value as Record<string, unknown>;
  if (typeof input.englishLevel !== "string" || !["a1", "a2", "b1", "b2", "c1", "c2"].includes(input.englishLevel)
    || typeof input.text !== "string" || !input.text.trim() || input.text.length > 40000
    || !Array.isArray(input.turns) || !input.turns.length) return null;
  const turns: ShadowingScript["turns"] = [];
  for (const value of input.turns) {
    if (!value || typeof value !== "object") return null;
    const turn = value as Record<string, unknown>;
    if (!Number.isInteger(turn.sequence) || (turn.sequence as number) < 1
      || typeof turn.question !== "string" || !turn.question.trim()
      || typeof turn.answerText !== "string" || !turn.answerText.trim() || turn.answerText.length > 2400
      || (turns.length > 0 && (turn.sequence as number) <= turns[turns.length - 1].sequence)) return null;
    turns.push({ sequence: turn.sequence as number, question: turn.question, answerText: turn.answerText });
  }
  if (turns.flatMap((turn) => [turn.question, turn.answerText]).join(" ") !== input.text) return null;
  return { englishLevel: input.englishLevel, text: input.text, turns };
};

export const parseShadowingStatus = (value: unknown): ShadowingStatus => {
  if (value === "pending" || value === "processing" || value === "ready" || value === "failed") {
    return value;
  }
  return "pending";
};

export const shouldScheduleShadowing = ({
  recordingStatus,
  correctedTranscript,
  shadowingStatus,
  requestLoading,
  hasObsoleteScript = false,
}: {
  recordingStatus: "processing" | "ready" | "failed";
  correctedTranscript: string;
  shadowingStatus: ShadowingStatus;
  requestLoading: boolean;
  hasObsoleteScript?: boolean;
}): boolean => {
  return (
    recordingStatus === "ready" &&
    correctedTranscript.trim().length > 0 &&
    (shadowingStatus === "pending" || (shadowingStatus === "ready" && hasObsoleteScript)) &&
    !requestLoading
  );
};

export const shouldPollRecording = (
  recordingStatus: string,
  shadowingStatus: ShadowingStatus,
  strengthsStatus?: string,
): boolean => {
  return recordingStatus === "processing" || shadowingStatus === "processing" || strengthsStatus === "processing";
};

export const isShadowingStale = (
  status: ShadowingStatus,
  updatedAt: string,
  nowMs = Date.now(),
): boolean => {
  if (status !== "processing") {
    return false;
  }

  const updatedAtMs = Date.parse(updatedAt);
  if (!Number.isFinite(updatedAtMs)) {
    return true;
  }

  return nowMs - updatedAtMs >= 5 * 60 * 1000;
};

export const shadowingProgressLabel = (
  status: ShadowingStatus,
  stale: boolean,
): string => {
  if (status === "processing" && stale) {
    return "Pronunciation audio is taking longer than expected.";
  }
  return "Creating pronunciation audio...";
};
