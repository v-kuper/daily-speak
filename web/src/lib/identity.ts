import {
  apiFetchUnauthenticated,
  configureApiAuthorization,
  readApiJSON,
} from "./apiClient";

type IdentityKind = "guest" | "user";

export type IdentityUser = {
  email: string;
  isSubscriber: boolean;
  englishLevel: string;
};

export type IdentityPromotion = {
  status: "promoted" | "not_promoted" | "no_preview";
  previewId?: string;
  recordingId?: string;
  reason?: "promotion_already_used" | "quota_exceeded";
};

export type BrowserIdentity = {
  principalId: string;
  kind: IdentityKind;
  accessToken: string;
  accessTokenExpiresAt: string;
  user: IdentityUser | null;
  guestPreviewPromotion?: IdentityPromotion;
};

type IdentityEnvelope = {
  principal?: { id?: unknown; type?: unknown };
  user?: { email?: unknown; isSubscriber?: unknown; englishLevel?: unknown };
  tokens?: {
    tokenType?: unknown;
    accessToken?: unknown;
    accessTokenExpiresAt?: unknown;
  };
  guestPreviewPromotion?: unknown;
  error?: { code?: unknown; message?: unknown };
};

export class IdentityError extends Error {
  constructor(message: string, readonly code = "identity_failed") {
    super(message);
    this.name = "IdentityError";
  }
}

let currentIdentity: BrowserIdentity | null = null;
let refreshPromise: Promise<BrowserIdentity> | null = null;

const isFuture = (value: string, leewayMilliseconds = 0): boolean => {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && timestamp > Date.now() + leewayMilliseconds;
};

const parsePromotion = (value: unknown): IdentityPromotion | undefined => {
  if (!value || typeof value !== "object") return undefined;
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
  if (candidate.status === "no_preview") return { status: "no_preview" };
  return undefined;
};

const parseIdentity = (payload: IdentityEnvelope | null): BrowserIdentity | null => {
  const principal = payload?.principal;
  const tokens = payload?.tokens;
  if (
    !principal || typeof principal.id !== "string" || !principal.id ||
    (principal.type !== "guest" && principal.type !== "user") ||
    tokens?.tokenType !== "Bearer" ||
    typeof tokens.accessToken !== "string" || !tokens.accessToken ||
    typeof tokens.accessTokenExpiresAt !== "string"
  ) return null;

  let user: IdentityUser | null = null;
  if (principal.type === "user") {
    const candidate = payload?.user;
    if (
      !candidate || typeof candidate.email !== "string" ||
      typeof candidate.isSubscriber !== "boolean" ||
      typeof candidate.englishLevel !== "string"
    ) return null;
    user = {
      email: candidate.email,
      isSubscriber: candidate.isSubscriber,
      englishLevel: candidate.englishLevel,
    };
  }

  return {
    principalId: principal.id,
    kind: principal.type,
    accessToken: tokens.accessToken,
    accessTokenExpiresAt: tokens.accessTokenExpiresAt,
    user,
    guestPreviewPromotion: parsePromotion(payload?.guestPreviewPromotion),
  };
};

const identityError = async (response: Response, fallback: string): Promise<IdentityError> => {
  const payload = await readApiJSON<IdentityEnvelope>(response).catch(() => null);
  const code = typeof payload?.error?.code === "string" ? payload.error.code : "identity_failed";
  const message = typeof payload?.error?.message === "string" ? payload.error.message : fallback;
  return new IdentityError(message, code);
};

const acceptIdentity = async (response: Response, fallback: string): Promise<BrowserIdentity> => {
  if (!response.ok) throw await identityError(response, fallback);
  const identity = parseIdentity(await readApiJSON<IdentityEnvelope>(response));
  if (!identity) throw new IdentityError("The identity response is invalid.");
  currentIdentity = identity;
  return identity;
};

const refreshBrowserIdentity = async (): Promise<BrowserIdentity> => {
  const response = await apiFetchUnauthenticated("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  try {
    return await acceptIdentity(response, "The session has expired.");
  } catch (error) {
    currentIdentity = null;
    throw error;
  }
};

const refreshOnce = (): Promise<BrowserIdentity> => {
  if (!refreshPromise) {
    refreshPromise = refreshBrowserIdentity().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
};

const provideAccessToken = async (forceRefresh: boolean, rejectedToken?: string): Promise<string | null> => {
  if (!currentIdentity) return null;
  if (
    forceRefresh && rejectedToken && currentIdentity.accessToken !== rejectedToken &&
    isFuture(currentIdentity.accessTokenExpiresAt, 30_000)
  ) {
    return currentIdentity.accessToken;
  }
  if (!forceRefresh && isFuture(currentIdentity.accessTokenExpiresAt, 30_000)) {
    return currentIdentity.accessToken;
  }
  try {
    return (await refreshOnce()).accessToken;
  } catch {
    return null;
  }
};

configureApiAuthorization(provideAccessToken);

export const restoreBrowserIdentity = (): Promise<BrowserIdentity> => refreshOnce();

export const browserIdentity = (): BrowserIdentity | null => currentIdentity;

export const forgetBrowserIdentity = (): void => {
  currentIdentity = null;
};

export const createAnonymousIdentity = async (): Promise<BrowserIdentity> => {
  const response = await apiFetchUnauthenticated("/api/v1/auth/anonymous", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceName: "Daily Speaking Web", platform: "web" }),
  });
  return acceptIdentity(response, "Cannot start a guest session.");
};

export const authenticateBrowserIdentity = async (
  mode: "signIn" | "signUp",
  email: string,
  password: string,
  promoteGuest = false,
): Promise<BrowserIdentity> => {
  if (promoteGuest && (!currentIdentity || !isFuture(currentIdentity.accessTokenExpiresAt, 30_000))) {
    try {
      await refreshOnce();
    } catch {
      throw new IdentityError("The guest preview session is missing or expired.", "guest_session_expired");
    }
  }
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (promoteGuest && currentIdentity?.kind === "guest") {
    headers.Authorization = `Bearer ${currentIdentity.accessToken}`;
  }
  const response = await apiFetchUnauthenticated(`/api/v1/auth/${mode === "signUp" ? "register" : "login"}`, {
    method: "POST",
    headers,
    body: JSON.stringify({ email, password, deviceName: "Daily Speaking Web", platform: "web" }),
  });
  return acceptIdentity(response, mode === "signUp" ? "Failed to create account." : "Failed to sign in.");
};

export const logoutBrowserIdentity = async (): Promise<void> => {
  let identity = currentIdentity;
  if (!identity || !isFuture(identity.accessTokenExpiresAt, 30_000)) {
    try {
      identity = await refreshOnce();
    } catch {
      currentIdentity = null;
      return;
    }
  }
  try {
    await apiFetchUnauthenticated("/api/v1/auth/logout", {
      method: "POST",
      headers: { Authorization: `Bearer ${identity.accessToken}` },
    });
  } finally {
    currentIdentity = null;
  }
};
