import type { AppStore } from "../store";
import {
  cancelAuth,
  deleteRecording,
  openAuthForSave,
  saveRecording,
  showBackgroundRecordingSave,
  signIn,
  signUp,
  type AppState,
  type RecordingSaveDraft,
} from "../store/slices/appSlice";
import { parseHistoryDate, recordingPath, safeReturnTo } from "./routes";

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
      const result = await store.dispatch(saveRecording()).unwrap();
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
) {
  if (!draft.localRecordingId || store.getState().app.recordingSaveStatus === "loading") return;
  store.dispatch(showBackgroundRecordingSave(draft));
  router.push(recordingPath(draft.localRecordingId));
  try {
    if (finalUpload) await finalUpload;
    const result = await store.dispatch(saveRecording(draft)).unwrap();
    router.replace(recordingPath(result.recording.id));
  } catch {
    try {
      const result = await store.dispatch(saveRecording({ ...draft, recordingUploadSessionId: null })).unwrap();
      router.replace(recordingPath(result.recording.id));
    } catch {
      router.replace("/history");
    }
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
  const recording = error ? undefined : loaded;
  const shouldFetch = state.isAuthenticated && !recording && !error && state.recordingFetchStatuses[recordingId] !== "loading";
  return { recording, error, shouldFetch };
}
