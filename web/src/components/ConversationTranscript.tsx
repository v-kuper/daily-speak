import type { Suggestion } from "../lib/data";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import { buildConversationTranscriptTurns } from "../lib/transcriptHighlight";

type ConversationTranscriptProps = {
  turns: ReadonlyArray<SavedInterviewTurn>;
  suggestions: ReadonlyArray<Suggestion>;
  processing: boolean;
  answerKind?: "original" | "corrected";
};

export default function ConversationTranscript({
  turns,
  suggestions,
  processing,
  answerKind = "original",
}: ConversationTranscriptProps) {
  const conversation = buildConversationTranscriptTurns(turns, suggestions, answerKind);

  return (
    <div className="transcript-text conversation-transcript">
      {conversation.map((turn) => (
        <div key={turn.sequence} className="conversation-turn">
          <p className="conversation-line">
            <strong className="conversation-speaker">Interviewer:</strong>{" "}
            {turn.question}
          </p>
          <p className="conversation-line conversation-answer">
            <strong className="conversation-speaker">You:</strong>{" "}
            {turn.hasAnswer ? turn.answerSegments.map((segment, index) =>
              segment.isError ? (
                <mark
                  key={`answer-${turn.sequence}-${index}`}
                  className={`transcript-error-mark${segment.severity ? ` transcript-error-mark-${segment.severity}` : ""}`}
                >
                  {segment.text}
                </mark>
              ) : (
                <span key={`answer-${turn.sequence}-${index}`}>{segment.text}</span>
              )
            ) : (
              <span className="conversation-answer-pending">
                {processing
                  ? answerKind === "corrected" ? "Creating a natural answer…" : "Transcribing answer…"
                  : answerKind === "corrected" ? "Natural answer is unavailable." : "No answer was recorded."}
              </span>
            )}
          </p>
        </div>
      ))}
    </div>
  );
}
