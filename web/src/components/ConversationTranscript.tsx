import type { Strength, Suggestion } from "../lib/data";
import { transcriptMarkId, type ReviewKind } from "../lib/feedbackAnchors";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import { buildConversationTranscriptTurns } from "../lib/transcriptHighlight";

type ConversationTranscriptProps = {
  turns: ReadonlyArray<SavedInterviewTurn>;
  suggestions: ReadonlyArray<Suggestion>;
  processing: boolean;
  answerKind?: "original" | "corrected";
  strengths?: ReadonlyArray<Strength>;
  onReviewSelect?: (kind: ReviewKind, index: number) => void;
};

export default function ConversationTranscript({
  turns,
  suggestions,
  processing,
  answerKind = "original",
  strengths = [],
  onReviewSelect,
}: ConversationTranscriptProps) {
  const conversation = buildConversationTranscriptTurns(turns, suggestions, answerKind, strengths);
  const markedTargets = new Set<string>();

  const renderSegment = (segment: ReturnType<typeof buildConversationTranscriptTurns>[number]["answerSegments"][number], key: string) => {
    if (!segment.isError && !segment.isStrength) {
      return <span key={key}>{segment.text}</span>;
    }
    const kind: ReviewKind = segment.isError ? "correction" : "strength";
    const index = segment.feedbackIndex ?? 0;
    const targetKey = `${kind}-${index}`;
    const firstOccurrence = !markedTargets.has(targetKey);
    markedTargets.add(targetKey);
    return (
      <mark
        key={key}
        id={firstOccurrence ? transcriptMarkId(kind, (kind === "correction" ? suggestions[index]?.id : strengths[index]?.id) ?? index) : undefined}
        data-feedback-kind={kind}
        data-feedback-index={index}
        className={segment.isError
          ? `transcript-error-mark${segment.severity ? ` transcript-error-mark-${segment.severity}` : ""}`
          : "transcript-strength-mark"}
        role="button"
        tabIndex={0}
        aria-controls={`feedback-${kind}-${(kind === "correction" ? suggestions[index]?.id : strengths[index]?.id) ?? index}`}
        onClick={() => onReviewSelect?.(kind, index)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onReviewSelect?.(kind, index);
          }
        }}
      >
        {segment.text}
      </mark>
    );
  };

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
              renderSegment(segment, `answer-${turn.sequence}-${index}`)
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
