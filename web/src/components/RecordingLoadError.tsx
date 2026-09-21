"use client";

import { recordingDetailState, retryRecordingFetch } from "../lib/routeFlows";
import { useAppSelector, useAppStore } from "../store/hooks";

export default function RecordingLoadError({ recordingId }: { recordingId: string }) {
  const store = useAppStore();
  const error = useAppSelector((state) => recordingDetailState(state.app, recordingId).error);
  const canRetry = useAppSelector((state) => recordingDetailState(state.app, recordingId).canRetry);
  if (!error) return null;
  return (
    <div className="auth-error" role="alert">
      <p>{error}</p>
      {canRetry && (
        <button className="btn btn-secondary btn-small" onClick={() => void retryRecordingFetch(store, recordingId)}>
          Retry loading recording
        </button>
      )}
    </div>
  );
}
