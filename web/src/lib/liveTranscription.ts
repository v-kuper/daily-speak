import { CartesiaRealtimeTranscriber } from "./cartesiaRealtime";

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
