"use client";

import type { InterviewTurn } from "../lib/interviewSession";
import { formatTime } from "../lib/utils";

type InterviewQuestionCardProps = {
  turns: InterviewTurn[];
  canAdvance: boolean;
  onNext: () => void;
  liveTranscriptionAvailable: boolean;
};

export default function InterviewQuestionCard({
  turns,
  canAdvance,
  onNext,
  liveTranscriptionAvailable,
}: InterviewQuestionCardProps) {
  const current = turns[turns.length - 1];
  if (!current) return null;

  return (
    <div className="interview-question-panel">
      <div className="interview-question-card" aria-live="polite">
        <span className="interview-question-count">Question {current.seq}</span>
        <span>{current.question}</span>
      </div>
      <div className="interview-question-hint">
        {canAdvance
          ? "Answer when you are ready, then continue to the next question."
          : "Preparing another question. You can keep speaking or finish the recording."}
      </div>
      <button className="btn btn-secondary interview-next-btn" type="button" onClick={onNext} disabled={!canAdvance}>
        Next question →
      </button>

      <div className="interview-timeline">
        <div className="section-title">Interview timeline</div>
        {!liveTranscriptionAvailable && (
          <div className="notice">Live transcription is unavailable in this browser. Your complete audio will still be analyzed after saving.</div>
        )}
        <ol className="interview-timeline-list">
          {turns.map((turn) => (
            <li key={turn.seq} className="interview-timeline-item">
              <div className="interview-timeline-time">{formatTime(Math.floor(turn.askedAtMs / 1000))}</div>
              <div className="interview-timeline-question">{turn.question}</div>
              {turn.provisionalTranscript ? (
                <div className="interview-timeline-answer">{turn.provisionalTranscript}</div>
              ) : turn.endedAtMs !== null && liveTranscriptionAvailable ? (
                <div className="interview-timeline-pending">
                  {turn.transcriptStatus === "failed" ? "Live transcription failed. The complete recording will still be checked after saving." : "Transcribing answer…"}
                </div>
              ) : null}
            </li>
          ))}
        </ol>
      </div>
    </div>
  );
}
