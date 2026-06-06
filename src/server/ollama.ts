export const DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434";
export const DEFAULT_OLLAMA_MODEL = "gemma4:31b-cloud";
const DEFAULT_OLLAMA_IS_THINKING_MODEL = true;

export type OllamaUserSettings = {
  model: string;
  isThinkingModel: boolean;
};

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

export const getDefaultOllamaModel = (): string => {
  return normalizeModel(process.env.OLLAMA_MODEL) ?? DEFAULT_OLLAMA_MODEL;
};

export const getDefaultOllamaIsThinkingModel = (): boolean => {
  const rawValue = process.env.OLLAMA_THINKING_MODEL;
  if (typeof rawValue !== "string") {
    return DEFAULT_OLLAMA_IS_THINKING_MODEL;
  }

  const normalized = rawValue.trim().toLowerCase();
  if (!normalized) {
    return DEFAULT_OLLAMA_IS_THINKING_MODEL;
  }

  return normalized === "1" || normalized === "true" || normalized === "yes" || normalized === "on";
};

export const resolveOllamaSettingsForUser = async (): Promise<OllamaUserSettings> => {
  return {
    model: getDefaultOllamaModel(),
    isThinkingModel: getDefaultOllamaIsThinkingModel()
  };
};

export const getOllamaThinkOption = (isThinkingModel: boolean): boolean => {
  return isThinkingModel;
};

type OllamaContentCarrier = {
  message?: {
    content?: unknown;
  };
  response?: unknown;
};

const asContentText = (value: unknown): string => {
  if (typeof value !== "string") {
    return "";
  }

  return value.trim();
};

export const extractOllamaMessageContent = (payload: OllamaContentCarrier): string => {
  const messageContent = asContentText(payload?.message?.content);
  if (messageContent) {
    return messageContent;
  }

  return asContentText(payload?.response);
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
