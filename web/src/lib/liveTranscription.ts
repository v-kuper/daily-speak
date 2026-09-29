import { CartesiaRealtimeTranscriber } from "./cartesiaRealtime";
import type { RealtimeFailureReason } from "./cartesiaRealtime";
export { RealtimeTranscriptionError } from "./cartesiaRealtime";
export type { RealtimeFailureReason } from "./cartesiaRealtime";

export const liveTranscriptionNotice = (reason: RealtimeFailureReason | null): string => {
  const cause = reason === "finalize_timeout"
    ? "Live captions paused while finishing the previous answer."
    : reason === "connect_timeout" || reason === "connect_failed"
      ? "Live captions could not connect."
      : reason === "provider_error"
        ? "The live transcription service paused."
        : "Live captions paused after a connection interruption.";
  return `${cause} Recording continues; you can keep answering. Completed answers will be transcribed in the background.`;
};

export type LiveTranscriptSnapshot = {
  turnSeq: number;
  finalText: string;
  interimText: string;
  captionText: string;
};

export type LiveTranscriptionConnection = {
  sendPCM: (pcm: Int16Array) => void;
  beginTurn: (turnSeq: number) => void;
  finalizeTurn: (turnSeq?: number | null) => Promise<string>;
  close: () => Promise<void>;
};

export type LiveTranscriptionConfig = {
  token: string;
  expiresAt: string;
  websocketUrl: string;
  model: string;
  encoding: string;
  sampleRate: number;
};

export const connectLiveTranscription = (
  config: LiveTranscriptionConfig,
  onFailure?: (error: Error) => void,
  onTranscript?: (snapshot: LiveTranscriptSnapshot) => void,
): Promise<LiveTranscriptionConnection> => CartesiaRealtimeTranscriber.connect(
  config,
  undefined,
  onFailure,
  onTranscript,
);
