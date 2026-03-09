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
  resolveOllamaModelForUser
} from "../../../src/server/ollama";
import { createRouteLogger, elapsedMs, toErrorMeta } from "../../../src/server/logger";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
const TOPIC_GUIDANCE_QUESTIONS_COUNT = 10;
const TOPIC_GUIDANCE_WORDS_COUNT = 8;

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
  response?: string;
};

type TopicGuidance = {
  questions: string[];
  words: string[];
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

const normalizeQuestions = (items: string[]): string[] => {
  const unique: string[] = [];
  const seen = new Set<string>();

  for (const raw of items) {
    const cleaned = raw
      .trim()
      .replace(/^\d+\s*[\)\.\-:]\s*/, "")
      .replace(/^[-*]\s*/, "")
      .replace(/\s+/g, " ");
    if (!cleaned) {
      continue;
    }

    const withQuestionMark = cleaned.endsWith("?") ? cleaned : `${cleaned}?`;
    const key = withQuestionMark.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    unique.push(withQuestionMark);
  }

  return unique;
};

const normalizeWords = (items: string[]): string[] => {
  const unique: string[] = [];
  const seen = new Set<string>();

  for (const raw of items) {
    const cleaned = raw
      .trim()
      .replace(/^\d+\s*[\)\.\-:]\s*/, "")
      .replace(/^[-*]\s*/, "")
      .replace(/[.;]+$/g, "")
      .replace(/\s+/g, " ");
    if (!cleaned) {
      continue;
    }

    const key = cleaned.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    unique.push(cleaned);
  }

  return unique;
};

const normalizeAvoidList = (items: string[]): string[] => {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const item of items) {
    const cleaned = item.trim().replace(/\s+/g, " ").toLowerCase();
    if (!cleaned || seen.has(cleaned)) {
      continue;
    }

    seen.add(cleaned);
    normalized.push(cleaned);
  }

  return normalized;
};

const hasAnyOverlap = (items: string[], avoidLowerCase: string[]): boolean => {
  if (avoidLowerCase.length === 0) {
    return false;
  }

  const avoidSet = new Set(avoidLowerCase);
  return items.some((item) => avoidSet.has(item.toLowerCase()));
};

const parseTopicGuidance = (content: string): TopicGuidance | null => {
  const tryParse = (jsonText: string): TopicGuidance | null => {
    try {
      const data = JSON.parse(jsonText) as { questions?: unknown; words?: unknown };
      const questions = Array.isArray(data.questions)
        ? normalizeQuestions(data.questions.filter((item): item is string => typeof item === "string"))
        : [];
      const words = Array.isArray(data.words)
        ? normalizeWords(data.words.filter((item): item is string => typeof item === "string"))
        : [];

      if (questions.length >= TOPIC_GUIDANCE_QUESTIONS_COUNT && words.length >= 5) {
        return {
          questions: questions.slice(0, TOPIC_GUIDANCE_QUESTIONS_COUNT),
          words: words.slice(0, TOPIC_GUIDANCE_WORDS_COUNT)
        };
      }
    } catch {
      return null;
    }

    return null;
  };

  const direct = tryParse(content);
  if (direct) {
    return direct;
  }

  for (const candidate of extractJsonCandidates(content)) {
    const extracted = tryParse(candidate);
    if (extracted) {
      return extracted;
    }
  }

  const lineCandidates = normalizeOllamaContent(content)
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);

  const questionLines = normalizeQuestions(
    lineCandidates.filter((line) => line.includes("?") || /^\d+\s*[\)\.\-:]/.test(line))
  );
  const wordLines = normalizeWords(lineCandidates.filter((line) => !line.includes("?")));

  if (questionLines.length >= TOPIC_GUIDANCE_QUESTIONS_COUNT && wordLines.length >= 5) {
    return {
      questions: questionLines.slice(0, TOPIC_GUIDANCE_QUESTIONS_COUNT),
      words: wordLines.slice(0, TOPIC_GUIDANCE_WORDS_COUNT)
    };
  }

  return null;
};

const createPrompt = (
  topic: string,
  refreshToken: string | null,
  englishLevel: EnglishLevel,
  interests: string[],
  avoidQuestions: string[],
  avoidWords: string[]
): string => {
  const formattedLevel = formatEnglishLevel(englishLevel);
  const levelGuidance = getEnglishLevelPromptGuidance(englishLevel);
  const parts = [
    `Topic: "${topic}".`,
    `Generate guidance for an English speaking practice session for level ${formattedLevel}.`,
    `Language difficulty: ${levelGuidance}`,
    `Return exactly ${TOPIC_GUIDANCE_QUESTIONS_COUNT} follow-up questions for speaking practice.`,
    `Return exactly ${TOPIC_GUIDANCE_WORDS_COUNT} useful words or short phrases connected to this topic.`,
    "Useful words must match the learner level and stay understandable for that level.",
    "All follow-up questions should be distinct in angle and not paraphrases of each other.",
    "Useful words should be diverse, not near-duplicates.",
    'Return only JSON with this exact shape: {"questions":["q1","q2","q3","q4","q5","q6","q7","q8","q9","q10"],"words":["w1","w2","w3","w4","w5","w6","w7","w8"]}.',
    "No markdown, no extra keys, no explanations."
  ];

  if (interests.length > 0) {
    parts.push(`Learner interests: ${interests.join(", ")}.`);
    parts.push("Keep questions and useful words relevant to these interests when possible.");
  }

  if (refreshToken) {
    parts.push(`Variation key: ${refreshToken}. Make a different set than previous outputs.`);
  }

  if (avoidQuestions.length > 0) {
    parts.push(`Do not reuse any of these previous follow-up questions: ${avoidQuestions.join(" | ")}.`);
  }

  if (avoidWords.length > 0) {
    parts.push(`Do not reuse any of these previous useful words/phrases: ${avoidWords.join(" | ")}.`);
  }

  return parts.join(" ");
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.topic-guidance.get", request);
  const startedAt = Date.now();
  const { searchParams } = new URL(request.url);
  const topic = searchParams.get("topic")?.trim() ?? "";
  const refreshToken = searchParams.get("refresh");
  const interests = normalizeInterests(searchParams.getAll("interest"));
  const avoidQuestionsRaw = normalizeQuestions(searchParams.getAll("avoidQuestion"));
  const avoidWordsRaw = normalizeWords(searchParams.getAll("avoidWord"));
  const avoidQuestions = normalizeAvoidList(avoidQuestionsRaw);
  const avoidWords = normalizeAvoidList(avoidWordsRaw);

  if (!topic) {
    logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "missing_topic" });
    return NextResponse.json({ error: "Query param `topic` is required." }, { status: 400 });
  }

  if (topic.length > 300) {
    logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "topic_too_long" });
    return NextResponse.json({ error: "Topic is too long." }, { status: 400 });
  }

  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  const user = await getUserBySessionToken(token);
  const requestedLevel = normalizeEnglishLevel(searchParams.get("level"), DEFAULT_ENGLISH_LEVEL);
  const englishLevel = user?.englishLevel ?? requestedLevel;
  const model = await resolveOllamaModelForUser(user?.id ?? null);
  const baseSeed = hashString(topic.toLowerCase());
  const interestsSeed = hashString(interests.join("|").toLowerCase());
  const levelSeed = hashString(englishLevel);
  const refreshSeed = refreshToken ? hashString(refreshToken) : 0;
  const seed = Math.abs((baseSeed * 131 + interestsSeed * 17 + levelSeed * 19 + refreshSeed) % 2_147_483_647);

  logger.info("request.start", {
    model,
    englishLevel,
    interestsCount: interests.length,
    avoidQuestionsCount: avoidQuestionsRaw.length,
    avoidWordsCount: avoidWordsRaw.length,
    hasRefreshToken: Boolean(refreshToken)
  });

  try {
    const maxAttempts = 3;

    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      const attemptNumber = attempt + 1;
      const attemptSeed = Math.abs((seed + (attempt + 1) * 7919) % 2_147_483_647);
      const response = await fetch(`${baseUrl}/api/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({
          model,
          stream: false,
          think: getOllamaThinkOption(model),
          messages: [
            {
              role: "system",
              content:
                "You generate concise speaking-practice guidance and must follow the output format exactly."
            },
            {
              role: "user",
              content: createPrompt(topic, refreshToken, englishLevel, interests, avoidQuestionsRaw, avoidWordsRaw)
            }
          ],
          options: {
            temperature: refreshToken ? 0.7 + attempt * 0.08 : 0.2 + attempt * 0.05,
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

      const guidance = parseTopicGuidance(content);
      if (!guidance) {
        logger.debug("ollama.parse_failed", { attempt: attemptNumber, model, contentLength: content.length });
        continue;
      }

      if (hasAnyOverlap(guidance.questions, avoidQuestions) || hasAnyOverlap(guidance.words, avoidWords)) {
        logger.debug("ollama.overlap_detected", { attempt: attemptNumber, model });
        continue;
      }

      logger.info("request.success", {
        status: 200,
        durationMs: elapsedMs(startedAt),
        model,
        attempt: attemptNumber,
        questionsCount: guidance.questions.length,
        wordsCount: guidance.words.length
      });
      return NextResponse.json(guidance, { status: 200 });
    }

    logger.warn("request.failed", {
      status: 502,
      durationMs: elapsedMs(startedAt),
      model,
      reason: "no_valid_generation"
    });
    return NextResponse.json(
      { error: "Could not generate sufficiently new guidance. Try regenerate again." },
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
