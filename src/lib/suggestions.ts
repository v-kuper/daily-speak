import type {
  LearningReference,
  Suggestion,
  SuggestionCategory,
  SuggestionSeverity
} from "./data";

const SUGGESTION_CATEGORIES = new Set<SuggestionCategory>([
  "language_switch",
  "verb_grammar",
  "nouns_determiners",
  "prepositions",
  "vocabulary",
  "sentence_structure",
  "naturalness"
]);

const SUGGESTION_SEVERITIES = new Set<SuggestionSeverity>(["major", "medium", "minor"]);
const LEARNING_REFERENCE_HOSTS = new Set([
  "dictionary.cambridge.org",
  "learnenglish.britishcouncil.org"
]);

const trimmedString = (value: unknown): string =>
  typeof value === "string" ? value.trim() : "";

const parseCategory = (value: unknown): SuggestionCategory | undefined => {
  const category = trimmedString(value) as SuggestionCategory;
  return SUGGESTION_CATEGORIES.has(category) ? category : undefined;
};

const parseSeverity = (value: unknown): SuggestionSeverity | undefined => {
  const severity = trimmedString(value) as SuggestionSeverity;
  return SUGGESTION_SEVERITIES.has(severity) ? severity : undefined;
};

const isSafeLearningURL = (value: string): boolean => {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && LEARNING_REFERENCE_HOSTS.has(url.hostname);
  } catch {
    return false;
  }
};

const parseLearningReference = (value: unknown): LearningReference | undefined => {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }

  const candidate = value as Record<string, unknown>;
  const id = trimmedString(candidate.id);
  const title = trimmedString(candidate.title);
  const summary = trimmedString(candidate.summary);

  if (!id || !title || !summary) {
    return undefined;
  }

  if (candidate.url === undefined) {
    return { id, title, summary };
  }

  const url = trimmedString(candidate.url);
  if (!url || !isSafeLearningURL(url)) {
    return undefined;
  }

  return { id, title, summary, url };
};

const parseSuggestion = (value: unknown): Suggestion | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const candidate = value as Record<string, unknown>;
  const wrong = trimmedString(candidate.wrong);
  const right = trimmedString(candidate.right);
  const explanation = trimmedString(candidate.explanation);

  if (!wrong || !right || !explanation) {
    return null;
  }

  const suggestion: Suggestion = { wrong, right, explanation };
  const category = parseCategory(candidate.category);
  const severity = parseSeverity(candidate.severity);
  const ruleId = trimmedString(candidate.ruleId);
  const learningReference = parseLearningReference(candidate.learningReference);

  if (category) {
    suggestion.category = category;
  }
  if (severity) {
    suggestion.severity = severity;
  }
  if (ruleId) {
    suggestion.ruleId = ruleId;
  }
  if (learningReference) {
    suggestion.learningReference = learningReference;
  }

  return suggestion;
};

export const parseSuggestions = (value: unknown): Suggestion[] => {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .map((item) => parseSuggestion(item))
    .filter((item): item is Suggestion => item !== null);
};
