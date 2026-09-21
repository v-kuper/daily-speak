import type { RecordingProcessingStage, RecordingStatus } from "./data";
import type { ShadowingStatus } from "./shadowing";

const RECORDING_PROCESSING_STAGES = new Set<RecordingProcessingStage>([
  "transcribing",
  "suggestions",
  "rewriting"
]);

export const parseRecordingProcessingStage = (value: unknown): RecordingProcessingStage | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().toLowerCase() as RecordingProcessingStage;
  return RECORDING_PROCESSING_STAGES.has(normalized) ? normalized : null;
};

export const recordingProcessingLabel = (stage: RecordingProcessingStage | null): string => {
  switch (stage) {
    case "transcribing":
      return "Transcribing audio...";
    case "suggestions":
      return "Analyzing your English...";
    case "rewriting":
      return "Creating a natural version...";
    default:
      return "Processing recording...";
  }
};

export const recordingRetryLabel = (stage: RecordingProcessingStage | null): string | null => {
  switch (stage) {
    case "transcribing":
      return "Retry transcription";
    case "suggestions":
      return "Retry AI analysis";
    case "rewriting":
      return "Retry natural version";
    default:
      return null;
  }
};

export const shouldShowShadowingProgress = ({
  recordingStatus,
  correctedTranscript,
  shadowingStatus,
}: {
  recordingStatus: RecordingStatus;
  correctedTranscript: string;
  shadowingStatus: ShadowingStatus;
}): boolean =>
  recordingStatus === "ready" &&
  correctedTranscript.trim().length > 0 &&
  (shadowingStatus === "pending" || shadowingStatus === "processing");
