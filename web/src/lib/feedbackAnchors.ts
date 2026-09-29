export type ReviewKind = "correction" | "strength";

export const feedbackCardId = (kind: ReviewKind, index: number): string =>
  `feedback-${kind}-${index}`;

export const transcriptMarkId = (kind: ReviewKind, index: number): string =>
  `transcript-${kind}-${index}`;
