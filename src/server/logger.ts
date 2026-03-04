import { randomUUID } from "node:crypto";

type LogLevel = "debug" | "info" | "warn" | "error";

type LogMeta = Record<string, unknown>;

type RequestLike = {
  method?: string;
  headers?: {
    get(name: string): string | null;
  };
  nextUrl?: {
    pathname?: string;
  };
};

const LEVEL_WEIGHT: Record<LogLevel, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40
};

const COLOR_BY_LEVEL: Record<LogLevel, string> = {
  debug: "\u001b[90m",
  info: "\u001b[36m",
  warn: "\u001b[33m",
  error: "\u001b[31m"
};

const COLOR_RESET = "\u001b[0m";
const MAX_META_LENGTH = 2400;

const normalizeLevel = (value: string | undefined): LogLevel | null => {
  if (!value) {
    return null;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "debug" || normalized === "info" || normalized === "warn" || normalized === "error") {
    return normalized;
  }

  return null;
};

const resolveMinLevel = (): LogLevel => {
  const fromEnv = normalizeLevel(process.env.SERVER_LOG_LEVEL);
  if (fromEnv) {
    return fromEnv;
  }

  return process.env.NODE_ENV === "development" ? "debug" : "info";
};

const minLevel = resolveMinLevel();

const shouldLog = (level: LogLevel): boolean => {
  return LEVEL_WEIGHT[level] >= LEVEL_WEIGHT[minLevel];
};

const stripControlChars = (value: string): string => {
  return value.replace(/[\u0000-\u001f\u007f]/g, " ").trim();
};

const safeJsonStringify = (value: unknown): string => {
  const seen = new WeakSet<object>();
  return JSON.stringify(
    value,
    (_key, currentValue: unknown) => {
      if (currentValue instanceof Error) {
        return {
          name: currentValue.name,
          message: stripControlChars(currentValue.message),
          stack: typeof currentValue.stack === "string" ? stripControlChars(currentValue.stack).slice(0, 1200) : undefined
        };
      }

      if (typeof currentValue === "bigint") {
        return currentValue.toString();
      }

      if (typeof currentValue === "string") {
        return stripControlChars(currentValue).slice(0, 600);
      }

      if (typeof currentValue === "object" && currentValue !== null) {
        if (seen.has(currentValue)) {
          return "[Circular]";
        }
        seen.add(currentValue);
      }

      return currentValue;
    },
    0
  );
};

const compactMeta = (meta: LogMeta): LogMeta => {
  const entries = Object.entries(meta).filter(([, value]) => typeof value !== "undefined");
  return Object.fromEntries(entries);
};

const formatMeta = (meta?: LogMeta): string => {
  if (!meta) {
    return "";
  }

  const compacted = compactMeta(meta);
  if (Object.keys(compacted).length === 0) {
    return "";
  }

  const json = safeJsonStringify(compacted);
  return json.length > MAX_META_LENGTH ? `${json.slice(0, MAX_META_LENGTH)}...` : json;
};

const formatTimestamp = (): string => {
  return new Date().toISOString();
};

const formatLevel = (level: LogLevel): string => {
  const label = level.toUpperCase().padEnd(5, " ");
  const useColor = Boolean(process.stdout.isTTY) && process.env.NO_COLOR !== "1";
  if (!useColor) {
    return label;
  }

  return `${COLOR_BY_LEVEL[level]}${label}${COLOR_RESET}`;
};

const write = (level: LogLevel, scope: string, message: string, meta?: LogMeta): void => {
  if (!shouldLog(level)) {
    return;
  }

  const levelLabel = formatLevel(level);
  const cleanedMessage = stripControlChars(message);
  const metaPart = formatMeta(meta);
  const line = `${formatTimestamp()} ${levelLabel} [${scope}] ${cleanedMessage}${metaPart ? ` ${metaPart}` : ""}`;

  if (level === "error") {
    console.error(line);
    return;
  }
  if (level === "warn") {
    console.warn(line);
    return;
  }

  console.log(line);
};

export const toErrorMeta = (error: unknown): LogMeta => {
  if (error instanceof Error) {
    return {
      errorName: error.name,
      errorMessage: error.message,
      errorStack: error.stack?.split("\n").slice(0, 6).join(" | ")
    };
  }

  return {
    errorMessage: String(error)
  };
};

type RouteLogger = {
  requestId: string;
  debug: (message: string, meta?: LogMeta) => void;
  info: (message: string, meta?: LogMeta) => void;
  warn: (message: string, meta?: LogMeta) => void;
  error: (message: string, meta?: LogMeta) => void;
};

export const createRouteLogger = (scope: string, request?: RequestLike): RouteLogger => {
  const headerRequestId = request?.headers?.get("x-request-id")?.trim();
  const requestId = headerRequestId && headerRequestId.length > 0 ? headerRequestId.slice(0, 64) : randomUUID().slice(0, 8);
  const baseMeta: LogMeta = {
    requestId,
    method: request?.method,
    path: request?.nextUrl?.pathname
  };

  const withBase = (meta?: LogMeta): LogMeta => {
    if (!meta) {
      return baseMeta;
    }
    return { ...baseMeta, ...meta };
  };

  return {
    requestId,
    debug: (message, meta) => write("debug", scope, message, withBase(meta)),
    info: (message, meta) => write("info", scope, message, withBase(meta)),
    warn: (message, meta) => write("warn", scope, message, withBase(meta)),
    error: (message, meta) => write("error", scope, message, withBase(meta))
  };
};

export const elapsedMs = (startedAt: number): number => {
  return Math.max(0, Date.now() - startedAt);
};
