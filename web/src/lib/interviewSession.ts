import { apiFetch, readApiJSON } from "./apiClient";
import { ensureGuestPreviewIdentity } from "./guestPreview";
import { resolveInterestLabels } from "./interestCatalog";
import { newIdempotencyKey, uploadMedia } from "./mediaUpload";

export type InterviewCandidate = { id: string; question: string };
export type InterviewVocabularyItem = { word: string; translation: string };
export type InterviewTurn = {
  seq: number;
  question: string;
  askedAtMs: number;
  endedAtMs: number | null;
  provisionalTranscript: string;
  transcriptStatus?: string;
  /** Client-only stable deltas received from the live STT socket. */
  liveTranscriptFinal?: string;
  /** Client-only replaceable STT hypothesis. It is never submitted. */
  liveTranscriptInterim?: string;
};
export type InterviewSession = {
  id: string;
  status: string;
  topic: string;
  openingQuestion: string;
  error: string | null;
  usefulWords: string[];
  usefulVocabulary: InterviewVocabularyItem[];
  turns: InterviewTurn[];
  candidates: InterviewCandidate[];
  currentTurnSeq: number | null;
  maxDurationSeconds: number | null;
};

export type InterviewTranscriptionSocketConfig = {
  token: string;
  expiresAt: string;
  websocketUrl: string;
  model: string;
  encoding: string;
  sampleRate: number;
};

type ErrorBody = { error?: { message?: unknown } };

export const mergeInterviewTranscriptStatus = (
  serverStatus: string | undefined,
  localStatus: string | undefined,
): string | undefined => {
  if (serverStatus === "ready") return "ready";
  if (localStatus === "failed") return "failed";
  return serverStatus || localStatus;
};

/** Keep local recognition feedback while an older/empty server snapshot is merged. */
export const preserveLiveInterviewTurn = (
  server: InterviewTurn,
  local: InterviewTurn,
): InterviewTurn => {
  const merged = {
    ...server,
    provisionalTranscript: server.transcriptStatus === "ready"
      ? server.provisionalTranscript
      : server.provisionalTranscript || local.provisionalTranscript,
  };
  // A ready server transcript is canonical for both realtime submission and
  // batch fallback. Do not let a stale partial socket hypothesis cover it.
  if (server.transcriptStatus === "ready") return merged;
  return {
    ...merged,
    ...(local.liveTranscriptFinal !== undefined ? { liveTranscriptFinal: local.liveTranscriptFinal } : {}),
    ...(local.liveTranscriptInterim !== undefined ? { liveTranscriptInterim: local.liveTranscriptInterim } : {}),
  };
};

export const parseInterviewVocabulary = (
  payload: unknown,
  fallbackWords: string[] = [],
): InterviewVocabularyItem[] => {
  const seen = new Set<string>();
  const items: InterviewVocabularyItem[] = [];
  const add = (wordValue: unknown, translationValue: unknown) => {
    if (typeof wordValue !== "string" || typeof translationValue !== "string") return;
    const word = wordValue.trim().replace(/\s+/g, " ");
    const translation = translationValue.trim().replace(/\s+/g, " ");
    const key = word.toLocaleLowerCase();
    if (!word || seen.has(key) || items.length >= 12) return;
    seen.add(key);
    items.push({ word, translation });
  };
  if (Array.isArray(payload)) {
    for (const value of payload) {
      if (!value || typeof value !== "object") continue;
      const item = value as Record<string, unknown>;
      add(item.word, item.translation);
    }
  }
  for (const word of fallbackWords) add(word, "");
  return items;
};

const parseSession = (payload: unknown): InterviewSession => {
  const outer = payload && typeof payload === "object" ? payload as Record<string, unknown> : {};
  const value = outer.interview ?? outer.session ?? outer;
  const source = value && typeof value === "object" ? value as Record<string, unknown> : {};
  if (typeof source.id !== "string" || !source.id) {
    throw new Error("The interview service returned an invalid session.");
  }
  const turns = (Array.isArray(source.turns) ? source.turns : []).flatMap((value): InterviewTurn[] => {
    if (!value || typeof value !== "object") return [];
    const turn = value as Record<string, unknown>;
    if (!Number.isSafeInteger(turn.seq) || Number(turn.seq) < 1 || typeof turn.question !== "string") return [];
    return [{
      seq: Number(turn.seq),
      question: turn.question,
      askedAtMs: Number.isFinite(turn.askedAtMs) ? Math.max(0, Number(turn.askedAtMs)) : 0,
      endedAtMs: Number.isFinite(turn.endedAtMs) ? Math.max(0, Number(turn.endedAtMs)) : null,
      provisionalTranscript: typeof turn.provisionalTranscript === "string" ? turn.provisionalTranscript : "",
      transcriptStatus: typeof turn.transcriptStatus === "string" ? turn.transcriptStatus : "pending",
    }];
  });
  const candidates = (Array.isArray(source.candidates) ? source.candidates : []).flatMap((value): InterviewCandidate[] => {
    if (!value || typeof value !== "object") return [];
    const candidate = value as Record<string, unknown>;
    if (typeof candidate.id !== "string" || !candidate.id || typeof candidate.question !== "string" || !candidate.question.trim()) return [];
    return [{ id: candidate.id, question: candidate.question.trim() }];
  });
  const usefulWords = (Array.isArray(source.usefulWords) ? source.usefulWords : [])
    .filter((word): word is string => typeof word === "string")
    .map((word) => word.trim().replace(/\s+/g, " "))
    .filter(Boolean)
    .slice(0, 12);
  return {
    id: source.id,
    status: typeof source.status === "string" ? source.status : "preparing",
    topic: typeof source.topic === "string" ? source.topic : "",
    openingQuestion: typeof source.openingQuestion === "string" && source.openingQuestion.trim()
      ? source.openingQuestion.trim()
      : typeof source.topic === "string" ? source.topic : "",
    error: typeof source.error === "string" && source.error.trim() ? source.error.trim() : null,
    usefulWords,
    usefulVocabulary: parseInterviewVocabulary(source.usefulVocabulary, usefulWords),
    turns,
    candidates,
    currentTurnSeq: Number.isSafeInteger(source.currentTurnSeq) && Number(source.currentTurnSeq) > 0
      ? Number(source.currentTurnSeq)
      : null,
    maxDurationSeconds: Number.isFinite(source.maxDurationSeconds) && Number(source.maxDurationSeconds) > 0
      ? Math.floor(Number(source.maxDurationSeconds))
      : null,
  };
};

const interviewRequest = async (path: string, init?: RequestInit): Promise<InterviewSession> => {
  const response = await apiFetch(path, init);
  if (!response.ok) {
    const payload = await readApiJSON<ErrorBody>(response).catch(() => null);
    const message = payload?.error?.message;
    throw new Error(typeof message === "string" && message ? message : "The interview service is unavailable.");
  }
  return parseSession(await readApiJSON<unknown>(response));
};

const postJSON = (body: unknown, idempotencyKey?: string): RequestInit => ({
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    ...(idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {}),
  },
  body: JSON.stringify(body),
});

export const prepareInterview = async ({
  topic,
  level,
  interestIds,
  guest,
  idempotencyKey,
}: {
  topic: string;
  level: string;
  interestIds: string[];
  guest: boolean;
  idempotencyKey: string;
}): Promise<InterviewSession> => {
  if (guest) await ensureGuestPreviewIdentity();
  return interviewRequest("/api/v1/interviews", postJSON({
    topic,
    openingQuestion: topic,
    idempotencyKey,
    englishLevel: level,
    interests: resolveInterestLabels(interestIds),
  }, idempotencyKey));
};

const interviewPath = (id: string): string => `/api/v1/interviews/${encodeURIComponent(id)}`;

export const getInterview = (id: string): Promise<InterviewSession> =>
  interviewRequest(interviewPath(id), { cache: "no-store" });

export const startInterview = (id: string, idempotencyKey: string): Promise<InterviewSession> =>
  interviewRequest(`${interviewPath(id)}/start`, postJSON({}, idempotencyKey));

export const cancelInterview = (id: string, keepalive = false): Promise<InterviewSession> =>
  interviewRequest(`${interviewPath(id)}/cancel`, { ...postJSON({}), keepalive });

export const advanceInterview = (
  id: string,
  currentTurnSeq: number,
  nextCandidateId: string,
  atMs: number,
  idempotencyKey: string,
  skipCurrent = false,
): Promise<InterviewSession> => interviewRequest(`${interviewPath(id)}/advance`, postJSON({
  idempotencyKey,
  currentTurnSeq,
  nextCandidateId,
  atMs,
  skipCurrent,
}, idempotencyKey));

export const skipInterviewTurn = (
  id: string,
  seq: number,
  atMs: number,
  idempotencyKey: string,
): Promise<InterviewSession> => interviewRequest(
  `${interviewPath(id)}/turns/${seq}/skip`,
  postJSON({ idempotencyKey, atMs }, idempotencyKey),
);

export const uploadInterviewTurnAudio = async (
  id: string,
  seq: number,
  blob: Blob,
  guest: boolean,
  idempotencyKey = newIdempotencyKey(`interview-turn-${seq}`),
): Promise<void> => {
  if (guest) await ensureGuestPreviewIdentity();
  const assetId = await uploadMedia({
    blob,
    purpose: "interview_turn_audio",
    interviewSessionId: id,
    idempotencyKey: `${idempotencyKey}:upload`,
  });
  const response = await apiFetch(`${interviewPath(id)}/turns/${seq}/audio`, postJSON({
    idempotencyKey,
    audioAssetId: assetId,
  }, idempotencyKey));
  if (!response.ok) {
    const payload = await readApiJSON<ErrorBody>(response).catch(() => null);
    throw new Error(typeof payload?.error?.message === "string" ? payload.error.message : "The answer could not be processed.");
  }
};

export const getInterviewTranscriptionToken = async (
  id: string,
): Promise<InterviewTranscriptionSocketConfig> => {
  const response = await apiFetch(`${interviewPath(id)}/transcription-token`, postJSON({}));
  const payload = await readApiJSON<unknown>(response).catch(() => null);
  if (!response.ok) {
    const error = payload && typeof payload === "object" ? (payload as ErrorBody).error : null;
    throw new Error(typeof error?.message === "string" ? error.message : "Realtime transcription is unavailable.");
  }
  const value = payload && typeof payload === "object" ? payload as Record<string, unknown> : {};
  const sampleRate = Number(value.sampleRate);
  if (
    typeof value.token !== "string" || !value.token ||
    typeof value.expiresAt !== "string" || !value.expiresAt ||
    typeof value.websocketUrl !== "string" || !value.websocketUrl ||
    typeof value.model !== "string" || !value.model ||
    typeof value.encoding !== "string" || !value.encoding ||
    !Number.isSafeInteger(sampleRate) || sampleRate <= 0
  ) {
    throw new Error("The transcription service returned an invalid connection token.");
  }
  return {
    token: value.token,
    expiresAt: value.expiresAt,
    websocketUrl: value.websocketUrl,
    model: value.model,
    encoding: value.encoding,
    sampleRate,
  };
};

export const submitInterviewTurnTranscript = (
  id: string,
  seq: number,
  text: string,
  idempotencyKey: string,
): Promise<InterviewSession> => interviewRequest(
  `${interviewPath(id)}/turns/${seq}/transcript`,
  postJSON({ idempotencyKey, text }, idempotencyKey),
);

export const finalizeInterview = async (
  id: string,
  endedAtMs: number,
  result: { recordingId: string } | { guestPreviewId: string },
  idempotencyKey: string,
): Promise<InterviewSession> => interviewRequest(`${interviewPath(id)}/finalize`, postJSON({
  idempotencyKey,
  endedAtMs,
  ...result,
}, idempotencyKey));
