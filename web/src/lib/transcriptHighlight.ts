import type { Strength, Suggestion, SuggestionSeverity } from "./data";
import type { SavedInterviewTurn } from "./interviewTimeline";

export type TranscriptSegment = {
  text: string;
  isError: boolean;
  isStrength?: boolean;
  severity: SuggestionSeverity | null;
  feedbackIndex?: number;
};

type HighlightSuggestion = Pick<Suggestion, "wrong" | "severity">;

export type ConversationTranscriptTurn = {
  sequence: number;
  question: string;
  answerSegments: TranscriptSegment[];
  hasAnswer: boolean;
};

const SEVERITY_RANK: Record<SuggestionSeverity, number> = {
  major: 3,
  medium: 2,
  minor: 1
};

const parseSeverity = (value: unknown): SuggestionSeverity | null => {
  return value === "major" || value === "medium" || value === "minor" ? value : null;
};

export const buildTranscriptSegments = (
  transcript: string,
  suggestions: ReadonlyArray<HighlightSuggestion>,
  strengths: ReadonlyArray<Pick<Strength, "excerpt">> = []
): TranscriptSegment[] => {
  if (!transcript) {
    return [];
  }

  const isError = Array.from({ length: transcript.length }, () => false);
  const severityRank = Array.from({ length: transcript.length }, () => -1);
  const severities = Array.from<SuggestionSeverity | null>({ length: transcript.length }).fill(null);
  const errorIndexes = Array.from<number | null>({ length: transcript.length }).fill(null);
  const strengthIndexes = Array.from<number | null>({ length: transcript.length }).fill(null);
  const lowerTranscript = transcript.toLowerCase();

  suggestions
    .map((suggestion, suggestionIndex) => ({ suggestion, suggestionIndex }))
    .sort((left, right) => right.suggestion.wrong.trim().length - left.suggestion.wrong.trim().length)
    .forEach(({ suggestion, suggestionIndex }) => {
      const phrase = typeof suggestion.wrong === "string" ? suggestion.wrong.trim().toLowerCase() : "";
      if (!phrase) {
        return;
      }

      const severity = parseSeverity(suggestion.severity);
      const rank = severity ? SEVERITY_RANK[severity] : 0;
      let fromIndex = 0;

      while (fromIndex < lowerTranscript.length) {
        const start = lowerTranscript.indexOf(phrase, fromIndex);
        if (start === -1) {
          break;
        }

        const end = start + phrase.length;
        for (let index = start; index < end; index += 1) {
          isError[index] = true;
          if (rank > severityRank[index]) {
            severityRank[index] = rank;
            severities[index] = severity;
            errorIndexes[index] = suggestionIndex;
          }
        }

        fromIndex = start + 1;
      }
    });

  strengths.forEach((strength, strengthIndex) => {
    const phrase = typeof strength.excerpt === "string" ? strength.excerpt.trim().toLowerCase() : "";
    if (!phrase) return;
    let fromIndex = 0;
    while (fromIndex < lowerTranscript.length) {
      const start = lowerTranscript.indexOf(phrase, fromIndex);
      if (start === -1) break;
      for (let index = start; index < start + phrase.length; index += 1) {
        if (!isError[index] && strengthIndexes[index] === null) {
          strengthIndexes[index] = strengthIndex;
        }
      }
      fromIndex = start + 1;
    }
  });

  const segments: TranscriptSegment[] = [];
  let start = 0;

  for (let index = 1; index <= transcript.length; index += 1) {
    const boundary =
      index === transcript.length ||
      isError[index] !== isError[start] ||
      severities[index] !== severities[start] ||
      errorIndexes[index] !== errorIndexes[start] ||
      strengthIndexes[index] !== strengthIndexes[start];

    if (!boundary) {
      continue;
    }

    const error = isError[start];
    const strength = !error && strengthIndexes[start] !== null;
    segments.push({
      text: transcript.slice(start, index),
      isError: error,
      severity: error ? severities[start] : null,
      ...(strength ? { isStrength: true } : {}),
      ...((error || strength) ? { feedbackIndex: (error ? errorIndexes[start] : strengthIndexes[start]) ?? 0 } : {})
    });
    start = index;
  }

  return segments;
};

export const buildConversationTranscriptTurns = (
  turns: ReadonlyArray<SavedInterviewTurn>,
  suggestions: ReadonlyArray<HighlightSuggestion>,
  answerKind: "original" | "corrected" = "original",
  strengths: ReadonlyArray<Pick<Strength, "excerpt">> = [],
): ConversationTranscriptTurn[] => turns.map((turn) => {
  const answerText = answerKind === "corrected" ? turn.correctedAnswerText ?? "" : turn.answerText;
  return {
    sequence: turn.sequence,
    question: turn.question,
    answerSegments: buildTranscriptSegments(answerText, answerKind === "corrected" ? [] : suggestions, answerKind === "corrected" ? [] : strengths),
    hasAnswer: answerText.trim().length > 0,
  };
});
