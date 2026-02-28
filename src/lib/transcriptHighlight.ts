export type TranscriptSegment = {
  text: string;
  isError: boolean;
};

type Match = {
  start: number;
  end: number;
  length: number;
};

const collectMatches = (transcript: string, phrases: string[]): Match[] => {
  const lowerTranscript = transcript.toLowerCase();
  const uniquePhrases = [...new Set(phrases.map((value) => value.trim().toLowerCase()).filter((value) => value))];
  const matches: Match[] = [];

  for (const phrase of uniquePhrases) {
    let fromIndex = 0;
    while (fromIndex < lowerTranscript.length) {
      const index = lowerTranscript.indexOf(phrase, fromIndex);
      if (index === -1) {
        break;
      }

      matches.push({
        start: index,
        end: index + phrase.length,
        length: phrase.length
      });
      fromIndex = index + phrase.length;
    }
  }

  return matches;
};

const selectNonOverlappingMatches = (matches: Match[]): Match[] => {
  const sorted = [...matches].sort((a, b) => {
    if (a.start !== b.start) {
      return a.start - b.start;
    }
    return b.length - a.length;
  });

  const selected: Match[] = [];
  let currentEnd = -1;

  for (const match of sorted) {
    if (match.start >= currentEnd) {
      selected.push(match);
      currentEnd = match.end;
    }
  }

  return selected;
};

export const buildTranscriptSegments = (transcript: string, wrongPhrases: string[]): TranscriptSegment[] => {
  if (!transcript) {
    return [];
  }

  const matches = selectNonOverlappingMatches(collectMatches(transcript, wrongPhrases));
  if (matches.length === 0) {
    return [{ text: transcript, isError: false }];
  }

  const segments: TranscriptSegment[] = [];
  let cursor = 0;

  for (const match of matches) {
    if (match.start > cursor) {
      segments.push({
        text: transcript.slice(cursor, match.start),
        isError: false
      });
    }

    segments.push({
      text: transcript.slice(match.start, match.end),
      isError: true
    });
    cursor = match.end;
  }

  if (cursor < transcript.length) {
    segments.push({
      text: transcript.slice(cursor),
      isError: false
    });
  }

  return segments;
};
