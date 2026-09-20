export type ShadowingStatus = "pending" | "processing" | "ready" | "failed";

export const parseShadowingStatus = (value: unknown): ShadowingStatus => {
  if (value === "pending" || value === "processing" || value === "ready" || value === "failed") {
    return value;
  }
  return "pending";
};

export const shouldScheduleShadowing = ({
  recordingStatus,
  correctedTranscript,
  shadowingStatus,
  requestLoading,
}: {
  recordingStatus: "processing" | "ready" | "failed";
  correctedTranscript: string;
  shadowingStatus: ShadowingStatus;
  requestLoading: boolean;
}): boolean => {
  return (
    recordingStatus === "ready" &&
    correctedTranscript.trim().length > 0 &&
    shadowingStatus === "pending" &&
    !requestLoading
  );
};

export const shouldPollRecording = (
  recordingStatus: string,
  shadowingStatus: ShadowingStatus,
): boolean => {
  return recordingStatus === "processing" || shadowingStatus === "processing";
};

export const isShadowingStale = (
  status: ShadowingStatus,
  updatedAt: string,
  nowMs = Date.now(),
): boolean => {
  if (status !== "processing") {
    return false;
  }

  const updatedAtMs = Date.parse(updatedAt);
  if (!Number.isFinite(updatedAtMs)) {
    return true;
  }

  return nowMs - updatedAtMs >= 5 * 60 * 1000;
};

export const shadowingProgressLabel = (
  status: ShadowingStatus,
  stale: boolean,
): string => {
  if (status === "processing" && stale) {
    return "Pronunciation audio is taking longer than expected.";
  }
  return "Creating pronunciation audio...";
};
