import type { SavedInterviewTurn } from "./interviewTimeline";

export const interviewPracticeTurns = (turns: SavedInterviewTurn[] = []): SavedInterviewTurn[] =>
  turns.filter(turn => turn.answerText.trim()).sort((a, b) => a.sequence - b.sequence);

// Sequence identifies the server question; position identifies its place in the
// practice list. Skipped/unanswered turns must not become navigation targets.
export const interviewPracticeNavigation = (turns: SavedInterviewTurn[], sequence: number | null) => {
  const index = turns.findIndex(turn => turn.sequence === sequence);
  if (index < 0) return null;
  return {
    turn: turns[index],
    position: index + 1,
    total: turns.length,
    previous: turns[index - 1]?.sequence ?? null,
    next: turns[index + 1]?.sequence ?? null,
  };
};
