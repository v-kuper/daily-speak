import type { Strength, Suggestion, SuggestionSeverity } from "./data";
import { feedbackRanges, exactPhraseRanges } from "./feedbackSpans";
import type { SavedInterviewTurn } from "./interviewTimeline";

export type TranscriptSegment = {
  text: string;
  isError: boolean;
  isStrength?: boolean;
  severity: SuggestionSeverity | null;
  feedbackIndex?: number;
};

type HighlightSuggestion = Pick<Suggestion, "wrong" | "severity" | "span" | "id">;

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
  strengths: ReadonlyArray<Pick<Strength, "excerpt" | "span" | "id">> = [],
  turnSequence = 0
): TranscriptSegment[] => {
  if (!transcript) {
    return [];
  }

  const isError = Array.from({ length: transcript.length }, () => false);
  const severityRank = Array.from({ length: transcript.length }, () => -1);
  const severities = Array.from<SuggestionSeverity | null>({ length: transcript.length }).fill(null);
  const errorIndexes = Array.from<number | null>({ length: transcript.length }).fill(null);
  const strengthIndexes = Array.from<number | null>({ length: transcript.length }).fill(null);

  suggestions
    .map((suggestion, suggestionIndex) => ({ suggestion, suggestionIndex }))
    .sort((left, right) => right.suggestion.wrong.trim().length - left.suggestion.wrong.trim().length)
    .forEach(({ suggestion, suggestionIndex }) => {
      const phrase = typeof suggestion.wrong === "string" ? suggestion.wrong.trim() : "";
      if (!phrase || (suggestion.id && !suggestion.span)) {
        return;
      }

      const severity = parseSeverity(suggestion.severity);
      const rank = severity ? SEVERITY_RANK[severity] : 0;
      for (const { start, end } of feedbackRanges(transcript, phrase, suggestion.span, turnSequence)) {
        for (let index = start; index < end; index += 1) {
          isError[index] = true;
          if (rank > severityRank[index]) {
            severityRank[index] = rank;
            severities[index] = severity;
            errorIndexes[index] = suggestionIndex;
          }
        }

      }
    });

  strengths.forEach((strength, strengthIndex) => {
    const phrase = typeof strength.excerpt === "string" ? strength.excerpt.trim() : "";
    if (!phrase || (strength.id && !strength.span)) return;
    for (const { start, end } of feedbackRanges(transcript, phrase, strength.span, turnSequence)) {
      for (let index = start; index < end; index += 1) {
        if (!isError[index] && strengthIndexes[index] === null) {
          strengthIndexes[index] = strengthIndex;
        }
      }
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
  strengths: ReadonlyArray<Pick<Strength, "excerpt" | "span" | "id">> = [],
): ConversationTranscriptTurn[] => {
  // A legacy phrase must be unique across the complete interview, not per turn.
  const unique = (phrase: string) => turns.reduce((count, turn) => count + exactPhraseRanges(turn.answerText, phrase).length, 0) === 1;
  const safeSuggestions = suggestions.map(item => item.span || unique(item.wrong) ? item : { ...item, wrong: "" });
  const safeStrengths = strengths.map(item => item.span || unique(item.excerpt) ? item : { ...item, excerpt: "" });
  return turns.map((turn) => {
    const answerText = answerKind === "corrected" ? turn.correctedAnswerText ?? "" : turn.answerText;
    return {
      sequence: turn.sequence,
      question: turn.question,
      answerSegments: buildTranscriptSegments(answerText, answerKind === "corrected" ? [] : safeSuggestions, answerKind === "corrected" ? [] : safeStrengths, turn.sequence),
      hasAnswer: answerText.trim().length > 0,
    };
  });
};
