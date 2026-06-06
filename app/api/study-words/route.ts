import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE_NAME, getUserBySessionToken } from "../../../src/server/auth";
import {
  DEFAULT_ENGLISH_LEVEL,
  formatEnglishLevel,
  getEnglishLevelPromptGuidance,
  normalizeEnglishLevel,
  type EnglishLevel
} from "../../../src/lib/englishLevel";
import {
  DEFAULT_OLLAMA_BASE_URL,
  extractOllamaMessageContent,
  extractJsonCandidates,
  getOllamaThinkOption,
  normalizeOllamaContent,
  resolveOllamaSettingsForUser
} from "../../../src/server/ollama";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../src/server/logger";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
  response?: string;
};

type StudyPack = {
  words: string[];
  text: string;
};

type ParsedPayload = {
  words?: unknown;
  vocabulary?: unknown;
  text?: unknown;
  story?: unknown;
  paragraph?: unknown;
};

const hashString = (value: string): number => {
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash);
};

const normalizeInterests = (items: string[]): string[] => {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const raw of items) {
    const value = raw.trim().replace(/\s+/g, " ");
    if (!value) {
      continue;
    }

    const key = value.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    normalized.push(value);
  }

  return normalized.slice(0, 10);
};

const normalizeAvoidWords = (items: string[]): string[] => {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const raw of items) {
    const value = normalizeWord(raw);
    if (!value) {
      continue;
    }

    const key = value.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    normalized.push(value);
  }

  return normalized;
};

function normalizeWord(value: string): string {
  return value
    .trim()
    .replace(/^\d+\s*[\)\.\-:]\s*/, "")
    .replace(/^[-*]\s*/, "")
    .replace(/^["'`]+/, "")
    .replace(/["'`]+$/, "")
    .replace(/[.,;:!?]+$/g, "")
    .replace(/\s+/g, " ");
}

const normalizeWordList = (items: string[]): string[] => {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const raw of items) {
    const word = normalizeWord(raw);
    if (!word) {
      continue;
    }

    const tokenCount = word.split(/\s+/).length;
    if (tokenCount > 3 || word.length > 36) {
      continue;
    }

    const key = word.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    normalized.push(word);
  }

  return normalized;
};

const normalizeText = (value: unknown): string => {
  if (typeof value !== "string") {
    return "";
  }

  return value
    .trim()
    .replace(/\r\n/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .replace(/[ \t]+/g, " ")
    .slice(0, 20000);
};

const countWords = (text: string): number => {
  return text
    .trim()
    .split(/\s+/)
    .filter((item) => item.length > 0).length;
};

const countMatchedWords = (text: string, words: string[]): number => {
  const lowerText = text.toLowerCase();
  let count = 0;

  for (const word of words) {
    const escaped = word.toLowerCase().replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const regex = new RegExp(`\\b${escaped}\\b`, "i");
    if (regex.test(lowerText)) {
      count += 1;
    }
  }

  return count;
};

const hasOverlap = (words: string[], avoidWords: string[]): boolean => {
  if (avoidWords.length === 0) {
    return false;
  }

  const avoidSet = new Set(avoidWords.map((item) => item.toLowerCase()));
  return words.some((item) => avoidSet.has(item.toLowerCase()));
};

const parseFromJson = (jsonText: string, avoidWords: string[]): StudyPack | null => {
  try {
    const payload = JSON.parse(jsonText) as ParsedPayload;
    const wordsRaw = Array.isArray(payload.words)
      ? payload.words
      : Array.isArray(payload.vocabulary)
        ? payload.vocabulary
        : [];
    const words = normalizeWordList(wordsRaw.filter((item): item is string => typeof item === "string")).slice(0, 10);
    const text = normalizeText(payload.text ?? payload.story ?? payload.paragraph);

    if (words.length !== 10 || countWords(text) < 80 || hasOverlap(words, avoidWords)) {
      return null;
    }

    if (countMatchedWords(text, words) < 7) {
      return null;
    }

    return { words, text };
  } catch {
    return null;
  }
};

const parseFromLines = (content: string, avoidWords: string[]): StudyPack | null => {
  const lines = normalizeOllamaContent(content)
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);

  if (lines.length < 4) {
    return null;
  }

  const wordsLine =
    lines.find((line) => /^words?\s*:/i.test(line)) ??
    lines.find((line) => line.includes(","));
  const candidateWords = wordsLine
    ? wordsLine.replace(/^words?\s*:/i, "").split(/[,\|]/g)
    : lines.slice(0, 10);
  const words = normalizeWordList(candidateWords).slice(0, 10);

  const textSource = lines
    .filter((line) => line !== wordsLine)
    .join("\n");
  const text = normalizeText(textSource);

  if (words.length !== 10 || countWords(text) < 80 || hasOverlap(words, avoidWords)) {
    return null;
  }

  if (countMatchedWords(text, words) < 7) {
    return null;
  }

  return { words, text };
};

const parseStudyPack = (content: string, avoidWords: string[]): StudyPack | null => {
  for (const candidate of extractJsonCandidates(content)) {
    const parsed = parseFromJson(candidate, avoidWords);
    if (parsed) {
      return parsed;
    }
  }

  return parseFromLines(content, avoidWords);
};

const createPrompt = (
  englishLevel: EnglishLevel,
  interests: string[],
  refreshToken: string | null,
  avoidWords: string[]
): string => {
  const formattedLevel = formatEnglishLevel(englishLevel);
  const levelGuidance = getEnglishLevelPromptGuidance(englishLevel);
  const parts = [
    "Generate vocabulary for English speaking/reading study.",
    `Learner level: ${formattedLevel}.`,
    `Language difficulty: ${levelGuidance}`,
    "Return exactly 10 useful English words (single words or short 2-word terms).",
    "Then write one cohesive text (120-180 words) that naturally uses these words in context.",
    "The text must be clear and practical so learner understands usage context.",
    'Return only JSON with this exact shape: {"words":["w1","w2","w3","w4","w5","w6","w7","w8","w9","w10"],"text":"..."}',
    "No markdown, no extra keys, no explanations."
  ];

  if (interests.length > 0) {
    parts.push(`Prefer topics connected to these interests: ${interests.join(", ")}.`);
  }

  if (avoidWords.length > 0) {
    parts.push(`Do not reuse these words: ${avoidWords.join(", ")}.`);
  }

  if (refreshToken) {
    parts.push(`Variation key: ${refreshToken}. Generate a different set than previous outputs.`);
  }

  return parts.join(" ");
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.study-words.get", request);
  const startedAt = Date.now();
  const { searchParams } = new URL(request.url);
  const refreshToken = searchParams.get("refresh");
  const interests = normalizeInterests(searchParams.getAll("interest"));
  const avoidWords = normalizeAvoidWords(searchParams.getAll("avoidWord"));

  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  const user = await getUserBySessionToken(token);
  const requestedLevel = normalizeEnglishLevel(searchParams.get("level"), DEFAULT_ENGLISH_LEVEL);
  const englishLevel = user?.englishLevel ?? requestedLevel;
  const ollamaSettings = await resolveOllamaSettingsForUser();
  const model = ollamaSettings.model;
  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;

  const seed = Math.abs(
    (hashString(englishLevel) * 131 +
      hashString(interests.join("|").toLowerCase()) * 17 +
      hashString(avoidWords.join("|").toLowerCase()) * 19 +
      hashString(refreshToken ?? "")) %
      2_147_483_647
  );

  logger.info("request.start", {
    model,
    englishLevel,
    interestsCount: interests.length,
    avoidWordsCount: avoidWords.length,
    hasRefreshToken: Boolean(refreshToken)
  });

  try {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const attemptNumber = attempt + 1;
      const attemptSeed = Math.abs((seed + (attempt + 1) * 9157) % 2_147_483_647);
      const response = await fetch(`${baseUrl}/api/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          model,
          stream: false,
          think: getOllamaThinkOption(ollamaSettings.isThinkingModel),
          messages: [
            {
              role: "system",
              content: "You generate level-appropriate vocabulary packs and must follow the JSON output format exactly."
            },
            {
              role: "user",
              content: createPrompt(englishLevel, interests, refreshToken, avoidWords)
            }
          ],
          options: {
            temperature: refreshToken ? 0.68 + attempt * 0.08 : 0.22 + attempt * 0.05,
            seed: attemptSeed
          }
        }),
        cache: "no-store"
      });

      if (!response.ok) {
        const text = await response.text();
        logger.warn("ollama.request_failed", {
          status: response.status,
          durationMs: elapsedMs(startedAt),
          model,
          attempt: attemptNumber,
          bodyPreview: text.slice(0, 200)
        });
        return NextResponse.json(
          { error: `Ollama request failed (${response.status}): ${text.slice(0, 300)}` },
          { status: 502 }
        );
      }

      const payload = (await response.json()) as OllamaChatResponse;
      const content = extractOllamaMessageContent(payload);
      if (!content) {
        logger.debug("ollama.empty_content", { attempt: attemptNumber, model });
        continue;
      }

      const parsed = parseStudyPack(content, avoidWords);
      if (!parsed) {
        logger.debug("ollama.parse_failed", { attempt: attemptNumber, model, contentLength: content.length });
        continue;
      }

      logger.info("request.success", {
        status: 200,
        durationMs: elapsedMs(startedAt),
        model,
        attempt: attemptNumber,
        wordsCount: parsed.words.length
      });
      return NextResponse.json(parsed, { status: 200 });
    }

    logger.warn("request.failed", {
      status: 502,
      durationMs: elapsedMs(startedAt),
      model,
      reason: "no_valid_generation"
    });
    return NextResponse.json(
      { error: "Could not generate a valid words pack. Try regenerate." },
      { status: 502 }
    );
  } catch (error) {
    logger.error("request.failed", { status: 502, durationMs: elapsedMs(startedAt), model, ...toErrorMeta(error) });
    return NextResponse.json(
      { error: "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service." },
      { status: 502 }
    );
  }
}
