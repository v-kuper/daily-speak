import { apiFetch, readApiJSON } from "./apiClient";
import { resolveInterestLabels } from "./interestCatalog";

export const dismissDailyQuestion = async (question: string): Promise<void> => {
  const response = await apiFetch("/api/v1/practice/daily-questions/dismiss", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ question }),
  });
  if (!response.ok) {
    const payload = await readApiJSON<{ error?: { message?: string } }>(response).catch(() => null);
    throw new Error(payload?.error?.message || "Could not save your question preference.");
  }
};

type ReplacementOptions = {
  dateKey: string;
  refreshToken: string;
  interestIds: string[];
  englishLevel: string;
  currentQuestions: string[];
  avoidQuestions: string[];
};

export const fetchDailyQuestionReplacement = async (options: ReplacementOptions): Promise<string> => {
  const params = new URLSearchParams({
    date: options.dateKey,
    count: "1",
    refresh: options.refreshToken,
    level: options.englishLevel,
  });
  resolveInterestLabels(options.interestIds).forEach((interest) => params.append("interest", interest));
  options.currentQuestions.forEach((question) => params.append("current", question));
  options.avoidQuestions.forEach((question) => params.append("avoid", question));

  const response = await apiFetch(`/api/v1/practice/daily-questions?${params.toString()}`, { cache: "no-store" });
  const payload = await readApiJSON<{ questions?: unknown; error?: { message?: string } }>(response).catch(() => null);
  if (!response.ok) {
    throw new Error(payload?.error?.message || "Could not find a replacement question.");
  }
  const questions = payload?.questions;
  if (!Array.isArray(questions) || questions.length !== 1 || typeof questions[0] !== "string" || !questions[0].trim()) {
    throw new Error("The practice service did not return one replacement question.");
  }
  return questions[0].trim();
};
