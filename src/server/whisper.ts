import { spawn } from "node:child_process";
import { constants as fsConstants } from "node:fs";
import { access, mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const DEFAULT_WHISPER_ROOT = path.join(process.cwd(), "tools", "whisper");
const DEFAULT_WHISPER_TIMEOUT_MS = 180_000;
const MAX_TRANSCRIPT_LENGTH = 20_000;
const DEFAULT_OPENAI_MODEL = "base.en";
const DEFAULT_OPENAI_MODEL_DIR = path.join(DEFAULT_WHISPER_ROOT, "openai-models");
const DEFAULT_OPENAI_CACHE_DIR = path.join(DEFAULT_WHISPER_ROOT, "cache");
const DEFAULT_LOCAL_FFMPEG_BIN = path.join(process.cwd(), "tools", "ffmpeg", "bin", "ffmpeg");

type WhisperBackend = "auto" | "cpp" | "openai";

export class WhisperTranscriptionError extends Error {
  status: number;

  constructor(message: string, status = 502) {
    super(message);
    this.status = status;
  }
}

const normalizePathEnv = (value: string | undefined): string | null => {
  if (!value) {
    return null;
  }

  const normalized = value.trim();
  if (!normalized) {
    return null;
  }

  return path.isAbsolute(normalized) ? normalized : path.join(process.cwd(), normalized);
};

const normalizeCommandEnv = (value: string | undefined): string | null => {
  if (!value) {
    return null;
  }

  const normalized = value.trim();
  if (!normalized) {
    return null;
  }

  if (path.isAbsolute(normalized)) {
    return normalized;
  }

  if (normalized.startsWith(".") || normalized.includes("/") || normalized.includes("\\")) {
    return path.join(process.cwd(), normalized);
  }

  return normalized;
};

const normalizeLanguage = (value: string | undefined): string => {
  const normalized = value?.trim().toLowerCase() ?? "";
  if (!normalized) {
    return "en";
  }

  if (!/^[a-z]{2,12}$/.test(normalized)) {
    return "en";
  }

  return normalized;
};

const resolveTimeoutMs = (): number => {
  const parsed = Number.parseInt(process.env.WHISPER_TIMEOUT_MS ?? "", 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return DEFAULT_WHISPER_TIMEOUT_MS;
  }
  return Math.min(parsed, 900_000);
};

const resolveBackend = (): WhisperBackend => {
  const raw = (process.env.WHISPER_BACKEND ?? "auto").trim().toLowerCase();
  if (raw === "cpp" || raw === "whisper.cpp" || raw === "whispercpp") {
    return "cpp";
  }
  if (raw === "openai" || raw === "python" || raw === "whisper") {
    return "openai";
  }
  return "auto";
};

const resolveThreads = (): number => {
  const parsed = Number.parseInt(process.env.WHISPER_THREADS ?? "", 10);
  if (Number.isFinite(parsed) && parsed > 0) {
    return Math.min(parsed, 16);
  }

  const available = typeof os.availableParallelism === "function" ? os.availableParallelism() : os.cpus().length;
  return Math.max(1, Math.min(available, 8));
};

const resolveOpenAIModel = (): string => {
  const normalized = process.env.WHISPER_OPENAI_MODEL?.trim();
  if (!normalized) {
    return DEFAULT_OPENAI_MODEL;
  }
  return normalized.slice(0, 80);
};

const resolveOpenAIDevice = (): string | null => {
  const normalized = process.env.WHISPER_OPENAI_DEVICE?.trim().toLowerCase() ?? "";
  if (!normalized) {
    return null;
  }
  if (!/^[a-z0-9:_-]{2,32}$/.test(normalized)) {
    return null;
  }
  return normalized;
};

const resolveOpenAIFp16 = (): string => {
  const normalized = process.env.WHISPER_OPENAI_FP16?.trim().toLowerCase() ?? "";
  return normalized === "1" || normalized === "true" ? "True" : "False";
};

const resolveOpenAIModelDir = (): string => {
  const envPath = normalizePathEnv(process.env.WHISPER_OPENAI_MODEL_DIR);
  return envPath ?? DEFAULT_OPENAI_MODEL_DIR;
};

const resolveOpenAICacheDir = (): string => {
  const envPath = normalizePathEnv(process.env.WHISPER_OPENAI_CACHE_DIR);
  return envPath ?? DEFAULT_OPENAI_CACHE_DIR;
};

const resolveFfmpegBinaryPath = (): string => {
  const envPath = normalizePathEnv(process.env.WHISPER_FFMPEG_BIN);
  return envPath ?? DEFAULT_LOCAL_FFMPEG_BIN;
};

const getCppBinaryCandidates = (): string[] => {
  const envPath = normalizePathEnv(process.env.WHISPER_BINARY_PATH);
  const candidates = [
    envPath,
    path.join(DEFAULT_WHISPER_ROOT, "bin", "whisper-cli"),
    path.join(DEFAULT_WHISPER_ROOT, "bin", "main"),
    path.join(process.cwd(), "tools", "whisper.cpp", "build", "bin", "whisper-cli"),
    path.join(process.cwd(), "tools", "whisper.cpp", "main")
  ];

  if (process.platform === "win32") {
    candidates.push(
      path.join(DEFAULT_WHISPER_ROOT, "bin", "whisper-cli.exe"),
      path.join(DEFAULT_WHISPER_ROOT, "bin", "main.exe"),
      path.join(process.cwd(), "tools", "whisper.cpp", "build", "bin", "whisper-cli.exe"),
      path.join(process.cwd(), "tools", "whisper.cpp", "main.exe")
    );
  }

  return candidates.filter((value): value is string => Boolean(value));
};

const getCppModelCandidates = (): string[] => {
  const envPath = normalizePathEnv(process.env.WHISPER_MODEL_PATH);
  return [
    envPath,
    path.join(DEFAULT_WHISPER_ROOT, "models", "ggml-base.en.bin"),
    path.join(DEFAULT_WHISPER_ROOT, "models", "ggml-base.bin"),
    path.join(DEFAULT_WHISPER_ROOT, "models", "ggml-small.en.bin"),
    path.join(process.cwd(), "tools", "whisper.cpp", "models", "ggml-base.en.bin"),
    path.join(process.cwd(), "tools", "whisper.cpp", "models", "ggml-base.bin")
  ].filter((value): value is string => Boolean(value));
};

const canAccess = async (candidatePath: string, mode: number): Promise<boolean> => {
  try {
    await access(candidatePath, mode);
    return true;
  } catch {
    return false;
  }
};

const resolveCppBinaryPath = async (): Promise<string> => {
  const envPath = normalizePathEnv(process.env.WHISPER_BINARY_PATH);
  const mode = process.platform === "win32" ? fsConstants.F_OK : fsConstants.X_OK;

  if (envPath) {
    if (await canAccess(envPath, mode)) {
      return envPath;
    }

    throw new WhisperTranscriptionError(
      `WHISPER_BINARY_PATH is set but file is not executable: ${envPath}.`,
      500
    );
  }

  for (const candidate of getCppBinaryCandidates()) {
    if (await canAccess(candidate, mode)) {
      return candidate;
    }
  }

  throw new WhisperTranscriptionError(
    "Local Whisper binary not found. Place whisper-cli in tools/whisper/bin or set WHISPER_BINARY_PATH.",
    500
  );
};

const resolveCppModelPath = async (): Promise<string> => {
  const envPath = normalizePathEnv(process.env.WHISPER_MODEL_PATH);

  if (envPath) {
    if (await canAccess(envPath, fsConstants.F_OK)) {
      return envPath;
    }

    throw new WhisperTranscriptionError(`WHISPER_MODEL_PATH is set but file is missing: ${envPath}.`, 500);
  }

  for (const candidate of getCppModelCandidates()) {
    if (await canAccess(candidate, fsConstants.F_OK)) {
      return candidate;
    }
  }

  throw new WhisperTranscriptionError(
    "Whisper model not found. Place ggml model in tools/whisper/models or set WHISPER_MODEL_PATH.",
    500
  );
};

type CommandOutput = {
  stdout: string;
  stderr: string;
};

type RunCommandOptions = {
  env?: NodeJS.ProcessEnv;
};

const runCommand = async (
  command: string,
  args: string[],
  timeoutMs: number,
  options: RunCommandOptions = {}
): Promise<CommandOutput> => {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      stdio: ["ignore", "pipe", "pipe"],
      env: options.env ?? process.env
    });

    let stdout = "";
    let stderr = "";
    let finished = false;

    const complete = (handler: () => void) => {
      if (finished) {
        return;
      }
      finished = true;
      clearTimeout(timer);
      handler();
    };

    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      complete(() => {
        reject(new WhisperTranscriptionError(`Whisper command timed out after ${timeoutMs}ms.`, 504));
      });
    }, timeoutMs);

    child.stdout.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
    });

    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    });

    child.on("error", (error) => {
      complete(() => {
        reject(new WhisperTranscriptionError(`Whisper command failed to start: ${error.message}`, 500));
      });
    });

    child.on("close", (code, signal) => {
      complete(() => {
        if (code === 0) {
          resolve({ stdout, stderr });
          return;
        }

        const details = stderr.trim() || stdout.trim() || `code=${code ?? "null"} signal=${signal ?? "null"}`;
        reject(new WhisperTranscriptionError(`Whisper transcription failed: ${details.slice(0, 500)}`, 502));
      });
    });
  });
};

const dedupe = (values: string[]): string[] => {
  const seen = new Set<string>();
  const result: string[] = [];

  for (const value of values) {
    if (!value) {
      continue;
    }
    const key = process.platform === "win32" ? value.toLowerCase() : value;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(value);
  }

  return result;
};

const getPythonCandidates = (): string[] => {
  const envPython = normalizeCommandEnv(process.env.WHISPER_PYTHON_BIN);
  const venvPython =
    process.platform === "win32"
      ? path.join(process.cwd(), ".venv", "Scripts", "python.exe")
      : path.join(process.cwd(), ".venv", "bin", "python");
  const projectVenvPython =
    process.platform === "win32"
      ? path.join(process.cwd(), "venv", "Scripts", "python.exe")
      : path.join(process.cwd(), "venv", "bin", "python");
  const defaults = process.platform === "win32" ? ["python"] : ["python3", "python"];

  return dedupe([envPython, venvPython, projectVenvPython, ...defaults].filter((value): value is string => Boolean(value)));
};

const isCommandNotFound = (message: string): boolean => {
  return /\bENOENT\b/i.test(message) || /not found/i.test(message);
};

const isWhisperModuleMissing = (message: string): boolean => {
  return /No module named whisper/i.test(message) || /ModuleNotFoundError/i.test(message);
};

const isFfmpegMissing = (message: string): boolean => {
  return /ffmpeg/i.test(message) && /(not found|No such file|required|install)/i.test(message);
};

const prependPath = (basePath: string | undefined, prependDir: string): string => {
  if (!basePath) {
    return prependDir;
  }
  const separator = process.platform === "win32" ? ";" : ":";
  return `${prependDir}${separator}${basePath}`;
};

const buildWhisperCommandEnv = async (): Promise<NodeJS.ProcessEnv> => {
  const env: NodeJS.ProcessEnv = { ...process.env };
  const ffmpegPath = resolveFfmpegBinaryPath();
  const ffmpegDir = path.dirname(ffmpegPath);
  const cacheDir = resolveOpenAICacheDir();

  await mkdir(cacheDir, { recursive: true });

  if (await canAccess(ffmpegPath, process.platform === "win32" ? fsConstants.F_OK : fsConstants.X_OK)) {
    env.PATH = prependPath(env.PATH, ffmpegDir);
  }

  if (!env.XDG_CACHE_HOME) {
    env.XDG_CACHE_HOME = cacheDir;
  }
  if (!env.TRANSFORMERS_CACHE) {
    env.TRANSFORMERS_CACHE = path.join(cacheDir, "transformers");
  }
  if (!env.HF_HOME) {
    env.HF_HOME = path.join(cacheDir, "hf");
  }
  if (!env.IMAGEIO_USERDIR) {
    env.IMAGEIO_USERDIR = path.join(process.cwd(), "tools", "ffmpeg");
  }
  env.PYTHONUTF8 = "1";

  return env;
};

const normalizeTranscript = (raw: string): string => {
  return raw
    .replace(/\[[0-9:.]+\s*-->\s*[0-9:.]+\]/g, " ")
    .replace(/\r?\n+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, MAX_TRANSCRIPT_LENGTH);
};

const transcribeWithCppBackend = async (audioFilePath: string): Promise<string> => {
  const binaryPath = await resolveCppBinaryPath();
  const modelPath = await resolveCppModelPath();
  const language = normalizeLanguage(process.env.WHISPER_LANGUAGE);
  const timeoutMs = resolveTimeoutMs();
  const threads = resolveThreads();
  const tempDirectory = await mkdtemp(path.join(os.tmpdir(), "daily-whisper-"));
  const outputPrefix = path.join(tempDirectory, "transcript");
  const transcriptPath = `${outputPrefix}.txt`;

  const args = ["-m", modelPath, "-f", audioFilePath, "-l", language, "-t", String(threads), "-otxt", "-of", outputPrefix];

  try {
    const output = await runCommand(binaryPath, args, timeoutMs);
    const transcriptFromFile = await readFile(transcriptPath, "utf8").catch(() => "");
    const transcript = normalizeTranscript(transcriptFromFile || output.stdout);
    if (!transcript) {
      throw new WhisperTranscriptionError(
        "Whisper returned an empty transcript. Check audio format or model configuration.",
        422
      );
    }

    return transcript;
  } finally {
    await rm(tempDirectory, { recursive: true, force: true }).catch(() => undefined);
  }
};

const transcribeWithOpenAIBackend = async (audioFilePath: string): Promise<string> => {
  const timeoutMs = resolveTimeoutMs();
  const language = normalizeLanguage(process.env.WHISPER_LANGUAGE);
  const threads = resolveThreads();
  const model = resolveOpenAIModel();
  const device = resolveOpenAIDevice();
  const fp16 = resolveOpenAIFp16();
  const modelDir = resolveOpenAIModelDir();
  const commandEnv = await buildWhisperCommandEnv();
  const tempDirectory = await mkdtemp(path.join(os.tmpdir(), "daily-whisper-openai-"));
  const transcriptFileName = `${path.parse(audioFilePath).name}.txt`;
  const transcriptPath = path.join(tempDirectory, transcriptFileName);

  await mkdir(modelDir, { recursive: true });

  const baseArgs = [
    "-m",
    "whisper",
    audioFilePath,
    "--task",
    "transcribe",
    "--model",
    model,
    "--model_dir",
    modelDir,
    "--language",
    language,
    "--threads",
    String(threads),
    "--output_dir",
    tempDirectory,
    "--output_format",
    "txt",
    "--verbose",
    "False",
    "--fp16",
    fp16
  ];

  if (device) {
    baseArgs.push("--device", device);
  }

  let lastError: WhisperTranscriptionError | null = null;

  try {
    for (const pythonCommand of getPythonCandidates()) {
      try {
        const output = await runCommand(pythonCommand, baseArgs, timeoutMs, { env: commandEnv });
        const transcriptFromFile = await readFile(transcriptPath, "utf8").catch(() => "");
        const transcript = normalizeTranscript(transcriptFromFile || output.stdout);
        if (!transcript) {
          throw new WhisperTranscriptionError(
            "OpenAI Whisper returned an empty transcript. Check microphone audio quality and model settings.",
            422
          );
        }
        return transcript;
      } catch (error) {
        const normalizedError =
          error instanceof WhisperTranscriptionError
            ? error
            : new WhisperTranscriptionError("OpenAI Whisper command failed.", 502);
        lastError = normalizedError;
        const message = normalizedError.message;

        if (isFfmpegMissing(message)) {
          const expected = resolveFfmpegBinaryPath();
          throw new WhisperTranscriptionError(
            `OpenAI Whisper requires ffmpeg. Put binary at ${expected} or install ffmpeg globally, then restart dev server.`,
            500
          );
        }

        if (isCommandNotFound(message) || isWhisperModuleMissing(message)) {
          continue;
        }
      }
    }

    if (lastError) {
      if (isWhisperModuleMissing(lastError.message)) {
        throw new WhisperTranscriptionError(
          "Python module whisper is missing. Install it with: pip install -U openai-whisper",
          500
        );
      }

      if (isCommandNotFound(lastError.message)) {
        throw new WhisperTranscriptionError(
          "Python is not available for OpenAI Whisper. Set WHISPER_PYTHON_BIN or install python3.",
          500
        );
      }

      throw lastError;
    }

    throw new WhisperTranscriptionError(
      "OpenAI Whisper backend is not configured. Set WHISPER_PYTHON_BIN and install openai-whisper.",
      500
    );
  } finally {
    await rm(tempDirectory, { recursive: true, force: true }).catch(() => undefined);
  }
};

export const transcribeAudioWithLocalWhisper = async (audioFilePath: string): Promise<string> => {
  const normalizedAudioPath = audioFilePath.trim();
  if (!normalizedAudioPath) {
    throw new WhisperTranscriptionError("Audio file path is required for transcription.", 500);
  }

  const backend = resolveBackend();
  if (backend === "cpp") {
    return transcribeWithCppBackend(normalizedAudioPath);
  }

  if (backend === "openai") {
    return transcribeWithOpenAIBackend(normalizedAudioPath);
  }

  try {
    return await transcribeWithCppBackend(normalizedAudioPath);
  } catch (cppError) {
    try {
      return await transcribeWithOpenAIBackend(normalizedAudioPath);
    } catch (openAiError) {
      const cppMessage =
        cppError instanceof Error ? cppError.message : "cpp backend failed with unknown error.";
      if (openAiError instanceof WhisperTranscriptionError) {
        throw new WhisperTranscriptionError(
          `Whisper backends failed. cpp: ${cppMessage} | openai: ${openAiError.message}`,
          openAiError.status
        );
      }

      throw new WhisperTranscriptionError(`Whisper backends failed. cpp: ${cppMessage}`, 502);
    }
  }
};
