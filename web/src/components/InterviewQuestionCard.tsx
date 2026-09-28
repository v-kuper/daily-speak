"use client";

import type { InterviewTurn } from "../lib/interviewSession";

type InterviewQuestionCardProps = {
  turns: InterviewTurn[];
  canAdvance: boolean;
  onNext: () => void;
  liveTranscriptionAvailable: boolean;
  hasAnswerEvidence: boolean;
  boundaryPending: boolean;
};

export default function InterviewQuestionCard({
  turns,
  canAdvance,
  onNext,
  liveTranscriptionAvailable,
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

      <div className="interview-live-transcript">
        <div className="section-title">Conversation transcript</div>
        {liveTranscriptionAvailable && (
          <div className="interview-caption-hint">Live subtitles show what was recognized. You can rephrase before continuing.</div>
        )}
        {!liveTranscriptionAvailable && (
          <div className="notice">
            {hasTerminalTranscriptionFailure
              ? "An answer could not be transcribed. Re-record the interview before saving."
              : "Live transcription is unavailable. Each completed answer will use the background transcription fallback."}
          </div>
        )}
        <div className="transcript-text conversation-transcript" aria-live="polite" aria-relevant="text">
          {turns.map((turn) => {
            const hasLiveSnapshot = turn.liveTranscriptFinal !== undefined || turn.liveTranscriptInterim !== undefined;
            const finalText = hasLiveSnapshot ? turn.liveTranscriptFinal ?? "" : turn.provisionalTranscript;
            const interimText = hasLiveSnapshot ? turn.liveTranscriptInterim ?? "" : "";
            const hasAnswer = finalText.length > 0 || interimText.length > 0;
            const isCurrent = turn.seq === current.seq && turn.endedAtMs === null;
            return (
              <div key={turn.seq} className="conversation-turn">
                <p className="conversation-line">
                  <strong className="conversation-speaker">Interviewer:</strong>{" "}
                  {turn.question}
                </p>
                {turn.endedAtMs !== null && turn.transcriptStatus === "failed" ? (
                  <p className="conversation-line conversation-answer conversation-answer-pending">
                    <strong className="conversation-speaker">You:</strong>{" "}
                    This answer could not be transcribed. Re-record the interview before saving.
                  </p>
                ) : hasAnswer ? (
                  <p className="conversation-line conversation-answer">
                    <strong className="conversation-speaker">You:</strong>{" "}
                    {finalText}
                    {interimText && <span className="conversation-answer-interim">{interimText}</span>}
                  </p>
                ) : turn.endedAtMs !== null && liveTranscriptionAvailable ? (
                  <p className="conversation-line conversation-answer conversation-answer-pending">
                    <strong className="conversation-speaker">You:</strong>{" "}
                    Transcribing answer…
                  </p>
                ) : isCurrent && liveTranscriptionAvailable ? (
                  <p className="conversation-line conversation-answer conversation-answer-pending">
                    <strong className="conversation-speaker">You:</strong>{" "}
                    Start speaking — your words will appear here.
                  </p>
                ) : null}
              </div>
            );
          })}
        </div>
      </div>

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
