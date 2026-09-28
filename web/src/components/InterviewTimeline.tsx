import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import { formatTime } from "../lib/utils";

export default function InterviewTimeline({ turns, processing }: { turns?: SavedInterviewTurn[]; processing: boolean }) {
  if (!turns?.length) return null;
  return (
    <div className="transcript-section">
      <div className="section-title">Interview timeline</div>
      <ol className="interview-timeline-list">
        {turns.map((turn) => (
          <li key={turn.sequence} className="interview-timeline-item">
            <div className="interview-timeline-time">{formatTime(Math.floor(turn.askedAtMs / 1000))} · Question {turn.sequence}</div>
            <div className="interview-timeline-question">{turn.question}</div>
            {turn.answerText ? (
              <div className="interview-timeline-answer">
                {turn.answerText}
                {turn.answerSource === "provisional" && <div className="interview-timeline-pending">Preliminary transcription</div>}
                {turn.answerAlignment === "approximate" && <div className="interview-timeline-pending">Matched to this question approximately by recording time</div>}
              </div>
            ) : (
              <div className="interview-timeline-pending">
                {processing ? "Transcribing answer…" : "No answer text available for this question."}
              </div>
            )}
          </li>
        ))}
      </ol>
    </div>
  );
}
