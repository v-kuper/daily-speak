import { apiFetch, readApiJSON } from "./apiClient";

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
