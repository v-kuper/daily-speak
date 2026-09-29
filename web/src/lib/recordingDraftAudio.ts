const DATABASE_NAME = "daily-speaking-media";
const DATABASE_VERSION = 1;
const STORE_NAME = "recording-audio-v1";
const KEY_PREFIX = "recording-audio:";
const MAX_AUDIO_BYTES = 80 * 1024 * 1024;
const MAX_DRAFT_AGE_MS = 7 * 24 * 60 * 60 * 1000;

export class RecordingDraftAudioError extends Error {
  constructor(message: string, readonly code: "invalid_audio" | "missing_audio") {
    super(message);
    this.name = "RecordingDraftAudioError";
  }
}

type StoredAudio = {
  key: string;
  blob: Blob;
  createdAt: number;
};

const memory = new Map<string, Blob>();
let databasePromise: Promise<IDBDatabase | null> | null = null;

const validKey = (value: unknown): value is string =>
  typeof value === "string" && value.startsWith(KEY_PREFIX) && value.length <= 128;

const openDatabase = (): Promise<IDBDatabase | null> => {
  if (databasePromise) return databasePromise;
  databasePromise = new Promise((resolve) => {
    if (typeof indexedDB === "undefined") {
      resolve(null);
      return;
    }
    try {
      const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
      request.onupgradeneeded = () => {
        const database = request.result;
        if (!database.objectStoreNames.contains(STORE_NAME)) {
          const store = database.createObjectStore(STORE_NAME, { keyPath: "key" });
          store.createIndex("createdAt", "createdAt");
        }
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => resolve(null);
      request.onblocked = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
  return databasePromise;
};

const transactionDone = (transaction: IDBTransaction): Promise<void> => new Promise((resolve, reject) => {
  transaction.oncomplete = () => resolve();
  transaction.onerror = () => reject(transaction.error ?? new Error("Audio draft storage failed."));
  transaction.onabort = () => reject(transaction.error ?? new Error("Audio draft storage was aborted."));
});

const removeExpiredDrafts = async (database: IDBDatabase, now: number): Promise<void> => {
  const transaction = database.transaction(STORE_NAME, "readwrite");
  const store = transaction.objectStore(STORE_NAME);
  const keys = await new Promise<IDBValidKey[]>((resolve, reject) => {
    const request = store.index("createdAt").getAllKeys(IDBKeyRange.upperBound(now - MAX_DRAFT_AGE_MS));
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  for (const key of keys) store.delete(key);
  await transactionDone(transaction);
};

export const storeRecordingDraftAudio = async (blob: Blob): Promise<string> => {
  if (
    blob.size <= 0
    || blob.size > MAX_AUDIO_BYTES
    || (!blob.type.startsWith("audio/") && !blob.type.startsWith("video/"))
  ) {
    throw new RecordingDraftAudioError("Recorded audio is invalid.", "invalid_audio");
  }
  const key = `${KEY_PREFIX}${globalThis.crypto.randomUUID()}`;
  memory.set(key, blob);
  const database = await openDatabase();
  if (!database) return key;
  try {
    const transaction = database.transaction(STORE_NAME, "readwrite");
    const now = Date.now();
    transaction.objectStore(STORE_NAME).put({ key, blob, createdAt: now } satisfies StoredAudio);
    await transactionDone(transaction);
    await removeExpiredDrafts(database, now);
  } catch {
    // The in-memory copy still supports save and retry in this tab.
  }
  return key;
};

export const loadRecordingDraftAudio = async (key: string): Promise<Blob> => {
  if (!validKey(key)) {
    throw new RecordingDraftAudioError("Recorded audio reference is invalid.", "invalid_audio");
  }
  const cached = memory.get(key);
  if (cached) return cached;
  const database = await openDatabase();
  if (database) {
    try {
      const transaction = database.transaction(STORE_NAME, "readwrite");
      const store = transaction.objectStore(STORE_NAME);
      const stored = await new Promise<StoredAudio | undefined>((resolve, reject) => {
        const request = store.get(key);
        request.onsuccess = () => resolve(request.result as StoredAudio | undefined);
        request.onerror = () => reject(request.error);
      });
      if (stored?.blob instanceof Blob && stored.blob.size > 0) {
        if (!Number.isFinite(stored.createdAt) || stored.createdAt <= Date.now() - MAX_DRAFT_AGE_MS) {
          store.delete(key);
          memory.delete(key);
          throw new RecordingDraftAudioError(
            "Recorded audio is no longer available. Record it again.",
            "missing_audio",
          );
        }
        memory.set(key, stored.blob);
        return stored.blob;
      }
    } catch (error) {
      if (error instanceof RecordingDraftAudioError) throw error;
      // Report the same stable missing-draft error below.
    }
  }
  throw new RecordingDraftAudioError(
    "Recorded audio is no longer available. Record it again.",
    "missing_audio",
  );
};

export const deleteRecordingDraftAudio = async (key: string | null | undefined): Promise<void> => {
  if (!validKey(key)) return;
  memory.delete(key);
  const database = await openDatabase();
  if (!database) return;
  try {
    const transaction = database.transaction(STORE_NAME, "readwrite");
    transaction.objectStore(STORE_NAME).delete(key);
    await transactionDone(transaction);
  } catch {
    // Automatic trimming will eventually remove a stale entry.
  }
};

export const isRecordingDraftAudioKey = validKey;
