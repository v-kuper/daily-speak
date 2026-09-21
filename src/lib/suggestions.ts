import type { Suggestion } from "./data";

const parseSuggestion = (value: unknown): Suggestion | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const candidate = value as Record<string, unknown>;
  const wrong = typeof candidate.wrong === "string" ? candidate.wrong.trim() : "";
  const right = typeof candidate.right === "string" ? candidate.right.trim() : "";
  const explanation = typeof candidate.explanation === "string" ? candidate.explanation.trim() : "";

  if (!wrong || !right || !explanation) {
    return null;
  }

  return { wrong, right, explanation };
};

export const parseSuggestions = (value: unknown): Suggestion[] => {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .map((item) => parseSuggestion(item))
    .filter((item): item is Suggestion => item !== null);
};
