import type { RecordingProcessingStage } from "./data";

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
