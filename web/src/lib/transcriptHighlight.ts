import type { Suggestion, SuggestionSeverity } from "./data";

export type TranscriptSegment = {
  text: string;
  isError: boolean;
  severity: SuggestionSeverity | null;
};

type HighlightSuggestion = Pick<Suggestion, "wrong" | "severity">;

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
  suggestions: ReadonlyArray<HighlightSuggestion>
): TranscriptSegment[] => {
  if (!transcript) {
    return [];
  }

  const isError = Array.from({ length: transcript.length }, () => false);
  const severityRank = Array.from({ length: transcript.length }, () => -1);
  const severities = Array.from<SuggestionSeverity | null>({ length: transcript.length }).fill(null);
  const lowerTranscript = transcript.toLowerCase();

  for (const suggestion of suggestions) {
    const phrase = typeof suggestion.wrong === "string" ? suggestion.wrong.trim().toLowerCase() : "";
    if (!phrase) {
      continue;
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
        }
      }

      fromIndex = start + 1;
    }
  }

  const segments: TranscriptSegment[] = [];
  let start = 0;

  for (let index = 1; index <= transcript.length; index += 1) {
    const boundary =
      index === transcript.length ||
      isError[index] !== isError[start] ||
      severities[index] !== severities[start];

    if (!boundary) {
      continue;
    }

    segments.push({
      text: transcript.slice(start, index),
      isError: isError[start],
      severity: isError[start] ? severities[start] : null
    });
    start = index;
  }

  return segments;
};
