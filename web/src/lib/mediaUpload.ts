import { apiFetch, readApiJSON, resolveApiURL } from "./apiClient";

export type MediaPurpose = "recording_audio" | "recording_photo" | "interview_turn_audio";

export type MediaRequest = (path: string, init: RequestInit) => Promise<Response>;

type V1ErrorPayload = {
  error?: { code?: unknown; message?: unknown };
};

export class MediaUploadError extends Error {
  constructor(message: string, readonly code = "media_upload_failed") {
    super(message);
    this.name = "MediaUploadError";
  }
}

export const dataURLToBlob = (value: string): Blob => {
  const match = /^data:((?:audio|video|image)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i.exec(value);
  if (!match) throw new MediaUploadError("Media has an unsupported format.", "invalid_media");
  const contentType = match[1].split(";", 1)[0].toLowerCase();
  const binary = globalThis.atob(match[2]);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return new Blob([bytes], { type: contentType });
};

export const sha256Blob = async (value: Blob): Promise<string> => {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", await value.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
};

export const newIdempotencyKey = (scope: string): string => `${scope}:${globalThis.crypto.randomUUID()}`;

const responseError = async (response: Response, fallback: string): Promise<MediaUploadError> => {
  let payload: V1ErrorPayload | null = null;
  try {
    payload = await readApiJSON<V1ErrorPayload>(response);
  } catch {
    // Proxies and object stores are not required to return the API error envelope.
  }
  const code = typeof payload?.error?.code === "string"
    ? payload.error.code
    : response.status === 401
      ? "unauthorized"
      : "media_upload_failed";
  const message = typeof payload?.error?.message === "string" ? payload.error.message : fallback;
  return new MediaUploadError(message, code);
};

export type UploadMediaInput = {
  blob: Blob;
  purpose: MediaPurpose;
  idempotencyKey: string;
  interviewSessionId?: string;
  request?: MediaRequest;
};

export const uploadMedia = async ({
  blob,
  purpose,
  idempotencyKey,
  interviewSessionId,
  request = apiFetch,
}: UploadMediaInput): Promise<string> => {
  if (blob.size <= 0) throw new MediaUploadError("Media is empty.", "invalid_media");
  const checksum = await sha256Blob(blob);
  const createResponse = await request("/api/v1/media/uploads", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey,
    },
    body: JSON.stringify({
      purpose,
      ...(interviewSessionId ? { interviewSessionId } : {}),
      contentType: blob.type,
      sizeBytes: blob.size,
      checksum: { algorithm: "sha256", value: checksum },
    }),
  });
  if (!createResponse.ok) throw await responseError(createResponse, "Cannot prepare the media upload.");
  const resource = await readApiJSON<{
    asset?: { id?: unknown; state?: unknown };
    upload?: { id?: unknown; state?: unknown; partSizeBytes?: unknown; partCount?: unknown };
  }>(createResponse);
  const assetId = typeof resource?.asset?.id === "string" ? resource.asset.id : "";
  const uploadId = typeof resource?.upload?.id === "string" ? resource.upload.id : "";
  const partSize = Number(resource?.upload?.partSizeBytes);
  const partCount = Number(resource?.upload?.partCount);
  if (!assetId || !uploadId || !Number.isSafeInteger(partSize) || partSize <= 0 || !Number.isSafeInteger(partCount) || partCount <= 0) {
    throw new MediaUploadError("The media upload response is invalid.");
  }
  if (resource?.asset?.state === "ready" && resource?.upload?.state === "completed") return assetId;

  const blobs = Array.from(
    { length: partCount },
    (_, index) => blob.slice(index * partSize, Math.min(blob.size, (index + 1) * partSize), blob.type),
  );
  const descriptors = await Promise.all(blobs.map(async (part, index) => ({
    partNumber: index + 1,
    sizeBytes: part.size,
    checksumSha256: await sha256Blob(part),
  })));
  const signedResponse = await request(`/api/v1/media/uploads/${encodeURIComponent(uploadId)}/parts`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parts: descriptors }),
  });
  if (!signedResponse.ok) throw await responseError(signedResponse, "Cannot prepare the media upload parts.");
  const signed = await readApiJSON<{ parts?: Array<{
    partNumber?: unknown;
    request?: { method?: unknown; url?: unknown; headers?: unknown };
  }> }>(signedResponse);
  if (!Array.isArray(signed?.parts) || signed.parts.length !== blobs.length) {
    throw new MediaUploadError("The signed upload response is invalid.");
  }

  const completed: Array<{ partNumber: number; etag: string; checksumSha256: string }> = [];
  for (const part of signed.parts) {
    const partNumber = Number(part.partNumber);
    const descriptor = descriptors[partNumber - 1];
    const body = blobs[partNumber - 1];
    const signedRequest = part.request;
    if (!descriptor || !body || signedRequest?.method !== "PUT" || typeof signedRequest.url !== "string" || !signedRequest.url) {
      throw new MediaUploadError("A signed upload part is invalid.");
    }
    const headers = signedRequest.headers && typeof signedRequest.headers === "object"
      ? signedRequest.headers as Record<string, string>
      : {};
    const uploadResponse = await globalThis.fetch(resolveApiURL(signedRequest.url), {
      method: "PUT",
      headers,
      body,
      credentials: "omit",
    });
    if (!uploadResponse.ok) throw new MediaUploadError("Media upload failed. Please try again.");
    const etag = uploadResponse.headers.get("ETag")?.trim();
    if (!etag) throw new MediaUploadError("The media service did not confirm the uploaded part.", "missing_etag");
    completed.push({ partNumber, etag, checksumSha256: descriptor.checksumSha256 });
  }

  const completeResponse = await request(`/api/v1/media/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parts: completed }),
  });
  if (!completeResponse.ok) throw await responseError(completeResponse, "Cannot finalize the media upload.");
  const completedResource = await readApiJSON<{ asset?: { id?: unknown; state?: unknown } }>(completeResponse);
  if (completedResource?.asset?.id !== assetId || completedResource?.asset?.state !== "ready") {
    throw new MediaUploadError("The uploaded media is not ready.");
  }
  return assetId;
};
