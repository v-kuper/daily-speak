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
      <div className="interview-question-card" aria-live="polite">
        <span className="interview-question-count">Question {current.seq}</span>
        <span>{current.question}</span>
      </div>

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
          ? "Finishing this answer before the next question…"
          : !hasAnswerEvidence
            ? "Say your answer before moving to the next question or finishing."
            : canAdvance
          ? "Answer when you are ready, then continue to the next question."
          : "Preparing another question. You can keep speaking or finish the recording."}
      </div>
      <button className="btn btn-secondary interview-next-btn" type="button" onClick={onNext} disabled={!canAdvance}>
        Next question →
      </button>
    </div>
  );
}
