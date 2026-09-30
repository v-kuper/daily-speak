import type { SavedInterviewTurn } from "../lib/interviewTimeline";

export default function ConversationTranscript({ turns, processing, answerKind = "original" }: {
  turns: ReadonlyArray<SavedInterviewTurn>;
  processing: boolean;
  answerKind?: "original" | "corrected";
}) {
  return (
    <div className="transcript-text conversation-transcript">
      {turns.map((turn) => {
        const text = answerKind === "corrected" ? turn.correctedAnswerText ?? "" : turn.answerText;
        return <div key={turn.sequence} className="conversation-turn">
          <p className="conversation-line">
            <strong className="conversation-speaker">Interviewer:</strong>{" "}{turn.question}
          </p>
          <p className="conversation-line conversation-answer">
            <strong className="conversation-speaker">You:</strong>{" "}
            {text.trim() ? text : <span className="conversation-answer-pending">
              {processing
                ? answerKind === "corrected" ? "Creating a natural answer…" : "Transcribing answer…"
                : answerKind === "corrected" ? "Natural answer is unavailable." : "No answer was recorded."}
            </span>}
          </p>
        </div>;
      })}
    </div>
  );
}
