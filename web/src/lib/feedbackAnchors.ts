export type ReviewKind = "correction" | "strength";

export const feedbackCardId = (kind: ReviewKind, index: number | string): string =>
  `feedback-${kind}-${index}`;

export const transcriptMarkId = (kind: ReviewKind, index: number | string): string =>
  `transcript-${kind}-${index}`;
