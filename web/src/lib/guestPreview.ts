import { apiFetch, readApiJSON } from "./apiClient";
import type { PracticeType, Suggestion } from "./data";
import {
  browserIdentity,
  createAnonymousIdentity,
  restoreBrowserIdentity,
  type IdentityPromotion,
} from "./identity";
import { dataURLToBlob, newIdempotencyKey, sha256Blob, uploadMedia } from "./mediaUpload";
import { parseSuggestions } from "./suggestions";
import { parseInterviewTurns, type SavedInterviewTurn } from "./interviewTimeline";

const GUEST_SESSION_KEY = "daily-speaking.guest-preview.v1";
const GUEST_OPERATION_KEY = "daily-speaking.guest-operation.v1";
const MAX_GUEST_AUDIO_BYTES = 10 * 1024 * 1024;
export const MAX_GUEST_PREVIEW_SECONDS = 60;

export type GuestPreviewSession = {
  principalId: string;
  previewId: string | null;
};

export type GuestPreview = {
  id: string;
  state: "queued" | "processing" | "ready" | "failed";
  topic: string;
  duration: number;
  timestamp: string;
  practiceType: "free_talk" | "topic";
  transcript: string;
  interviewTurns: SavedInterviewTurn[];
  corrections: Suggestion[];
  processingError: string | null;
  expiresAt: string;
};

export type GuestPreviewDraft = {
  topic: string;
  duration: number;
  timestamp: string;
  practiceType: PracticeType;
  audioDataUrl: string;
  interviewSessionId?: string;
};

export type GuestPreviewPromotion = IdentityPromotion;

type GuestPreviewOperation = {
  audioChecksum: string;
  uploadKey: string;
  previewKey: string;
  previewRequest: {
    topic: string;
    duration: number;
    timestamp: string;
    practiceType: "free_talk" | "topic";
    interviewSessionId?: string;
  };
};

type V1ErrorPayload = {
  error?: { code?: unknown; message?: unknown };
};

export class GuestPreviewError extends Error {
  constructor(message: string, readonly code = "guest_preview_failed") {
    super(message);
    this.name = "GuestPreviewError";
  }
}

const storage = (): Storage | null => {
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch {
    return null;
  }
};

export const readGuestPreviewSession = (): GuestPreviewSession | null => {
  const value = storage()?.getItem(GUEST_SESSION_KEY);
  if (!value) return null;
  try {
    const candidate = JSON.parse(value) as Record<string, unknown>;
    if (typeof candidate.principalId !== "string" || !candidate.principalId) return null;
    return {
      principalId: candidate.principalId,
      previewId: typeof candidate.previewId === "string" && candidate.previewId ? candidate.previewId : null,
    };
  } catch {
    return null;
  }
};

const writeGuestPreviewSession = (session: GuestPreviewSession | null): void => {
  const target = storage();
  if (!target) return;
  if (session) target.setItem(GUEST_SESSION_KEY, JSON.stringify(session));
  else target.removeItem(GUEST_SESSION_KEY);
};

const readGuestOperation = (): GuestPreviewOperation | null => {
  const value = storage()?.getItem(GUEST_OPERATION_KEY);
  if (!value) return null;
  try {
    const candidate = JSON.parse(value) as GuestPreviewOperation;
    return candidate && typeof candidate.audioChecksum === "string" && typeof candidate.uploadKey === "string"
      && typeof candidate.previewKey === "string" && candidate.previewRequest ? candidate : null;
  } catch {
    return null;
  }
};

const writeGuestOperation = (operation: GuestPreviewOperation | null): void => {
  const target = storage();
  if (!target) return;
  if (operation) target.setItem(GUEST_OPERATION_KEY, JSON.stringify(operation));
  else target.removeItem(GUEST_OPERATION_KEY);
};

export const completeGuestPromotion = async (): Promise<void> => {
  writeGuestPreviewSession(null);
  writeGuestOperation(null);
};

const responseError = async (response: Response, fallback: string): Promise<GuestPreviewError> => {
  let payload: V1ErrorPayload | null = null;
  try {
    payload = await readApiJSON<V1ErrorPayload>(response);
  } catch {
    // Use the safe fallback for non-JSON storage or proxy failures.
  }
  const code = typeof payload?.error?.code === "string" ? payload.error.code : "guest_preview_failed";
  const message = typeof payload?.error?.message === "string" ? payload.error.message : fallback;
  return new GuestPreviewError(message, code);
};

const createGuestIdentity = async (): Promise<GuestPreviewSession> => {
  const identity = await createAnonymousIdentity();
  if (identity.kind !== "guest") throw new GuestPreviewError("The guest session response is invalid.");
  const session = { principalId: identity.principalId, previewId: null };
  writeGuestPreviewSession(session);
  return session;
};

const activeGuestIdentity = async (createIfMissing: boolean): Promise<GuestPreviewSession> => {
  let identity = browserIdentity();
  if (!identity) {
    try {
      identity = await restoreBrowserIdentity();
    } catch {
      if (createIfMissing) return createGuestIdentity();
      throw new GuestPreviewError("This guest preview session has expired. Record a new sample to continue.", "guest_session_expired");
    }
  }
  if (identity.kind !== "guest") {
    writeGuestPreviewSession(null);
    if (createIfMissing) return createGuestIdentity();
    throw new GuestPreviewError("This guest preview session has expired. Record a new sample to continue.", "guest_session_expired");
  }
  const stored = readGuestPreviewSession();
  const session = {
    principalId: identity.principalId,
    previewId: stored?.principalId === identity.principalId ? stored.previewId : null,
  };
  writeGuestPreviewSession(session);
  return session;
};

/** Prepare the same anonymous principal used by media uploads and the final preview. */
export const ensureGuestPreviewIdentity = (): Promise<GuestPreviewSession> => activeGuestIdentity(true);

const guestFetch = async (path: string, init: RequestInit, createIfMissing = false): Promise<Response> => {
  await activeGuestIdentity(createIfMissing);
  return apiFetch(path, init);
};

const uploadGuestAudio = async (blob: Blob, uploadKey: string): Promise<string> => {
  if (blob.size <= 0 || blob.size > MAX_GUEST_AUDIO_BYTES) {
    throw new GuestPreviewError("Guest recordings must be smaller than 10 MB.", "audio_too_large");
  }
  await activeGuestIdentity(true);
  return uploadMedia({
    blob,
    purpose: "recording_audio",
    idempotencyKey: uploadKey,
    request: (path, init) => guestFetch(path, init),
  });
};

const parsePreview = (value: unknown): GuestPreview | null => {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Record<string, unknown>;
  const state = candidate.state;
  const practiceType = candidate.practiceType;
  if (
    typeof candidate.id !== "string" || !candidate.id ||
    (state !== "queued" && state !== "processing" && state !== "ready" && state !== "failed") ||
    typeof candidate.topic !== "string" ||
    !Number.isFinite(candidate.duration) ||
    typeof candidate.timestamp !== "string" ||
    (practiceType !== "free_talk" && practiceType !== "topic") ||
    typeof candidate.transcript !== "string" ||
    typeof candidate.expiresAt !== "string"
  ) return null;
  return {
    id: candidate.id,
    state,
    topic: candidate.topic,
    duration: Number(candidate.duration),
    timestamp: candidate.timestamp,
    practiceType,
    transcript: candidate.transcript,
    interviewTurns: parseInterviewTurns(candidate.interviewTurns),
    corrections: parseSuggestions(candidate.corrections).slice(0, 2),
    processingError: typeof candidate.processingError === "string" ? candidate.processingError : null,
    expiresAt: candidate.expiresAt,
  };
};

export const createGuestPreview = async (draft: GuestPreviewDraft): Promise<GuestPreview> => {
  if (draft.practiceType === "photo_description") {
    throw new GuestPreviewError("Photo sessions require an account before analysis.", "unsupported_practice_type");
  }
  const duration = Math.floor(draft.duration);
  if (duration < 1 || duration > MAX_GUEST_PREVIEW_SECONDS) {
    throw new GuestPreviewError("Guest previews can be between 1 and 60 seconds.", "invalid_duration");
  }
  const blob = dataURLToBlob(draft.audioDataUrl);
  const audioChecksum = await sha256Blob(blob);
  const previousOperation = readGuestOperation();
  const operation: GuestPreviewOperation = previousOperation?.audioChecksum === audioChecksum
    && previousOperation.previewRequest.interviewSessionId === draft.interviewSessionId
    ? previousOperation
    : {
        audioChecksum,
        uploadKey: newIdempotencyKey("web-upload"),
        previewKey: newIdempotencyKey("web-preview"),
        previewRequest: {
          topic: draft.topic,
          duration,
          timestamp: draft.timestamp,
          practiceType: draft.practiceType,
          ...(draft.interviewSessionId ? { interviewSessionId: draft.interviewSessionId } : {}),
        },
      };
  writeGuestOperation(operation);
  const assetId = await uploadGuestAudio(blob, operation.uploadKey);
  const response = await guestFetch("/api/v1/guest/previews", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": operation.previewKey,
    },
    body: JSON.stringify({
      audioAssetId: assetId,
      ...operation.previewRequest,
    }),
  });
  if (!response.ok) throw await responseError(response, "Cannot start the guest analysis.");
  const payload = await readApiJSON<{ preview?: unknown }>(response);
  const preview = parsePreview(payload?.preview);
  if (!preview) throw new GuestPreviewError("The guest preview response is invalid.");
  const session = await activeGuestIdentity(false);
  session.previewId = preview.id;
  writeGuestPreviewSession(session);
  writeGuestOperation(null);
  return preview;
};

export const fetchGuestPreview = async (previewId: string): Promise<GuestPreview> => {
  const session = readGuestPreviewSession();
  if (!session || session.previewId !== previewId) {
    throw new GuestPreviewError("This preview belongs to another or expired guest session.", "preview_not_available");
  }
  const response = await guestFetch(`/api/v1/guest/previews/${encodeURIComponent(previewId)}`, { cache: "no-store" });
  if (!response.ok) throw await responseError(response, "Cannot load the guest preview.");
  const payload = await readApiJSON<{ preview?: unknown }>(response);
  const preview = parsePreview(payload?.preview);
  if (!preview) throw new GuestPreviewError("The guest preview response is invalid.");
  return preview;
};

/** Returns false when this anonymous principal has already used its one preview. */
export const startNewGuestPreviewSession = (): boolean => {
  if (readGuestPreviewSession()?.previewId) return false;
  // Reuse the existing anonymous principal. Clearing only the in-memory access
  // token does not clear the secure refresh cookie, so it cannot create a new
  // guest entitlement and can also break interview recovery.
  writeGuestOperation(null);
  return true;
};

export const guestPreviewPath = (previewId: string): string => `/preview/${encodeURIComponent(previewId)}`;

export const guestPreviewIdFromPath = (value: string): string | null => {
  const match = /^\/preview\/([A-Za-z0-9-]+)$/.exec(value);
  return match ? match[1] : null;
};
