export type EnglishLevel = "a1" | "a2" | "b1" | "b2" | "c1" | "c2";

export type EnglishLevelOption = {
  value: EnglishLevel;
  label: string;
  description: string;
};

export const DEFAULT_ENGLISH_LEVEL: EnglishLevel = "b1";

export const ENGLISH_LEVEL_OPTIONS: EnglishLevelOption[] = [
  { value: "a1", label: "A1 Beginner", description: "Very simple words and short daily-life questions." },
  { value: "a2", label: "A2 Elementary", description: "Simple everyday questions with basic past/future forms." },
  { value: "b1", label: "B1 Intermediate", description: "Practical questions with moderate vocabulary." },
  { value: "b2", label: "B2 Upper-Intermediate", description: "More nuanced questions and wider vocabulary." },
  { value: "c1", label: "C1 Advanced", description: "Abstract topics, precise wording, and complex structures." },
  { value: "c2", label: "C2 Proficiency", description: "Near-native complexity with idiomatic phrasing." }
];

const ENGLISH_LEVEL_SET = new Set<EnglishLevel>(ENGLISH_LEVEL_OPTIONS.map((option) => option.value));

export const parseEnglishLevel = (value: unknown): EnglishLevel | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().toLowerCase();
  if (!ENGLISH_LEVEL_SET.has(normalized as EnglishLevel)) {
    return null;
  }

  return normalized as EnglishLevel;
};

export const normalizeEnglishLevel = (
  value: unknown,
  fallback: EnglishLevel = DEFAULT_ENGLISH_LEVEL
): EnglishLevel => {
  return parseEnglishLevel(value) ?? fallback;
};

export const formatEnglishLevel = (level: EnglishLevel): string => {
  return level.toUpperCase();
};

export const getEnglishLevelPromptGuidance = (level: EnglishLevel): string => {
  switch (level) {
    case "a1":
      return "Use very basic vocabulary, short present-tense phrasing, and one clear idea per question.";
    case "a2":
      return "Use simple everyday vocabulary, short sentences, and basic past/future forms.";
    case "b1":
      return "Use practical intermediate vocabulary and clear sentence structures.";
    case "b2":
      return "Use richer vocabulary, nuanced scenarios, and natural connector words.";
    case "c1":
      return "Use advanced vocabulary, abstract angles, and complex but natural phrasing.";
    case "c2":
      return "Use near-native sophistication, idiomatic phrasing, and subtle distinctions.";
    default:
      return "Use practical intermediate vocabulary and clear sentence structures.";
  }
};
