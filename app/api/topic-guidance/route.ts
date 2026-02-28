import { NextResponse } from "next/server";

export const dynamic = "force-dynamic";

const DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434";
const DEFAULT_OLLAMA_MODEL = "gemma3:12b";

type OllamaChatResponse = {
  message?: {
    content?: string;
  };
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

      if (questions.length >= 3 && words.length >= 5) {
        return {
          questions: questions.slice(0, 3),
          words: words.slice(0, 8)
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

  const jsonCandidate = content.match(/\{[\s\S]*\}/);
  if (jsonCandidate) {
    const extracted = tryParse(jsonCandidate[0]);
    if (extracted) {
      return extracted;
    }
  }

  const lineCandidates = content
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);

  const questionLines = normalizeQuestions(
    lineCandidates.filter((line) => line.includes("?") || /^\d+\s*[\)\.\-:]/.test(line))
  );
  const wordLines = normalizeWords(lineCandidates.filter((line) => !line.includes("?")));

  if (questionLines.length >= 3 && wordLines.length >= 5) {
    return {
      questions: questionLines.slice(0, 3),
      words: wordLines.slice(0, 8)
    };
  }

  return null;
};

const createPrompt = (topic: string, refreshToken: string | null): string => {
  const parts = [
    `Topic: "${topic}".`,
    "Generate guidance for an English speaking practice session (B1-B2).",
    "Return exactly 3 follow-up questions for speaking practice.",
    "Return exactly 8 useful words or short phrases connected to this topic.",
    'Return only JSON with this exact shape: {"questions":["q1","q2","q3"],"words":["w1","w2","w3","w4","w5","w6","w7","w8"]}.',
    "No markdown, no extra keys, no explanations."
  ];

  if (refreshToken) {
    parts.push(`Variation key: ${refreshToken}. Make a different set than previous outputs.`);
  }

  return parts.join(" ");
};

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const topic = searchParams.get("topic")?.trim() ?? "";
  const refreshToken = searchParams.get("refresh");

  if (!topic) {
    return NextResponse.json({ error: "Query param `topic` is required." }, { status: 400 });
  }

  if (topic.length > 300) {
    return NextResponse.json({ error: "Topic is too long." }, { status: 400 });
  }

  const baseUrl = process.env.OLLAMA_BASE_URL ?? DEFAULT_OLLAMA_BASE_URL;
  const model = process.env.OLLAMA_MODEL ?? DEFAULT_OLLAMA_MODEL;
  const baseSeed = hashString(topic.toLowerCase());
  const refreshSeed = refreshToken ? hashString(refreshToken) : 0;
  const seed = Math.abs((baseSeed * 131 + refreshSeed) % 2_147_483_647);
  const temperature = refreshToken ? 0.65 : 0.2;

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
              "You generate concise speaking-practice guidance and must follow the output format exactly."
          },
          {
            role: "user",
            content: createPrompt(topic, refreshToken)
          }
        ],
        options: {
          temperature,
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

    const guidance = parseTopicGuidance(content);
    if (!guidance) {
      return NextResponse.json(
        { error: "Could not parse follow-up questions and useful words from Ollama response." },
        { status: 502 }
      );
    }

    return NextResponse.json(guidance, { status: 200 });
  } catch {
    return NextResponse.json(
      { error: "Cannot connect to local Ollama. Check OLLAMA_BASE_URL and running Ollama service." },
      { status: 502 }
    );
  }
}
