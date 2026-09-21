import type { AppStore } from "../store";
import {
  cancelAuth,
  deleteRecording,
  fetchRecording,
  finishFailedRecordingSave,
  openAuthForSave,
  saveRecording,
  selectRecording,
  showBackgroundRecordingSave,
  signIn,
  signUp,
  type AppState,
  type RecordingSaveDraft,
} from "../store/slices/appSlice";
import { parseHistoryDate, recordingPath, safeReturnTo } from "./routes";
import { shouldPollRecording } from "./shadowing";

type RouteNavigator = { push: (path: string) => void; replace: (path: string) => void };

export async function authenticateAndNavigate(
  store: AppStore, router: RouteNavigator, mode: "signIn" | "signUp", returnTo: string,
) {
  const state = store.getState().app;
  if (state.authStatus === "loading" || state.recordingSaveStatus === "loading") return;
  try {
    // A failed post-auth save can be retried without submitting the cleared password.
    if (!state.isAuthenticated) {
      const user = await store.dispatch(mode === "signIn" ? signIn() : signUp()).unwrap();
      if (!user) return;
    }
    if (store.getState().app.pendingSaveAfterAuth) {
      const result = await store.dispatch(saveRecording(store.getState().app.pendingAuthSaveDraft ?? undefined)).unwrap();
      router.replace(recordingPath(result.recording.id));
    } else {
      router.replace(safeReturnTo(returnTo));
    }
  } catch {
    // The rejected thunk exposes the error; stay here so the user can retry.
  }
}

export function cancelAuthentication(store: AppStore, router: RouteNavigator) {
  store.dispatch(cancelAuth());
  router.replace("/speak");
}

export function startGuestSave(store: AppStore, router: RouteNavigator) {
  store.dispatch(openAuthForSave());
  router.push("/auth?returnTo=%2Fspeak");
}

export async function saveAndNavigate(
  store: AppStore, router: RouteNavigator, draft: RecordingSaveDraft, finalUpload: Promise<void> | null,
  currentPath: () => string,
) {
  if (!draft.localRecordingId || store.getState().app.recordingSaveStatus === "loading") return;
  const localPath = recordingPath(draft.localRecordingId);
  store.dispatch(showBackgroundRecordingSave(draft));
  router.push(localPath);
  try {
    if (finalUpload) await finalUpload;
    await store.dispatch(saveRecording(draft)).unwrap();
  } catch {
    // A 401 invalidates the session and preserves the draft. The route guard owns re-authentication.
    if (!store.getState().app.isAuthenticated) return;
    try {
      await store.dispatch(saveRecording({ ...draft, recordingUploadSessionId: null })).unwrap();
    } catch {
      if (!store.getState().app.isAuthenticated) return;
      store.dispatch(finishFailedRecordingSave(draft.localRecordingId));
    }
  }
  reconcileRecordingSaveRoute(store, router, draft.localRecordingId, currentPath);
}

export function reconcileRecordingSaveRoute(store: AppStore, router: RouteNavigator, recordingId: string, currentPath: () => string) {
  const state = store.getState().app;
  const result = state.recordingSaveResults[recordingId];
  if (state.isAuthenticated && result !== undefined && currentPath() === recordingPath(recordingId)) {
    router.replace(result === null ? "/history" : recordingPath(result));
  }
}

export async function deleteAndNavigate(store: AppStore, router: RouteNavigator, recordingId: string) {
  try {
    await store.dispatch(deleteRecording(recordingId)).unwrap();
    router.replace("/history");
  } catch {
    // Keep details and its Redux deletion error visible.
  }
}

export function historyDateFromSearch(search: { getAll: (name: string) => string[] }) {
  const dates = search.getAll("date");
  return dates.length === 1 ? parseHistoryDate(dates[0]) : null;
}

export function recordingDetailState(state: AppState, recordingId: string) {
  const deleted = state.deletedRecordingIds.includes(recordingId);
  const loaded = state.recordings.find((recording) => recording.id === recordingId);
  const error = deleted ? "Recording not found." : state.recordingFetchErrors[recordingId]
    ?? (!loaded && recordingId.startsWith("local-") ? "Recording not found." : null);
  const terminal = deleted || state.recordingFetchFailureKinds[recordingId] === "terminal";
  const recording = terminal || !state.isAuthenticated ? undefined : loaded;
  const isLoading = state.recordingFetchStatuses[recordingId] === "loading";
  const shouldFetch = state.isAuthenticated && !recording && !error && !isLoading;
  const canRetry = state.isAuthenticated && !isLoading && !terminal && state.recordingFetchFailureKinds[recordingId] === "transient";
  return { recording, error, shouldFetch, canRetry, isLoading };
}

type PollScheduler = {
  setInterval: (callback: () => Promise<void>, delay: number) => unknown;
  clearInterval: (handle: unknown) => void;
};
const browserScheduler: PollScheduler = {
  setInterval: (callback, delay) => window.setInterval(callback, delay),
  clearInterval: (handle) => window.clearInterval(handle as number),
};

async function refreshRecording(store: AppStore, recordingId: string) {
  const state = store.getState().app;
  if (!state.isAuthenticated || recordingId.startsWith("local-")) return;
  const { recording, error, shouldFetch, isLoading } = recordingDetailState(state, recordingId);
  // Pause on errors until the user retries. Reads happen at each tick, including after a 401.
  if (!error && !isLoading && (shouldFetch || (recording && shouldPollRecording(recording.status, recording.shadowingStatus)))) {
    await store.dispatch(fetchRecording(recordingId));
  }
}

export function startRecordingDetailLifecycle(store: AppStore, recordingId: string, scheduler = browserScheduler) {
  store.dispatch(selectRecording(recordingId));
  const refresh = () => refreshRecording(store, recordingId);
  void refresh();
  const timer = scheduler.setInterval(refresh, 3000);
  return () => scheduler.clearInterval(timer);
}

export function startHistoryRecordingPolling(store: AppStore, scheduler = browserScheduler) {
  const refresh = async () => {
    for (const recording of store.getState().app.recordings) {
      if (recording.status === "processing") await refreshRecording(store, recording.id);
    }
  };
  void refresh();
  const timer = scheduler.setInterval(refresh, 5000);
  return () => scheduler.clearInterval(timer);
}

export async function retryRecordingFetch(store: AppStore, recordingId: string) {
  if (recordingDetailState(store.getState().app, recordingId).canRetry) {
    await store.dispatch(fetchRecording(recordingId));
  }
}
