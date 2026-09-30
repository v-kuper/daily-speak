import { apiFetch, readApiJSON, resolveApiURL } from "./apiClient";

type MediaRequest = (path: string, init: RequestInit) => Promise<Response>;

type MediaErrorPayload = {
  error?: { code?: unknown; message?: unknown };
};

export type MediaDownloadPayload = {
  asset?: { id?: unknown; state?: unknown };
  request?: {
    method?: unknown;
    url?: unknown;
    headers?: unknown;
    expiresAt?: unknown;
  };
};

export type MediaPlaybackTicket = {
  url: string;
  expiresAt: string;
};

export class MediaDownloadError extends Error {
  constructor(message: string, readonly code = "media_download_failed") {
    super(message);
    this.name = "MediaDownloadError";
  }
}

const parseDownloadAssetID = (downloadPath: string): string | null => {
  const match = /^\/api\/v1\/media\/([^/?#]+)\/download$/.exec(downloadPath);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
};

const readDownloadError = async (response: Response): Promise<MediaDownloadError> => {
  let payload: MediaErrorPayload | null = null;
  try {
    payload = await readApiJSON<MediaErrorPayload>(response);
  } catch {
    // A proxy is not required to preserve the API error envelope.
  }
  const code = typeof payload?.error?.code === "string"
    ? payload.error.code
    : response.status === 401
      ? "unauthorized"
      : "media_download_failed";
  const message = typeof payload?.error?.message === "string"
    ? payload.error.message
    : response.status === 401
      ? "Your session has expired. Sign in again."
      : "Pronunciation audio is temporarily unavailable.";
  return new MediaDownloadError(message, code);
};

const resolvePlaybackURL = (value: string): string => {
  if (value.startsWith("/")) return resolveApiURL(value);
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new MediaDownloadError("The media service returned an invalid download URL.");
  }
  if (!["http:", "https:"].includes(parsed.protocol) || parsed.username || parsed.password) {
    throw new MediaDownloadError("The media service returned an invalid download URL.");
  }
  return parsed.href;
};

const validateBrowserPlaybackHeaders = (value: unknown, playbackURL: string): void => {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new MediaDownloadError("The media service returned an invalid download request.");
  }
  const expectedHost = new URL(playbackURL).host.toLowerCase();
  for (const [name, headerValue] of Object.entries(value)) {
    if (typeof headerValue !== "string") {
      throw new MediaDownloadError("The media service returned an invalid download request.");
    }
    if (name.toLowerCase() === "host" && headerValue.toLowerCase() === expectedHost) continue;
    throw new MediaDownloadError("This protected audio request cannot be played by the browser.");
  }
};

export const requestMediaPlaybackTicket = async ({
  downloadPath,
  signal,
  request = apiFetch,
  now = Date.now,
}: {
  downloadPath: string;
  signal?: AbortSignal;
  request?: MediaRequest;
  now?: () => number;
}): Promise<MediaPlaybackTicket> => {
  const assetId = parseDownloadAssetID(downloadPath);
  if (!assetId) throw new MediaDownloadError("The protected media reference is invalid.");

  const response = await request(downloadPath, { method: "GET", cache: "no-store", signal });
  if (!response.ok) throw await readDownloadError(response);
  const payload = await readApiJSON<MediaDownloadPayload>(response);
  return parseMediaPlaybackTicket(payload, assetId, now);
};

export const parseMediaPlaybackTicket = (payload: MediaDownloadPayload | null, assetId: string, now = Date.now): MediaPlaybackTicket => {
  const signedRequest = payload?.request;
  if (
    payload?.asset?.id !== assetId ||
    payload.asset.state !== "ready" ||
    signedRequest?.method !== "GET" ||
    typeof signedRequest.url !== "string" ||
    !signedRequest.url ||
    typeof signedRequest.expiresAt !== "string"
  ) {
    throw new MediaDownloadError("The media service returned an invalid download request.");
  }

  const expiresAt = Date.parse(signedRequest.expiresAt);
  if (!Number.isFinite(expiresAt) || expiresAt <= now() + 1_000) {
    throw new MediaDownloadError("The media download request has already expired.", "media_download_expired");
  }
  const url = resolvePlaybackURL(signedRequest.url);
  validateBrowserPlaybackHeaders(signedRequest.headers, url);
  return { url, expiresAt: new Date(expiresAt).toISOString() };
};
