import { query } from "./db";

export const DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434";
export const DEFAULT_OLLAMA_MODEL = "gemma3:12b";

type UserModelRow = {
  preferred_ollama_model: string | null;
};

type OllamaTagModel = {
  name?: unknown;
  model?: unknown;
};

type OllamaTagsResponse = {
  models?: unknown;
};

const THINKING_MODEL_HINTS = [
  "deepseek-r1",
  "r1",
  "qwq",
  "reason",
  "thinking"
];

const normalizeModel = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const cleaned = value.trim();
  if (!cleaned || cleaned.length > 120) {
    return null;
  }

  return cleaned;
};

const uniqueModels = (models: string[]): string[] => {
  const seen = new Set<string>();
  const unique: string[] = [];

  for (const model of models) {
    const normalized = normalizeModel(model);
    if (!normalized) {
      continue;
    }

    const key = normalized.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    unique.push(normalized);
  }

  return unique;
};

export const getDefaultOllamaModel = (): string => {
  return normalizeModel(process.env.OLLAMA_MODEL) ?? DEFAULT_OLLAMA_MODEL;
};

export const getUserPreferredOllamaModel = async (userId: string): Promise<string | null> => {
  const result = await query<UserModelRow>(
    `SELECT preferred_ollama_model
     FROM users
     WHERE id = $1
     LIMIT 1`,
    [userId]
  );

  return normalizeModel(result.rows[0]?.preferred_ollama_model);
};

export const resolveOllamaModelForUser = async (userId?: string | null): Promise<string> => {
  const fallback = getDefaultOllamaModel();
  if (!userId) {
    return fallback;
  }

  const preferred = await getUserPreferredOllamaModel(userId);
  return preferred ?? fallback;
};

export const saveUserPreferredOllamaModel = async (userId: string, model: string): Promise<string> => {
  const normalized = normalizeModel(model);
  if (!normalized) {
    throw new Error("Model name is invalid.");
  }

  await query(
    `UPDATE users
     SET preferred_ollama_model = $2
     WHERE id = $1`,
    [userId, normalized]
  );

  return normalized;
};

export const fetchLocalOllamaModels = async (baseUrl: string): Promise<string[]> => {
  const response = await fetch(`${baseUrl}/api/tags`, {
    method: "GET",
    cache: "no-store"
  });

  if (!response.ok) {
    throw new Error(`Failed to list models (${response.status}).`);
  }

  const payload = (await response.json()) as OllamaTagsResponse;
  const rawModels = Array.isArray(payload.models) ? (payload.models as OllamaTagModel[]) : [];
  const names = rawModels
    .map((item) => {
      if (!item || typeof item !== "object") {
        return null;
      }
      return normalizeModel(item.name) ?? normalizeModel(item.model);
    })
    .filter((value): value is string => value !== null);

  return uniqueModels(names);
};

export const mergeModelList = (...lists: string[][]): string[] => {
  const flattened = lists.flat();
  return uniqueModels(flattened);
};

export const isThinkingModel = (model: string): boolean => {
  const normalized = model.trim().toLowerCase();
  return THINKING_MODEL_HINTS.some((hint) => normalized.includes(hint));
};

const stripThinkingBlocks = (value: string): string => {
  return value
    .replace(/<think>[\s\S]*?<\/think>/gi, " ")
    .replace(/<thinking>[\s\S]*?<\/thinking>/gi, " ");
};

const stripMarkdownFence = (value: string): string => {
  const trimmed = value.trim();
  if (!trimmed.startsWith("```")) {
    return trimmed;
  }

  const noStartFence = trimmed.replace(/^```[a-zA-Z0-9_-]*\s*\n?/, "");
  return noStartFence.replace(/\n?```$/, "").trim();
};

export const normalizeOllamaContent = (value: string): string => {
  const noThinking = stripThinkingBlocks(value);
  return stripMarkdownFence(noThinking).trim();
};

const extractJsonObjects = (value: string): string[] => {
  const objects: string[] = [];
  let depth = 0;
  let start = -1;
  let inString = false;
  let escaped = false;

  for (let index = 0; index < value.length; index += 1) {
    const char = value[index];

    if (inString) {
      if (escaped) {
        escaped = false;
        continue;
      }

      if (char === "\\") {
        escaped = true;
        continue;
      }

      if (char === "\"") {
        inString = false;
      }

      continue;
    }

    if (char === "\"") {
      inString = true;
      continue;
    }

    if (char === "{") {
      if (depth === 0) {
        start = index;
      }
      depth += 1;
      continue;
    }

    if (char === "}" && depth > 0) {
      depth -= 1;
      if (depth === 0 && start >= 0) {
        objects.push(value.slice(start, index + 1));
        start = -1;
      }
    }
  }

  return objects;
};

export const extractJsonCandidates = (content: string): string[] => {
  const normalized = normalizeOllamaContent(content);
  const candidates: string[] = [];

  if (normalized) {
    candidates.push(normalized);
  }

  for (const jsonObject of extractJsonObjects(normalized)) {
    if (!candidates.includes(jsonObject)) {
      candidates.push(jsonObject);
    }
  }

  return candidates;
};
