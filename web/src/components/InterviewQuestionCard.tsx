"use client";

import type { InterviewTurn } from "../lib/interviewSession";

type InterviewQuestionCardProps = {
  turns: InterviewTurn[];
  canAdvance: boolean;
  onNext: () => void;
  liveTranscriptionAvailable: boolean;
  liveCaption: string | null;
  hasAnswerEvidence: boolean;
  boundaryPending: boolean;
};

export default function InterviewQuestionCard({
  turns,
  canAdvance,
  onNext,
  liveTranscriptionAvailable,
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
      <button
        className="interview-question-card"
        type="button"
        onClick={onNext}
        disabled={!canAdvance}
        aria-label={canAdvance ? `Go to the next question. Current question: ${current.question}` : undefined}
      >
        <span className="interview-question-count">Question {current.seq}</span>
        <span aria-live="polite">{current.question}</span>
      </button>

      {liveTranscriptionAvailable && liveCaption && (
        <div className="interview-live-caption" role="status" aria-live="polite" aria-atomic="true">
          {liveCaption}
        </div>
      )}
      {!liveTranscriptionAvailable && (
        <div className="notice interview-live-notice">
          {hasTerminalTranscriptionFailure
            ? "An answer could not be transcribed. Re-record the interview before saving."
            : "Live transcription is unavailable. Each completed answer will use the background transcription fallback."}
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
