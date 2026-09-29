"use client";

import type { InterviewTurn } from "../lib/interviewSession";
import { liveTranscriptionNotice, type RealtimeFailureReason } from "../lib/liveTranscription";
import type { QuestionSpeechState } from "../lib/questionSpeech";

type InterviewQuestionCardProps = {
  turns: InterviewTurn[];
  canAdvance: boolean;
  onNext: () => void;
  onListen: (question: string) => void;
  onToggleSpeechMuted: () => void;
  speechState: QuestionSpeechState;
  speechError: string | null;
  speechMuted: boolean;
  liveTranscriptionAvailable: boolean;
  liveTranscriptionIssue: RealtimeFailureReason | null;
  liveCaption: string | null;
  hasAnswerEvidence: boolean;
  boundaryPending: boolean;
};

export default function InterviewQuestionCard({
  turns,
  canAdvance,
  onNext,
  onListen,
  onToggleSpeechMuted,
  speechState,
  speechError,
  speechMuted,
  liveTranscriptionAvailable,
  liveTranscriptionIssue,
  liveCaption,
  hasAnswerEvidence,
  boundaryPending,
}: InterviewQuestionCardProps) {
  const current = turns[turns.length - 1];
  if (!current) return null;
  const hasTerminalTranscriptionFailure = turns.some((turn) =>
    turn.endedAtMs !== null && turn.transcriptStatus === "failed");

  return (
    <div className="interview-question-panel">
      <div className="interview-question-card">
        <button
          className="interview-question-advance"
          type="button"
          onClick={onNext}
          disabled={!canAdvance}
          aria-label={canAdvance ? `Go to the next question. Current question: ${current.question}` : undefined}
        >
          <span className="interview-question-count">Question {current.seq}</span>
          <span aria-live="polite">{current.question}</span>
        </button>
        <div className="interview-question-audio-controls">
          <button
            className="interview-question-listen"
            type="button"
            onClick={() => onListen(current.question)}
            disabled={speechMuted}
            aria-label={speechState === "playing" || speechState === "loading"
              ? "Stop question audio"
              : `Listen to the question again: ${current.question}`}
          >
            <span aria-hidden="true">{speechState === "playing" ? "■" : "▶"}</span>
            {speechState === "loading"
              ? "Loading…"
              : speechState === "playing"
                ? "Stop"
                : speechState === "error"
                  ? "Try again"
                  : "Listen again"}
          </button>
          <button
            className="interview-question-mute"
            type="button"
            onClick={onToggleSpeechMuted}
            aria-pressed={speechMuted}
            aria-label={speechMuted ? "Unmute automatic question audio" : "Mute automatic question audio"}
          >
            <span aria-hidden="true">{speechMuted ? "🔇" : "🔊"}</span>
            {speechMuted ? "Unmute questions" : "Mute questions"}
          </button>
        </div>
      </div>

      {speechError && <div className="interview-question-speech-error" role="status">{speechError}</div>}

      {liveTranscriptionAvailable && liveCaption && (
        <div className="interview-live-caption" role="status" aria-live="polite" aria-atomic="true">
          {liveCaption}
        </div>
      )}
      {!liveTranscriptionAvailable && (
        <div className="notice interview-live-notice">
          {hasTerminalTranscriptionFailure
            ? "An answer could not be transcribed. Re-record the interview before saving."
            : liveTranscriptionNotice(liveTranscriptionIssue)}
        </div>
      )}

      <div className="interview-question-hint">
        {boundaryPending
          ? "Finishing this turn before the next question…"
          : canAdvance && !hasAnswerEvidence
            ? "Answer when you want, or tap the question to skip it."
            : canAdvance
          ? "Tap the question when you are ready for the next one."
          : "Preparing another question. You can keep speaking or finish the recording."}
      </div>
    </div>
  );
}
