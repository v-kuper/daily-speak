import type { Recording } from "./data";

const normalizedKey = (value: string): string => value.trim().toLocaleLowerCase().replace(/\s+/g, " ");

export const collectRecentAnsweredQuestions = (recordings: Recording[], limit = 20): string[] => {
  if (!Number.isSafeInteger(limit) || limit <= 0) return [];
  const seen = new Set<string>();
  const questions: string[] = [];
  const add = (value: string) => {
    const question = value.trim().replace(/\s+/g, " ");
    const key = normalizedKey(question);
    if (!question || seen.has(key) || questions.length >= limit) return;
    seen.add(key);
    questions.push(question);
  };

  for (const recording of recordings) {
    if (recording.practiceType !== "topic") continue;
    if (recording.transcript.trim().length > 0 || recording.interviewTurns.some((turn) => turn.answerText.trim().length > 0)) {
      add(recording.topic);
    }
    if (questions.length >= limit) break;
  }
  return questions;
};

export const questionHistoryKey = (questions: string[]): string =>
  questions.map(normalizedKey).filter(Boolean).join("\u0000");
