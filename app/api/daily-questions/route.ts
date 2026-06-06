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

const DATE_KEY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const DAILY_QUESTIONS_COUNT = 3;

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
  response?: string;
};

type ParsedQuestions = {
  questions: string[];
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

const normalizeQuestions = (items: string[], limit = DAILY_QUESTIONS_COUNT): string[] => {
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

    const key = cleaned.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    unique.push(cleaned.endsWith("?") ? cleaned : `${cleaned}?`);
  }

  return unique.slice(0, limit);
};

const normalizeAvoidQuestions = (items: string[]): string[] => {
  const normalized = normalizeQuestions(items);
  return Array.from(new Set(normalized.map((item) => item.toLowerCase())));
};

const hasQuestionOverlap = (questions: string[], avoidLowerCase: string[]): boolean => {
  if (avoidLowerCase.length === 0) {
    return false;
  }

  const avoidSet = new Set(avoidLowerCase);
  return questions.some((question) => avoidSet.has(question.toLowerCase()));
};

const parseQuestions = (content: string): ParsedQuestions | null => {
  const parseFromJson = (candidate: string): ParsedQuestions | null => {
    try {
      const data = JSON.parse(candidate) as { questions?: unknown };
      if (!Array.isArray(data.questions)) {
        return null;
      }

      const normalized = normalizeQuestions(data.questions.filter((item): item is string => typeof item === "string"));
      if (normalized.length !== DAILY_QUESTIONS_COUNT) {
        return null;
      }

      return { questions: normalized };
    } catch {
      return null;
    }
  };

  for (const candidate of extractJsonCandidates(content)) {
    const parsed = parseFromJson(candidate);
    if (parsed) {
      return parsed;
    }
  }

  const lineCandidates = normalizeOllamaContent(content)
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
  const normalized = normalizeQuestions(lineCandidates);
  if (normalized.length === DAILY_QUESTIONS_COUNT) {
    return { questions: normalized };
  }

  return null;
};

const createPrompt = (
  dateKey: string,
  refreshToken: string | null,
  englishLevel: EnglishLevel,
  interests: string[],
  avoidQuestions: string[]
): string => {
  const formattedLevel = formatEnglishLevel(englishLevel);
  const levelGuidance = getEnglishLevelPromptGuidance(englishLevel);
  const parts = [
    `Generate exactly ${DAILY_QUESTIONS_COUNT} daily English speaking practice questions for ${dateKey}.`,
    `Audience: English learner level ${formattedLevel}.`,
    `Language difficulty: ${levelGuidance}`,
    "Questions must be short, practical, and suitable for a 1-3 minute spoken answer.",
    "Each question must be clearly tied to a concrete theme, not a generic life question.",
    "The theme should be explicit in the wording of each question.",
    `All ${DAILY_QUESTIONS_COUNT} questions must be semantically different from each other.`,
    'Return only JSON with this exact shape: {"questions":["question 1","question 2","question 3"]}.',
    "Do not add markdown, explanations, numbering, or extra keys."
  ];

  if (interests.length > 0) {
    parts.push(`User interests (use as themes): ${interests.join(", ")}.`);
    parts.push("Each question must map to one of these interests and mention that theme explicitly.");
    parts.push(`Use different interests across the ${DAILY_QUESTIONS_COUNT} questions when possible.`);
    parts.push("Do not generate off-topic or generic questions unrelated to the listed interests.");
  }

  if (refreshToken) {
    parts.push(
      `Variation key: ${refreshToken}. Return a different set than earlier generations for the same date.`
    );
  }

  if (avoidQuestions.length > 0) {
    parts.push(`Do not reuse any of these previous questions: ${avoidQuestions.join(" | ")}.`);
    parts.push("If a candidate is similar, replace it with a new angle.");
  }

  return parts.join(" ");
};

export async function GET(request: NextRequest) {
  const logger = createRouteLogger("api.daily-questions.get", request);
  const startedAt = Date.now();
  const { searchParams } = new URL(request.url);
  const dateKey = searchParams.get("date");
  const refreshToken = searchParams.get("refresh");
  const interests = normalizeInterests(searchParams.getAll("interest"));
  const avoidQuestionsRaw = searchParams.getAll("avoid");
  const avoidQuestions = normalizeQuestions(avoidQuestionsRaw);
  const avoidQuestionsLowerCase = normalizeAvoidQuestions(avoidQuestionsRaw);

  if (!dateKey || !DATE_KEY_PATTERN.test(dateKey)) {
    logger.warn("request.rejected", { status: 400, durationMs: elapsedMs(startedAt), reason: "invalid_date" });
    return NextResponse.json({ error: "Query param `date` must be in YYYY-MM-DD format." }, { status: 400 });
  }

  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const token = request.cookies.get(SESSION_COOKIE_NAME)?.value;
  const user = await getUserBySessionToken(token);
  const requestedLevel = normalizeEnglishLevel(searchParams.get("level"), DEFAULT_ENGLISH_LEVEL);
  const englishLevel = user?.englishLevel ?? requestedLevel;
  const ollamaSettings = await resolveOllamaSettingsForUser();
  const model = ollamaSettings.model;
  const dateSeed = Number.parseInt(dateKey.replaceAll("-", ""), 10);
  const interestsSeed = hashString(interests.join("|").toLowerCase());
  const levelSeed = hashString(englishLevel);
  const refreshSeed = refreshToken ? Number.parseInt(refreshToken.replace(/\D/g, ""), 10) : Number.NaN;
  const hasRefreshSeed = Number.isFinite(refreshSeed);
  const seed = hasRefreshSeed
    ? Math.abs((dateSeed * 131 + interestsSeed * 17 + levelSeed * 19 + refreshSeed) % 2_147_483_647)
    : Math.abs((dateSeed * 131 + interestsSeed * 17 + levelSeed * 19) % 2_147_483_647);

  logger.info("request.start", {
    dateKey,
    model,
    englishLevel,
    interestsCount: interests.length,
    avoidCount: avoidQuestions.length,
    hasRefreshSeed
  });

  try {
    const maxAttempts = 3;

    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      const attemptNumber = attempt + 1;
      const attemptSeed = Math.abs((seed + (attempt + 1) * 9973) % 2_147_483_647);
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
              content:
                "You are an assistant that generates concise speaking-practice questions and always follows output format exactly."
            },
            {
              role: "user",
              content: createPrompt(dateKey, refreshToken, englishLevel, interests, avoidQuestions)
            }
          ],
          options: {
            temperature: hasRefreshSeed ? 0.7 + attempt * 0.08 : 0.2 + attempt * 0.05,
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

      const parsed = parseQuestions(content);
      if (!parsed) {
        logger.debug("ollama.parse_failed", { attempt: attemptNumber, model, contentLength: content.length });
        continue;
      }

      if (hasQuestionOverlap(parsed.questions, avoidQuestionsLowerCase)) {
        logger.debug("ollama.overlap_detected", { attempt: attemptNumber, model });
        continue;
      }

      logger.info("request.success", {
        status: 200,
        durationMs: elapsedMs(startedAt),
        model,
        attempt: attemptNumber
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
      { error: "Could not generate a sufficiently new set of questions. Try regenerate again." },
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
