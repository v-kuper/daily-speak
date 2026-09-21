const CATEGORY_LABELS: Record<string, string> = {
  language_switch: "Russian → English",
  verb_grammar: "Verb grammar",
  nouns_determiners: "Nouns and determiners",
  prepositions: "Prepositions",
  vocabulary: "Vocabulary",
  sentence_structure: "Sentence structure",
  naturalness: "Naturalness"
};

const SEVERITY_LABELS: Record<string, string> = {
  major: "Major",
  medium: "Medium",
  minor: "Minor"
};

export const suggestionCategoryLabel = (value: unknown): string | null => {
  return typeof value === "string" ? CATEGORY_LABELS[value] ?? null : null;
};

export const suggestionSeverityLabel = (value: unknown): string | null => {
  return typeof value === "string" ? SEVERITY_LABELS[value] ?? null : null;
};

export const suggestionSeverityClass = (value: unknown): string | null => {
  return suggestionSeverityLabel(value) ? `suggestion-severity-${value}` : null;
};
