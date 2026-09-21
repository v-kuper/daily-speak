const normalizeGuidanceText = (value: string): string => {
  return value.trim().replace(/\s+/g, " ");
};

export const buildInterviewQuestions = (topic: string, followUps: string[]): string[] => {
  const questions: string[] = [];
  const seen = new Set<string>();

  for (const candidate of [topic, ...followUps]) {
    const normalized = normalizeGuidanceText(candidate);
    const key = normalized.toLocaleLowerCase();
    if (!normalized || seen.has(key)) {
      continue;
    }
    seen.add(key);
    questions.push(normalized);
  }

  return questions;
};

export const moveInterviewQuestion = (currentIndex: number, direction: -1 | 1, questionCount: number): number => {
  if (!Number.isFinite(questionCount) || questionCount <= 0) {
    return 0;
  }
  const lastIndex = Math.max(0, Math.floor(questionCount) - 1);
  const normalizedIndex = Math.max(0, Math.min(lastIndex, Math.floor(currentIndex)));
  return Math.max(0, Math.min(lastIndex, normalizedIndex + direction));
};

export const normalizeTickerOffset = (offset: number, cycleWidth: number): number => {
  if (!Number.isFinite(offset) || !Number.isFinite(cycleWidth) || cycleWidth <= 0) {
    return 0;
  }
  return ((offset % cycleWidth) + cycleWidth) % cycleWidth;
};

export const isCurrentInterviewGuidanceRequest = (
  activeRequestId: string | null,
  settledRequestId: string
): boolean => {
  return activeRequestId !== null && activeRequestId === settledRequestId;
};

export const buildInterviewGuidanceRequestKey = (
  topic: string,
  interestIds: string[],
  englishLevel: string
): string => {
  return JSON.stringify([normalizeGuidanceText(topic), [...interestIds].sort(), englishLevel]);
};
