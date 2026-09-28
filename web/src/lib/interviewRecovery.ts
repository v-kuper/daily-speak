import type { BrowserIdentity } from "./identity";

const SESSION_KEY = "daily-speaking.interview.v1";
const LEASE_PREFIX = "daily-speaking.interview-owner.v1:";
const LEASE_MS = 12_000;

export type StoredInterview = {
  ownerToken: string;
  principalId: string;
  kind: BrowserIdentity["kind"];
  createKey: string;
  input: { topic: string; level: string; interestIds: string[] };
  sessionId: string | null;
  phase: "preparing" | "active" | "saving" | "abandoned";
  updatedAt: number;
};

type Lease = { ownerToken: string; expiresAt: number };
type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const parseRecord = (value: string | null): StoredInterview | null => {
  if (!value) return null;
  try {
    const record = JSON.parse(value) as Record<string, unknown>;
    const input = record.input && typeof record.input === "object" ? record.input as Record<string, unknown> : {};
    if (record.version !== 1 || typeof record.ownerToken !== "string" || !record.ownerToken ||
      typeof record.principalId !== "string" || !record.principalId ||
      (record.kind !== "guest" && record.kind !== "user") ||
      typeof record.createKey !== "string" || !record.createKey ||
      typeof input.topic !== "string" || !input.topic ||
      typeof input.level !== "string" ||
      !Array.isArray(input.interestIds) || !input.interestIds.every((id) => typeof id === "string") ||
      (record.sessionId !== null && typeof record.sessionId !== "string") ||
      !["preparing", "active", "saving", "abandoned"].includes(String(record.phase))) return null;
    return {
      ownerToken: record.ownerToken,
      principalId: record.principalId,
      kind: record.kind,
      createKey: record.createKey,
      input: { topic: input.topic, level: input.level, interestIds: input.interestIds as string[] },
      sessionId: record.sessionId as string | null,
      phase: record.phase as StoredInterview["phase"],
      updatedAt: Number.isFinite(record.updatedAt) ? Number(record.updatedAt) : 0,
    };
  } catch {
    return null;
  }
};

const parseLease = (value: string | null): Lease | null => {
  if (!value) return null;
  try {
    const lease = JSON.parse(value) as Record<string, unknown>;
    return typeof lease.ownerToken === "string" && Number.isFinite(lease.expiresAt)
      ? { ownerToken: lease.ownerToken, expiresAt: Number(lease.expiresAt) }
      : null;
  } catch {
    return null;
  }
};

export const createInterviewRecovery = (
  sessionStore: Store | null,
  sharedStore: Store | null,
  ownerToken: string,
  now: () => number = Date.now,
) => {
  const read = (): StoredInterview | null => {
    try { return parseRecord(sessionStore?.getItem(SESSION_KEY) ?? null); } catch { return null; }
  };
  const write = (record: Omit<StoredInterview, "ownerToken" | "updatedAt">): void => {
    if (!sessionStore) throw new Error("Browser session storage is required to prepare an interview.");
    sessionStore.setItem(SESSION_KEY, JSON.stringify({ version: 1, ...record, ownerToken, updatedAt: now() }));
  };
  const update = (createKey: string, changes: Partial<Pick<StoredInterview, "sessionId" | "phase">>): void => {
    const record = read();
    if (record?.createKey !== createKey || record.ownerToken !== ownerToken) return;
    try { write({ ...record, ...changes }); } catch { /* The in-memory session remains usable. */ }
  };
  const clear = (createKey?: string): void => {
    const record = read();
    if (createKey && record?.createKey !== createKey) return;
    try { sessionStore?.removeItem(SESSION_KEY); } catch { /* Recovery remains best effort. */ }
  };
  const leaseKey = (principalId: string) => `${LEASE_PREFIX}${principalId}`;
  const heldByOther = (principalId: string): boolean => {
    if (!sharedStore) return true;
    try {
      const lease = parseLease(sharedStore.getItem(leaseKey(principalId)));
      return Boolean(lease && lease.expiresAt > now() && lease.ownerToken !== ownerToken);
    } catch {
      return true;
    }
  };
  const claim = (principalId: string): boolean => {
    if (!sharedStore || heldByOther(principalId)) return false;
    try {
      sharedStore.setItem(leaseKey(principalId), JSON.stringify({ ownerToken, expiresAt: now() + LEASE_MS }));
      return parseLease(sharedStore.getItem(leaseKey(principalId)))?.ownerToken === ownerToken;
    } catch {
      return false;
    }
  };
  const renew = (principalId: string): void => {
    if (!sharedStore) return;
    try {
      if (parseLease(sharedStore.getItem(leaseKey(principalId)))?.ownerToken === ownerToken) {
        sharedStore.setItem(leaseKey(principalId), JSON.stringify({ ownerToken, expiresAt: now() + LEASE_MS }));
      }
    } catch { /* A later preparation will report unavailable storage. */ }
  };
  const release = (principalId: string): void => {
    if (!sharedStore) return;
    try {
      if (parseLease(sharedStore.getItem(leaseKey(principalId)))?.ownerToken === ownerToken) {
        sharedStore.removeItem(leaseKey(principalId));
      }
    } catch { /* Lease expires if storage cannot be changed. */ }
  };
  return { ownerToken, read, write, update, clear, heldByOther, claim, renew, release };
};

export type InterviewRecovery = ReturnType<typeof createInterviewRecovery>;

export const mayAbandonInterview = (
  sessionId: string | null,
  saving: boolean,
  protectedSessionId: string | null,
  storedPhase?: StoredInterview["phase"],
): boolean => !saving && storedPhase !== "saving" &&
  (!protectedSessionId || sessionId !== protectedSessionId);

type RecoverySession = { id: string; status: string };
type RecoveryServices = {
  rehydrate: (record: StoredInterview) => Promise<RecoverySession>;
  get: (id: string) => Promise<RecoverySession>;
  cancel: (id: string) => Promise<unknown>;
};

const savedStatus = (status: string): boolean =>
  status === "finalizing" || status === "finalized" || status === "cancelled";

export const recoverPreviousInterview = async (
  recovery: InterviewRecovery,
  principal: Pick<BrowserIdentity, "principalId" | "kind"> | null,
  services: RecoveryServices,
  isCurrent: () => boolean,
  now: () => number = Date.now,
): Promise<void> => {
  const record = recovery.read();
  if (!record || !isCurrent()) return;
  if (!principal || record.principalId !== principal.principalId || record.kind !== principal.kind) {
    recovery.clear(record.createKey);
    return;
  }
  if (!recovery.claim(principal.principalId)) {
    throw new Error("This interview is still open in another tab. Finish it there, or retry after closing that tab.");
  }
  try {
    if (!isCurrent()) return;
    const id = record.sessionId ?? (await services.rehydrate(record)).id;
    if (!isCurrent()) return;
    if (record.phase === "saving") {
      const session = await services.get(id);
      if (!isCurrent()) return;
      if (savedStatus(session.status)) {
        recovery.clear(record.createKey);
        return;
      }
      if (now() - record.updatedAt < 30_000) {
        throw new Error("The previous interview may still be saving. Retry preparation in a moment.");
      }
    }
    try {
      await services.cancel(id);
    } catch (error) {
      if (!isCurrent()) return;
      const session = await services.get(id);
      if (!savedStatus(session.status)) throw error;
    }
    if (isCurrent()) recovery.clear(record.createKey);
  } finally {
    recovery.release(principal.principalId);
  }
};

let browserRecovery: InterviewRecovery | null = null;
export const browserInterviewRecovery = (): InterviewRecovery => {
  if (!browserRecovery) {
    const getStore = (name: "sessionStorage" | "localStorage"): Store | null => {
      try { return typeof window === "undefined" ? null : window[name]; } catch { return null; }
    };
    browserRecovery = createInterviewRecovery(
      getStore("sessionStorage"),
      getStore("localStorage"),
      globalThis.crypto.randomUUID(),
    );
  }
  return browserRecovery;
};
