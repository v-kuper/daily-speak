import type { InterviewCandidate, InterviewSession, InterviewTurn } from "./interviewSession";

export type InterviewAdvance = {
  candidate: InterviewCandidate;
  previousTurn: InterviewTurn;
  session: InterviewSession;
};

export const MIN_ANSWER_MS = 300;
export const MAX_LIVE_SEGMENT_ATTEMPTS = 3;

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

export const withoutInterviewTimeline = <T extends {
  interviewSessionId?: string;
  interviewEndedAtMs?: number;
}>(draft: T): Omit<T, "interviewSessionId" | "interviewEndedAtMs"> => {
  const { interviewSessionId: _sessionId, interviewEndedAtMs: _endedAtMs, ...plain } = draft;
  return plain;
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
