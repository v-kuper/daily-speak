import { apiFetch, readApiJSON, resolveApiURL } from "./apiClient";
import type { PracticeType, Suggestion } from "./data";
import { parseSuggestions } from "./suggestions";

const GUEST_SESSION_KEY = "daily-speaking.guest-preview.v1";
const GUEST_PROMOTION_KEY = "daily-speaking.guest-promotion.v1";
const GUEST_OPERATION_KEY = "daily-speaking.guest-operation.v1";
const MAX_GUEST_AUDIO_BYTES = 10 * 1024 * 1024;
export const MAX_GUEST_PREVIEW_SECONDS = 60;

type IdentityTokens = {
  tokenType: "Bearer";
  accessToken: string;
  accessTokenExpiresAt: string;
  refreshToken: string;
  refreshTokenExpiresAt: string;
};

export type GuestPreviewSession = {
  principalId: string;
  tokens: IdentityTokens;
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
};

export type GuestPreviewPromotion = {
  status: "promoted" | "not_promoted" | "no_preview";
  previewId?: string;
  recordingId?: string;
  reason?: "promotion_already_used" | "quota_exceeded";
};

type PendingPromotion = {
  email: string;
  promotion: GuestPreviewPromotion;
  userAccessToken: string;
};

type GuestPreviewOperation = {
  audioChecksum: string;
  uploadKey: string;
  previewKey: string;
  previewRequest: {
    topic: string;
    duration: number;
    timestamp: string;
    practiceType: "free_talk" | "topic";
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

const isFuture = (value: string, leewayMilliseconds = 0): boolean => {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && timestamp > Date.now() + leewayMilliseconds;
};

const parseTokens = (value: unknown): IdentityTokens | null => {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Record<string, unknown>;
  if (
    candidate.tokenType !== "Bearer" ||
    typeof candidate.accessToken !== "string" || !candidate.accessToken ||
    typeof candidate.accessTokenExpiresAt !== "string" ||
    typeof candidate.refreshToken !== "string" || !candidate.refreshToken ||
    typeof candidate.refreshTokenExpiresAt !== "string"
  ) return null;
  return candidate as IdentityTokens;
};

export const readGuestPreviewSession = (): GuestPreviewSession | null => {
  const value = storage()?.getItem(GUEST_SESSION_KEY);
  if (!value) return null;
  try {
    const candidate = JSON.parse(value) as Record<string, unknown>;
    const tokens = parseTokens(candidate.tokens);
    if (typeof candidate.principalId !== "string" || !candidate.principalId || !tokens) return null;
    return {
      principalId: candidate.principalId,
      tokens,
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

const readPendingPromotion = (): PendingPromotion | null => {
  const value = storage()?.getItem(GUEST_PROMOTION_KEY);
  if (!value) return null;
  try {
    const candidate = JSON.parse(value) as PendingPromotion;
    return candidate && typeof candidate.email === "string" && candidate.promotion ? candidate : null;
  } catch {
    return null;
  }
};

const writePendingPromotion = (value: PendingPromotion | null): void => {
  const target = storage();
  if (!target) return;
  if (value) target.setItem(GUEST_PROMOTION_KEY, JSON.stringify(value));
  else target.removeItem(GUEST_PROMOTION_KEY);
};

export const completeGuestPromotion = async (): Promise<void> => {
  const pending = readPendingPromotion();
  if (pending?.userAccessToken) {
    try {
      await apiFetch("/api/v1/auth/logout", {
        method: "POST",
        headers: { Authorization: `Bearer ${pending.userAccessToken}` },
      });
    } catch {
      // The cookie session is already active; the short-lived device grant will expire if cleanup is unavailable.
    }
  }
  writePendingPromotion(null);
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

const parseIdentity = (value: unknown): GuestPreviewSession | null => {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Record<string, unknown>;
  const principal = candidate.principal as Record<string, unknown> | undefined;
  const tokens = parseTokens(candidate.tokens);
  if (!principal || typeof principal.id !== "string" || !principal.id || !tokens) return null;
  return { principalId: principal.id, tokens, previewId: null };
};

const createGuestIdentity = async (): Promise<GuestPreviewSession> => {
  const response = await apiFetch("/api/v1/auth/anonymous", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceName: "Daily Speaking Web", platform: "web" }),
  });
  if (!response.ok) throw await responseError(response, "Cannot start a guest session.");
  const session = parseIdentity(await readApiJSON<unknown>(response));
  if (!session) throw new GuestPreviewError("The guest session response is invalid.");
  writeGuestPreviewSession(session);
  return session;
};

const refreshGuestIdentity = async (session: GuestPreviewSession): Promise<GuestPreviewSession> => {
  const response = await apiFetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken: session.tokens.refreshToken }),
  });
  if (!response.ok) throw await responseError(response, "The guest session has expired.");
  const refreshed = parseIdentity(await readApiJSON<unknown>(response));
  if (!refreshed) throw new GuestPreviewError("The refreshed guest session is invalid.");
  refreshed.previewId = session.previewId;
  writeGuestPreviewSession(refreshed);
  return refreshed;
};

let guestRefreshPromise: Promise<GuestPreviewSession> | null = null;

const isTerminalRefreshError = (error: unknown): boolean => error instanceof GuestPreviewError && new Set([
  "invalid_refresh_token",
  "refresh_token_expired",
  "refresh_token_reused",
]).has(error.code);

const refreshGuestIdentityOnce = (session: GuestPreviewSession): Promise<GuestPreviewSession> => {
  if (!guestRefreshPromise) {
    guestRefreshPromise = refreshGuestIdentity(session).finally(() => {
      guestRefreshPromise = null;
    });
  }
  return guestRefreshPromise;
};

const activeGuestIdentity = async (createIfMissing: boolean): Promise<GuestPreviewSession> => {
  const current = readGuestPreviewSession();
  if (current && isFuture(current.tokens.accessTokenExpiresAt, 30_000)) return current;
  if (current && isFuture(current.tokens.refreshTokenExpiresAt)) {
    try {
      return await refreshGuestIdentityOnce(current);
    } catch (error) {
      if (isTerminalRefreshError(error)) {
        writeGuestPreviewSession(null);
        if (createIfMissing) return createGuestIdentity();
      }
      throw error;
    }
  }
  if (current) writeGuestPreviewSession(null);
  if (createIfMissing) return createGuestIdentity();
  throw new GuestPreviewError("This guest preview session has expired. Record a new sample to continue.", "guest_session_expired");
};

const guestFetch = async (path: string, init: RequestInit, createIfMissing = false): Promise<Response> => {
  let session = await activeGuestIdentity(createIfMissing);
  const request = () => apiFetch(path, {
    ...init,
    headers: { ...init.headers, Authorization: `Bearer ${session.tokens.accessToken}` },
  });
  let response = await request();
  if (response.status === 401 && isFuture(session.tokens.refreshTokenExpiresAt)) {
    try {
      session = await refreshGuestIdentityOnce(session);
    } catch (error) {
      if (isTerminalRefreshError(error)) writeGuestPreviewSession(null);
      throw error;
    }
    response = await request();
  }
  return response;
};

const dataURLToBlob = (value: string): Blob => {
  const match = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i.exec(value);
  if (!match) throw new GuestPreviewError("Recorded audio has an unsupported format.", "invalid_audio");
  const contentType = match[1].split(";", 1)[0].toLowerCase();
  const binary = globalThis.atob(match[2]);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return new Blob([bytes], { type: contentType });
};

const sha256 = async (value: Blob): Promise<string> => {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", await value.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
};

const idempotencyKey = (scope: string): string => `${scope}:${globalThis.crypto.randomUUID()}`;

const uploadGuestAudio = async (blob: Blob, checksum: string, uploadKey: string): Promise<string> => {
  if (blob.size <= 0 || blob.size > MAX_GUEST_AUDIO_BYTES) {
    throw new GuestPreviewError("Guest recordings must be smaller than 10 MB.", "audio_too_large");
  }
  const createResponse = await guestFetch("/api/v1/media/uploads", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": uploadKey,
    },
    body: JSON.stringify({
      purpose: "recording_audio",
      contentType: blob.type || "audio/webm",
      sizeBytes: blob.size,
      checksum: { algorithm: "sha256", value: checksum },
    }),
  }, true);
  if (!createResponse.ok) throw await responseError(createResponse, "Cannot prepare the guest audio upload.");
  const resource = await readApiJSON<{
    asset?: { id?: unknown; state?: unknown };
    upload?: { id?: unknown; state?: unknown; partSizeBytes?: unknown; partCount?: unknown };
  }>(createResponse);
  const assetId = typeof resource?.asset?.id === "string" ? resource.asset.id : "";
  const uploadId = typeof resource?.upload?.id === "string" ? resource.upload.id : "";
  const partSize = Number(resource?.upload?.partSizeBytes);
  const partCount = Number(resource?.upload?.partCount);
  if (!assetId || !uploadId || !Number.isSafeInteger(partSize) || partSize <= 0 || !Number.isSafeInteger(partCount) || partCount <= 0) {
    throw new GuestPreviewError("The media upload response is invalid.");
  }
  if (resource?.asset?.state === "ready" && resource?.upload?.state === "completed") return assetId;

  const blobs = Array.from({ length: partCount }, (_, index) => blob.slice(index * partSize, Math.min(blob.size, (index + 1) * partSize), blob.type));
  const descriptors = await Promise.all(blobs.map(async (part, index) => ({
    partNumber: index + 1,
    sizeBytes: part.size,
    checksumSha256: await sha256(part),
  })));
  const signedResponse = await guestFetch(`/api/v1/media/uploads/${encodeURIComponent(uploadId)}/parts`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parts: descriptors }),
  });
  if (!signedResponse.ok) throw await responseError(signedResponse, "Cannot prepare the guest audio parts.");
  const signed = await readApiJSON<{ parts?: Array<{
    partNumber?: unknown;
    checksumSha256?: unknown;
    request?: { method?: unknown; url?: unknown; headers?: unknown };
  }> }>(signedResponse);
  if (!Array.isArray(signed?.parts) || signed.parts.length !== blobs.length) {
    throw new GuestPreviewError("The signed upload response is invalid.");
  }

  const completed = [] as Array<{ partNumber: number; etag: string; checksumSha256: string }>;
  for (const part of signed.parts) {
    const partNumber = Number(part.partNumber);
    const request = part.request;
    const descriptor = descriptors[partNumber - 1];
    const body = blobs[partNumber - 1];
    if (!descriptor || !body || request?.method !== "PUT" || typeof request.url !== "string" || !request.url) {
      throw new GuestPreviewError("A signed upload part is invalid.");
    }
    const headers = request.headers && typeof request.headers === "object"
      ? request.headers as Record<string, string>
      : {};
    const uploadResponse = await globalThis.fetch(resolveApiURL(request.url), {
      method: "PUT",
      headers,
      body,
      credentials: "omit",
    });
    if (!uploadResponse.ok) throw new GuestPreviewError("Guest audio upload failed. Please try again.", "media_upload_failed");
    const etag = uploadResponse.headers.get("ETag")?.trim();
    if (!etag) throw new GuestPreviewError("The media service did not confirm the uploaded part.", "missing_etag");
    completed.push({ partNumber, etag, checksumSha256: descriptor.checksumSha256 });
  }

  const completeResponse = await guestFetch(`/api/v1/media/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parts: completed }),
  });
  if (!completeResponse.ok) throw await responseError(completeResponse, "Cannot finalize the guest audio upload.");
  const completedResource = await readApiJSON<{ asset?: { id?: unknown; state?: unknown } }>(completeResponse);
  if (completedResource?.asset?.id !== assetId || completedResource?.asset?.state !== "ready") {
    throw new GuestPreviewError("The guest audio is not ready for analysis.");
  }
  return assetId;
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
  const audioChecksum = await sha256(blob);
  const previousOperation = readGuestOperation();
  const operation: GuestPreviewOperation = previousOperation?.audioChecksum === audioChecksum
    ? previousOperation
    : {
        audioChecksum,
        uploadKey: idempotencyKey("web-upload"),
        previewKey: idempotencyKey("web-preview"),
        previewRequest: {
          topic: draft.topic,
          duration,
          timestamp: draft.timestamp,
          practiceType: draft.practiceType,
        },
      };
  writeGuestOperation(operation);
  const assetId = await uploadGuestAudio(blob, audioChecksum, operation.uploadKey);
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

const parsePromotion = (value: unknown): GuestPreviewPromotion => {
  if (!value || typeof value !== "object") return { status: "no_preview" };
  const candidate = value as Record<string, unknown>;
  if (candidate.status === "promoted" && typeof candidate.recordingId === "string" && candidate.recordingId) {
    return {
      status: "promoted",
      previewId: typeof candidate.previewId === "string" ? candidate.previewId : undefined,
      recordingId: candidate.recordingId,
    };
  }
  if (candidate.status === "not_promoted") {
    return {
      status: "not_promoted",
      previewId: typeof candidate.previewId === "string" ? candidate.previewId : undefined,
      reason: candidate.reason === "quota_exceeded" ? "quota_exceeded" : "promotion_already_used",
    };
  }
  return { status: "no_preview" };
};

export const promoteGuestIdentity = async (
  mode: "signIn" | "signUp",
  email: string,
  password: string,
): Promise<GuestPreviewPromotion> => {
  const normalizedEmail = email.trim().toLowerCase();
  const pending = readPendingPromotion();
  if (pending?.email === normalizedEmail) return pending.promotion;
  if (pending) writePendingPromotion(null);

  const session = readGuestPreviewSession();
  if (!session?.previewId) {
    throw new GuestPreviewError("The guest preview session is missing or expired.", "guest_session_expired");
  }
  const response = await guestFetch(`/api/v1/auth/${mode === "signUp" ? "register" : "login"}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: normalizedEmail, password, deviceName: "Daily Speaking Web", platform: "web" }),
  });
  if (!response.ok) throw await responseError(response, mode === "signUp" ? "Failed to create account." : "Failed to sign in.");
  const payload = await readApiJSON<{ guestPreviewPromotion?: unknown; tokens?: unknown }>(response);
  const promotion = parsePromotion(payload?.guestPreviewPromotion);
  const userTokens = parseTokens(payload?.tokens);
  if (!userTokens) throw new GuestPreviewError("The authenticated session response is invalid.");
  writePendingPromotion({ email: normalizedEmail, promotion, userAccessToken: userTokens.accessToken });
  writeGuestPreviewSession(null);
  writeGuestOperation(null);
  return promotion;
};

export const startNewGuestPreviewSession = (): void => {
  writeGuestPreviewSession(null);
  writeGuestOperation(null);
  writePendingPromotion(null);
};

export const guestPreviewPath = (previewId: string): string => `/preview/${encodeURIComponent(previewId)}`;

export const guestPreviewIdFromPath = (value: string): string | null => {
  const match = /^\/preview\/([A-Za-z0-9-]+)$/.exec(value);
  return match ? match[1] : null;
};
