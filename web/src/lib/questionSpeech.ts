import type { InterviewQuestionSpeechConfig } from "./interviewSession";

const MAX_QUESTION_AUDIO_BYTES = 5 * 1024 * 1024;
const QUESTION_AUDIO_CACHE_NAME = "daily-speaking-question-audio-v1";
const MAX_PERSISTED_QUESTIONS = 48;

export type QuestionSpeechState = "idle" | "loading" | "playing" | "error";

const normalizedQuestion = (question: string): string => question.trim().replace(/\s+/g, " ");

const questionAudioCacheRequest = async (question: string): Promise<Request | null> => {
  if (typeof window === "undefined" || !window.isSecureContext || !("caches" in window) || !window.crypto?.subtle) {
    return null;
  }
  const digest = await window.crypto.subtle.digest("SHA-256", new TextEncoder().encode(question));
  const hash = Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
  return new Request(`${window.location.origin}/__question-audio-cache/${hash}`);
};

const readPersistedQuestionAudio = async (question: string): Promise<ArrayBuffer | null> => {
  try {
    const request = await questionAudioCacheRequest(question);
    if (!request) return null;
    const response = await (await window.caches.open(QUESTION_AUDIO_CACHE_NAME)).match(request);
    if (!response || !response.headers.get("Content-Type")?.toLowerCase().startsWith("audio/")) return null;
    const audio = await response.arrayBuffer();
    return audio.byteLength > 0 && audio.byteLength <= MAX_QUESTION_AUDIO_BYTES ? audio : null;
  } catch {
    return null;
  }
};

const persistQuestionAudio = async (question: string, audio: ArrayBuffer): Promise<void> => {
  try {
    const request = await questionAudioCacheRequest(question);
    if (!request) return;
    const cache = await window.caches.open(QUESTION_AUDIO_CACHE_NAME);
    await cache.put(request, new Response(audio.slice(0), { headers: { "Content-Type": "audio/mpeg" } }));
    const keys = await cache.keys();
    await Promise.all(keys.slice(0, Math.max(0, keys.length - MAX_PERSISTED_QUESTIONS)).map((key) => cache.delete(key)));
  } catch {
    // Playback still succeeds when private browsing or storage policy blocks Cache Storage.
  }
};

export const buildQuestionSpeechRequest = (
  config: InterviewQuestionSpeechConfig,
  question: string,
): { url: string; init: RequestInit } => {
  const transcript = normalizedQuestion(question);
  const endpoint = new URL(config.endpoint);
  if (endpoint.protocol !== "https:" || !endpoint.host || !config.token || !config.apiVersion ||
    !config.model || !config.voiceId || transcript.length < 2 || transcript.length > 300) {
    throw new Error("Question audio is unavailable.");
  }
  return {
    url: endpoint.toString(),
    init: {
      method: "POST",
      headers: {
        Authorization: `Bearer ${config.token}`,
        "Cartesia-Version": config.apiVersion,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model_id: config.model,
        transcript,
        voice: config.voiceId,
        output_format: { container: "mp3", sample_rate: 44100, bit_rate: 128000 },
        language: "en",
        normalization: "auto",
        generation_config: { volume: 1, speed: 1 },
      }),
    },
  };
};

export const fetchQuestionSpeech = async (
  config: InterviewQuestionSpeechConfig,
  question: string,
  fetcher: typeof fetch = fetch,
): Promise<ArrayBuffer> => {
  const request = buildQuestionSpeechRequest(config, question);
  const response = await fetcher(request.url, request.init);
  if (!response.ok) throw new Error("Question audio could not be generated.");
  const contentType = response.headers.get("Content-Type")?.toLowerCase() ?? "";
  const contentLength = Number(response.headers.get("Content-Length") ?? 0);
  if (!contentType.startsWith("audio/") || contentLength > MAX_QUESTION_AUDIO_BYTES) {
    throw new Error("The speech service returned invalid audio.");
  }
  const audio = await response.arrayBuffer();
  if (audio.byteLength === 0 || audio.byteLength > MAX_QUESTION_AUDIO_BYTES) {
    throw new Error("The speech service returned invalid audio.");
  }
  return audio;
};

export class QuestionSpeechPlayer {
  private context: AudioContext | null = null;
  private source: AudioBufferSourceNode | null = null;
  private resolveEnded: (() => void) | null = null;
  private generation = 0;
  private active = false;
  private readonly cache = new Map<string, ArrayBuffer>();
  private readonly pending = new Map<string, Promise<ArrayBuffer>>();

  isActive(): boolean {
    return this.active;
  }

  unlock(): void {
    try {
      const context = this.context ?? new AudioContext();
      this.context = context;
      void context.resume().catch(() => undefined);
    } catch {
      // Playback will surface the unsupported/blocked state when it is requested.
    }
  }

  async play(question: string, load: () => Promise<ArrayBuffer>, onPlaying: () => void): Promise<void> {
    this.stop();
    const generation = this.generation;
    const context = this.context ?? new AudioContext();
    this.context = context;
    this.active = true;
    const resumed = context.resume();
    try {
      const key = normalizedQuestion(question);
      const encoded = this.cache.get(key) ?? await this.loadOnce(key, load);
      if (generation !== this.generation) return;
      await resumed;
      const decoded = await context.decodeAudioData(encoded.slice(0));
      if (generation !== this.generation) return;
      const source = context.createBufferSource();
      this.source = source;
      source.buffer = decoded;
      source.connect(context.destination);
      onPlaying();
      await new Promise<void>((resolve) => {
        this.resolveEnded = resolve;
        source.onended = () => {
          this.resolveEnded = null;
          resolve();
        };
        source.start();
      });
    } finally {
      if (generation === this.generation) this.release();
    }
  }

  stop(): void {
    this.generation += 1;
    this.release();
  }

  dispose(): void {
    this.stop();
    this.cache.clear();
    const context = this.context;
    this.context = null;
    if (context) void context.close().catch(() => undefined);
  }

  private loadOnce(question: string, load: () => Promise<ArrayBuffer>): Promise<ArrayBuffer> {
    const existing = this.pending.get(question);
    if (existing) return existing;
    const pending = (async () => {
      const persisted = await readPersistedQuestionAudio(question);
      const encoded = persisted ?? await load();
      this.cache.set(question, encoded);
      while (this.cache.size > 12) this.cache.delete(this.cache.keys().next().value as string);
      if (!persisted) void persistQuestionAudio(question, encoded);
      return encoded;
    })();
    this.pending.set(question, pending);
    void pending.then(
      () => this.pending.delete(question),
      () => this.pending.delete(question),
    );
    return pending;
  }

  private release(): void {
    this.active = false;
    this.resolveEnded?.();
    this.resolveEnded = null;
    if (this.source) {
      this.source.onended = null;
      try { this.source.stop(); } catch { /* Playback already ended. */ }
      try { this.source.disconnect(); } catch { /* Playback already disconnected. */ }
      this.source = null;
    }
  }
}
