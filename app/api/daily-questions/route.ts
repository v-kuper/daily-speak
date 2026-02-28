import { NextResponse } from "next/server";

export const dynamic = "force-dynamic";

const DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434";
const DEFAULT_OLLAMA_MODEL = "gemma3:12b";
const DATE_KEY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
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

    const key = cleaned.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    unique.push(cleaned.endsWith("?") ? cleaned : `${cleaned}?`);
  }

  return unique.slice(0, 3);
};

const parseQuestions = (content: string): ParsedQuestions | null => {
  try {
    const direct = JSON.parse(content) as { questions?: unknown };
    if (Array.isArray(direct.questions)) {
      const normalized = normalizeQuestions(
        direct.questions.filter((item): item is string => typeof item === "string")
      );
      if (normalized.length === 3) {
        return { questions: normalized };
      }
    }
  } catch {
    // Fall through to extraction.
  }

  const jsonCandidate = content.match(/\{[\s\S]*\}/);
  if (jsonCandidate) {
    try {
      const extracted = JSON.parse(jsonCandidate[0]) as { questions?: unknown };
      if (Array.isArray(extracted.questions)) {
        const normalized = normalizeQuestions(
          extracted.questions.filter((item): item is string => typeof item === "string")
        );
        if (normalized.length === 3) {
          return { questions: normalized };
        }
      }
    } catch {
      // Fall through to line parsing.
    }
  }

  const lineCandidates = content
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
  const normalized = normalizeQuestions(lineCandidates);
  if (normalized.length === 3) {
    return { questions: normalized };
  }

  return null;
};

const createPrompt = (dateKey: string, refreshToken: string | null, interests: string[]): string => {
  const parts = [
    `Generate exactly 3 daily English speaking practice questions for ${dateKey}.`,
    "Audience: intermediate learner (A2-B1).",
    "Questions must be short, practical, and suitable for a 1-3 minute spoken answer.",
    'Return only JSON with this exact shape: {"questions":["question 1","question 2","question 3"]}.',
    "Do not add markdown, explanations, numbering, or extra keys."
  ];

  if (interests.length > 0) {
    parts.push(`User interests: ${interests.join(", ")}.`);
    parts.push("Personalize each question using these interests.");
  }

  if (refreshToken) {
    parts.push(
      `Variation key: ${refreshToken}. Return a different set than earlier generations for the same date.`
    );
  }

  return parts.join(" ");
};

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const dateKey = searchParams.get("date");
  const refreshToken = searchParams.get("refresh");
  const interests = normalizeInterests(searchParams.getAll("interest"));

  if (!dateKey || !DATE_KEY_PATTERN.test(dateKey)) {
    return NextResponse.json({ error: "Query param `date` must be in YYYY-MM-DD format." }, { status: 400 });
  }

  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const model = process.env.OLLAMA_MODEL ?? DEFAULT_OLLAMA_MODEL;
  const dateSeed = Number.parseInt(dateKey.replaceAll("-", ""), 10);
  const interestsSeed = hashString(interests.join("|").toLowerCase());
  const refreshSeed = refreshToken ? Number.parseInt(refreshToken.replace(/\D/g, ""), 10) : Number.NaN;
  const hasRefreshSeed = Number.isFinite(refreshSeed);
  const seed = hasRefreshSeed
    ? Math.abs((dateSeed * 131 + interestsSeed * 17 + refreshSeed) % 2_147_483_647)
    : Math.abs((dateSeed * 131 + interestsSeed * 17) % 2_147_483_647);

  try {
    const response = await fetch(`${baseUrl}/api/chat`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        model,
        stream: false,
        messages: [
          {
            role: "system",
            content:
              "You are an assistant that generates concise speaking-practice questions and always follows output format exactly."
          },
          {
            role: "user",
            content: createPrompt(dateKey, refreshToken, interests)
          }
        ],
        options: {
          temperature: hasRefreshSeed ? 0.65 : 0.2,
          seed
        }
      }),
      cache: "no-store"
    });

    if (!response.ok) {
      const text = await response.text();
      return NextResponse.json(
        { error: `Ollama request failed (${response.status}): ${text.slice(0, 300)}` },
        { status: 502 }
      );
    }

    const payload = (await response.json()) as OllamaChatResponse;
    const content = payload.message?.content?.trim();
    if (!content) {
      return NextResponse.json({ error: "Ollama returned an empty response." }, { status: 502 });
    }

    const parsed = parseQuestions(content);
    if (!parsed) {
      return NextResponse.json(
        { error: "Could not parse 3 valid questions from Ollama response." },
        { status: 502 }
      );
    }

    return NextResponse.json(parsed, { status: 200 });
  } catch {
    return NextResponse.json(
      { error: "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service." },
      { status: 502 }
    );
  }
}
