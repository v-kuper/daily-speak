import type { InterviewCandidate, InterviewSession, InterviewTurn } from "./interviewSession";

export type InterviewAdvance = {
  candidate: InterviewCandidate;
  previousTurn: InterviewTurn;
  session: InterviewSession;
};

export const MIN_ANSWER_MS = 300;
export const MAX_LIVE_SEGMENT_ATTEMPTS = 3;

export const hasInterviewAnswerEvidence = (
  turn: InterviewTurn | undefined,
  hasPCMSpeechActivity: boolean,
): boolean => Boolean(
  hasPCMSpeechActivity
  || turn?.liveTranscriptFinal?.trim()
  || turn?.liveTranscriptInterim?.trim()
  || turn?.provisionalTranscript.trim(),
);

export const resolveInterviewRecordingLimitSeconds = ({
  isAuthenticated,
  authenticatedLimitSeconds,
  guestLimitSeconds,
  interviewLimitSeconds,
}: {
  isAuthenticated: boolean;
  authenticatedLimitSeconds: number;
  guestLimitSeconds: number;
  interviewLimitSeconds: number | null;
}): number => {
  const localLimitSeconds = isAuthenticated ? authenticatedLimitSeconds : guestLimitSeconds;
  if (interviewLimitSeconds === null || !Number.isFinite(interviewLimitSeconds) || interviewLimitSeconds <= 0) {
    return localLimitSeconds;
  }
  return Math.min(localLimitSeconds, Math.floor(interviewLimitSeconds));
};

export const rotateFailedInterviewSegment = <T extends { attempts: number }>(
  queue: T[],
  failed: T,
  maxAttempts = MAX_LIVE_SEGMENT_ATTEMPTS,
): { queue: T[]; dropped: boolean } => {
  const rest = queue.filter((item) => item !== failed);
  const next = { ...failed, attempts: failed.attempts + 1 };
  if (next.attempts >= maxAttempts) return { queue: rest, dropped: true };
  return { queue: [...rest, next], dropped: false };
};

/** A missing prepared candidate leaves the current question and answer open. */
export const advanceInterviewTimeline = (
  session: InterviewSession,
  usedCandidateIds: ReadonlySet<string>,
  atMs: number,
  maxAtMs = Number.POSITIVE_INFINITY,
): InterviewAdvance | null => {
  const previousTurn = session.turns[session.turns.length - 1];
  const candidate = session.candidates.find((item) => !usedCandidateIds.has(item.id));
  if (!previousTurn || !candidate) return null;
  if (atMs - previousTurn.askedAtMs < MIN_ANSWER_MS) return null;
  const boundary = Math.max(previousTurn.askedAtMs + 1, Math.floor(atMs));
  if (boundary >= maxAtMs) return null;
  const nextTurn: InterviewTurn = {
    seq: previousTurn.seq + 1,
    question: candidate.question,
    askedAtMs: boundary,
    endedAtMs: null,
    provisionalTranscript: "",
  };
  return {
    candidate,
    previousTurn,
    session: {
      ...session,
      candidates: session.candidates.filter((item) => item.id !== candidate.id),
      turns: [
        ...session.turns.map((turn) => turn.seq === previousTurn.seq ? { ...turn, endedAtMs: boundary } : turn),
        nextTurn,
      ],
      currentTurnSeq: nextTurn.seq,
    },
  };
};

/**
 * Commits a prepared advance only after the audio worklet confirms its turn
 * boundary. Preserve any subtitle or server updates received while that
 * boundary was pending instead of replacing the previous turn with the stale
 * snapshot used to prepare the advance.
 */
export const commitInterviewAdvance = (
  current: InterviewSession,
  advance: InterviewAdvance,
): InterviewSession | null => {
  if (current.id !== advance.session.id) return null;
  const currentTurn = current.turns[current.turns.length - 1];
  const endedTurn = advance.session.turns.find((turn) => turn.seq === advance.previousTurn.seq);
  const nextTurn = advance.session.turns[advance.session.turns.length - 1];
  if (!currentTurn || currentTurn.seq !== advance.previousTurn.seq || currentTurn.endedAtMs !== null
    || !endedTurn || endedTurn.endedAtMs === null || !nextTurn || nextTurn.seq === currentTurn.seq) {
    return null;
  }
  return {
    ...current,
    candidates: current.candidates.filter((candidate) => candidate.id !== advance.candidate.id),
    turns: [
      ...current.turns.slice(0, -1),
      { ...currentTurn, endedAtMs: endedTurn.endedAtMs },
      nextTurn,
    ],
    currentTurnSeq: nextTurn.seq,
  };
};
