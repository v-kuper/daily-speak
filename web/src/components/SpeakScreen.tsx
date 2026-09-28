"use client";

import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import { useRouter } from "next/navigation";
import { saveAndNavigate, startGuestSave } from "../lib/routeFlows";
import {
  createGuestPreview,
  ensureGuestPreviewIdentity,
  guestPreviewPath,
  MAX_GUEST_PREVIEW_SECONDS,
  startNewGuestPreviewSession,
} from "../lib/guestPreview";
import { browserIdentity, restoreBrowserIdentity } from "../lib/identity";
import { browserInterviewRecovery, mayAbandonInterview, recoverPreviousInterview } from "../lib/interviewRecovery";
import {
  readBlobAsDataUrl,
  recordingElapsedMs,
  recordingMustStop,
  resolveBrowserRecordingSupportError,
  resolveMicrophoneError,
  resolveRecordingHardLimitMs,
  resolvePreferredAudioMimeType,
  stopMediaRecorderSafely
} from "../lib/browserMedia";
import {
  advanceInterview,
  cancelInterview,
  finalizeInterview,
  getInterview,
  getInterviewTranscriptionToken,
  mergeInterviewTranscriptStatus,
  preserveLiveInterviewTurn,
  prepareInterview,
  startInterview,
  submitInterviewTurnTranscript,
  uploadInterviewTurnAudio,
  type InterviewSession,
  type InterviewTurn,
} from "../lib/interviewSession";
import { CartesiaRealtimeTranscriber } from "../lib/cartesiaRealtime";
import { InterviewTurnCapture, type CapturedInterviewTurn } from "../lib/interviewTurnCapture";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import {
  advanceInterviewTimeline,
  commitInterviewAdvance,
  hasInterviewAnswerEvidence,
  MAX_LIVE_SEGMENT_ATTEMPTS,
  MIN_ANSWER_MS,
  resolveInterviewRecordingLimitSeconds,
  rotateFailedInterviewSegment,
} from "../lib/interviewFlow";
import { newIdempotencyKey } from "../lib/mediaUpload";
import { formatTime, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector, useAppStore } from "../store/hooks";
import {
  backToQuestionsList,
  clearPhotoForPractice,
  clearQuestionsError,
  clearStudyError,
  fetchDailyQuestions,
  fetchStudyWords,
  MAX_AUTHENTICATED_RECORDING_SECONDS,
  PHOTO_PRACTICE_MAX_BYTES,
  reRecord,
  type RecordingSaveDraft,
  selectTopic,
  setCustomTopicDraft,
  setRecordingAudioDataUrl,
  setRecordingInputError,
  setPhotoForPractice,
  setPhotoObjectDraft,
  setPhotoUploadError,
  startFreeTalk,
  startPhotoDescription,
  startRecording,
  stopRecording,
  tickRecording,
  toggleAddTopicInput,
  toggleWords,
  useCustomTopic as applyCustomTopic
} from "../store/slices/appSlice";
import GuidanceWordTicker from "./GuidanceWordTicker";
import InterviewQuestionCard from "./InterviewQuestionCard";

const PHOTO_ACCEPTED_TYPES = new Set(["image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif"]);
type FinalAudioUploadState = "idle" | "uploading" | "ready" | "failed";
type InterviewSyncOperation = {
  generation: number;
  sessionId: string;
  execute: () => Promise<InterviewSession>;
};
type InterviewSegmentOperation = {
  generation: number;
  sessionId: string;
  seq: number;
  blob: Blob | null;
  transcript: Promise<string> | null;
  transcriptSubmitted: boolean;
  key: string;
  attempts: number;
};

const snapshotInterviewTurns = (session: InterviewSession | null): SavedInterviewTurn[] =>
  session?.turns.map((turn) => {
    const answerText = turn.provisionalTranscript;
    return {
      sequence: turn.seq,
      question: turn.question,
      askedAtMs: turn.askedAtMs,
      endedAtMs: turn.endedAtMs,
      answerText,
      answerSource: answerText
        ? turn.transcriptStatus === "ready" ? "final" : "provisional"
        : "none",
      answerAlignment: null,
    };
  }) ?? [];

const waitForPromiseWithin = (operation: Promise<unknown>, timeoutMs: number): Promise<boolean> =>
  new Promise((resolve) => {
    let settled = false;
    const timer = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      resolve(false);
    }, Math.max(0, timeoutMs));
    operation.then(() => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      resolve(true);
    }, () => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      resolve(true);
    });
  });

const readFileAsDataUrl = (file: File): Promise<string> => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = typeof reader.result === "string" ? reader.result : "";
      if (!result) {
        reject(new Error("Failed to read image file."));
        return;
      }
      resolve(result);
    };
    reader.onerror = () => reject(new Error("Failed to read image file."));
    reader.readAsDataURL(file);
  });
};

type StudyTextSegment = {
  text: string;
  isStudyWord: boolean;
};

const escapeRegExp = (value: string): string => {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
};

const buildStudyTextSegments = (text: string, words: string[]): StudyTextSegment[] => {
  if (!text) {
    return [];
  }

  const uniqueWords = Array.from(
    new Set(
      words
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
    )
  );
  if (uniqueWords.length === 0) {
    return [{ text, isStudyWord: false }];
  }

  const pattern = uniqueWords
    .sort((a, b) => b.length - a.length)
    .map((item) => escapeRegExp(item))
    .join("|");
  if (!pattern) {
    return [{ text, isStudyWord: false }];
  }

  const regex = new RegExp(`\\b(?:${pattern})\\b`, "gi");
  const segments: StudyTextSegment[] = [];
  let cursor = 0;
  let match = regex.exec(text);

  while (match) {
    const start = match.index;
    const end = start + match[0].length;

    if (start > cursor) {
      segments.push({
        text: text.slice(cursor, start),
        isStudyWord: false
      });
    }

    segments.push({
      text: text.slice(start, end),
      isStudyWord: true
    });

    cursor = end;
    if (regex.lastIndex === start) {
      regex.lastIndex += 1;
    }
    match = regex.exec(text);
  }

  if (cursor < text.length) {
    segments.push({
      text: text.slice(cursor),
      isStudyWord: false
    });
  }

  return segments.length > 0 ? segments : [{ text, isStudyWord: false }];
};

export default function SpeakScreen() {
  const dispatch = useAppDispatch();
  const store = useAppStore();
  const router = useRouter();
  const [recordingStarting, setRecordingStarting] = useState(false);
  const [guestSaveStatus, setGuestSaveStatus] = useState<FinalAudioUploadState>("idle");
  const [guestSaveError, setGuestSaveError] = useState<string | null>(null);
  const [interview, setInterview] = useState<InterviewSession | null>(null);
  const [interviewPreparationError, setInterviewPreparationError] = useState<string | null>(null);
  const [interviewSyncError, setInterviewSyncError] = useState<string | null>(null);
  const [interviewSaveStatus, setInterviewSaveStatus] = useState<FinalAudioUploadState>("idle");
  const [interviewSaveError, setInterviewSaveError] = useState<string | null>(null);
  const [liveTranscriptionAvailable, setLiveTranscriptionAvailable] = useState(true);
  const [interviewLiveWarning, setInterviewLiveWarning] = useState<string | null>(null);
  const [interviewCaptureFailure, setInterviewCaptureFailure] = useState<string | null>(null);
  const [interviewStopNotice, setInterviewStopNotice] = useState<string | null>(null);
  const [interviewAnswerWarning, setInterviewAnswerWarning] = useState<string | null>(null);
  const [currentAnswerHasSpeech, setCurrentAnswerHasSpeech] = useState(false);
  const [interviewBoundaryPending, setInterviewBoundaryPending] = useState(false);
  const [interviewRefreshToken, setInterviewRefreshToken] = useState(0);
  const {
    speakState,
    selectedTopic,
    showWords,
    recordingDuration,
    topics,
    showAddTopicInput,
    customTopicDraft,
    isAuthenticated,
    questionsStatus,
    questionsError,
    selectedInterestIds,
    selectedEnglishLevel,
    studyWords,
    studyText,
    studyStatus,
    studyError,
    recordingSaveStatus,
    recordingSaveError,
    recordingPracticeType,
    pendingRecordingAudioDataUrl,
    recordingInputError,
    pendingPhotoDataUrl,
    pendingPhotoObjectDraft,
    pendingPhotoError
  } = useAppSelector((state) => state.app);

  const sessionLimitSeconds = MAX_AUTHENTICATED_RECORDING_SECONDS;

  const quotaHint = isAuthenticated
    ? `Account recording limit: ${formatTime(MAX_AUTHENTICATED_RECORDING_SECONDS)} per recording.`
    : null;
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);
  const recordingStartingRef = useRef(false);
  const mountedRef = useRef(false);
  const recordingAttemptRef = useRef(0);
  const interviewRef = useRef<InterviewSession | null>(null);
  const interviewGenerationRef = useRef(0);
  const interviewPreparationKeyRef = useRef<string | null>(null);
  const interviewPreparationGenerationRef = useRef(0);
  const interviewCreateKeyRef = useRef<string | null>(null);
  const interviewCreateInputRef = useRef<string | null>(null);
  const guestIdentityPreparedKeyRef = useRef<string | null>(null);
  const pendingInterviewPreparationRef = useRef<Promise<InterviewSession> | null>(null);
  const pendingInterviewCancelRef = useRef<Promise<void> | null>(null);
  const interviewCancelSessionIdRef = useRef<string | null>(null);
  const usedInterviewCandidateIdsRef = useRef(new Set<string>());
  const interviewSyncQueueRef = useRef<InterviewSyncOperation[]>([]);
  const interviewSyncRunningRef = useRef(false);
  const interviewSegmentQueueRef = useRef<InterviewSegmentOperation[]>([]);
  const interviewSegmentRunPromiseRef = useRef<Promise<void> | null>(null);
  const runInterviewSegmentsRef = useRef<(() => Promise<void>) | null>(null);
  const turnCaptureRef = useRef<InterviewTurnCapture | null>(null);
  const interviewRealtimeRef = useRef<CartesiaRealtimeTranscriber | null>(null);
  const turnCaptureStopRef = useRef<Promise<void> | null>(null);
  const recordingStartedAtRef = useRef<number | null>(null);
  const recordingEndedAtMsRef = useRef<number | null>(null);
  const interviewStartedAtRef = useRef<number | null>(null);
  const interviewEndedAtMsRef = useRef<number | null>(null);
  const recordingLimitTimerRef = useRef<number | null>(null);
  const recordingLimitMsRef = useRef<number | null>(null);
  const savedInterviewPreviewIdRef = useRef<string | null>(null);
  const interviewSaveDraftRef = useRef<RecordingSaveDraft | null>(null);
  const interviewSavingRef = useRef(false);
  const interviewSaveProtectedSessionIdRef = useRef<string | null>(null);
  const currentAnswerHasSpeechRef = useRef(false);
  const interviewBoundaryPendingRef = useRef(false);

  const updateInterview = useCallback((updater: (current: InterviewSession | null) => InterviewSession | null) => {
    const next = updater(interviewRef.current);
    interviewRef.current = next;
    setInterview(next);
  }, []);

  const updateCurrentAnswerSpeech = useCallback((active: boolean) => {
    currentAnswerHasSpeechRef.current = active;
    setCurrentAnswerHasSpeech(active);
    if (active) setInterviewAnswerWarning(null);
  }, []);

  const currentInterviewAnswerIsPresent = useCallback((session = interviewRef.current): boolean => {
    const current = session?.turns[session.turns.length - 1];
    return hasInterviewAnswerEvidence(
      current,
      currentAnswerHasSpeechRef.current || Boolean(turnCaptureRef.current?.hasSpeechActivity()),
    );
  }, []);

  const cancelCurrentInterview = useCallback((keepalive = false) => {
    const recovery = browserInterviewRecovery();
    const stored = recovery.read();
    const owned = stored?.ownerToken === recovery.ownerToken ? stored : null;
    const sessionId = interviewRef.current?.id ?? owned?.sessionId;
    if (!mayAbandonInterview(sessionId ?? null, interviewSavingRef.current,
      interviewSaveProtectedSessionIdRef.current, owned?.phase)) return;
    const preparing = pendingInterviewPreparationRef.current;
    if (!sessionId && !preparing) {
      if (owned) recovery.release(owned.principalId);
      return;
    }
    if (sessionId) interviewCreateKeyRef.current = null;
    const previousCancel = pendingInterviewCancelRef.current;
    const cancellation = (async () => {
      if (previousCancel) await previousCancel.catch(() => undefined);
      let id = sessionId;
      if (!id && preparing) {
        try { id = (await preparing).id; } catch {
          if (owned) recovery.release(owned.principalId);
          return;
        }
      }
      if (id) {
        interviewCancelSessionIdRef.current = id;
        await cancelInterview(id, keepalive);
        if (interviewCancelSessionIdRef.current === id) interviewCancelSessionIdRef.current = null;
        if (owned) {
          recovery.clear(owned.createKey);
          recovery.release(owned.principalId);
        }
      }
    })();
    pendingInterviewCancelRef.current = cancellation;
    // The old document cannot keep recording after pagehide. Let its replacement
    // reclaim the lease even if the keepalive cancellation has not completed yet.
    if (keepalive && owned) recovery.release(owned.principalId);
    void cancellation.then(() => {
      if (pendingInterviewCancelRef.current === cancellation) pendingInterviewCancelRef.current = null;
    }).catch(() => undefined);
  }, []);

  const mergeInterview = useCallback((server: InterviewSession, authoritativeCandidates = false) => {
    updateInterview((current) => {
      if (!current || current.id !== server.id) return server;
      const serverTurns = new Map(server.turns.map((turn) => [turn.seq, turn]));
      const turns = current.turns.length > server.turns.length
        ? current.turns.map((turn) => {
            const latest = serverTurns.get(turn.seq);
            return latest ? {
              ...preserveLiveInterviewTurn(latest, turn),
              askedAtMs: turn.askedAtMs,
              endedAtMs: turn.endedAtMs ?? latest.endedAtMs,
              transcriptStatus: mergeInterviewTranscriptStatus(latest.transcriptStatus, turn.transcriptStatus),
            } : turn;
          })
        : server.turns.map((turn) => {
            const local = current.turns.find((item) => item.seq === turn.seq);
            return local ? {
              ...preserveLiveInterviewTurn(turn, local),
              askedAtMs: local.askedAtMs,
              endedAtMs: local.endedAtMs ?? turn.endedAtMs,
              transcriptStatus: mergeInterviewTranscriptStatus(turn.transcriptStatus, local.transcriptStatus),
            } : turn;
          });
      const candidates = [...server.candidates, ...(authoritativeCandidates ? [] : current.candidates)]
        .filter((candidate) => !usedInterviewCandidateIdsRef.current.has(candidate.id))
        .filter((candidate, index, all) => all.findIndex((item) => item.id === candidate.id) === index);
      return {
        ...server,
        usefulWords: server.usefulWords.length ? server.usefulWords : current.usefulWords,
        turns,
        candidates,
        currentTurnSeq: turns.length ? turns[turns.length - 1].seq : server.currentTurnSeq,
      };
    });
  }, [updateInterview]);

  const runInterviewSync = useCallback(async () => {
    if (interviewSyncRunningRef.current) return;
    interviewSyncRunningRef.current = true;
    try {
      while (interviewSyncQueueRef.current.length) {
        const operation = interviewSyncQueueRef.current[0];
        const isCurrent = () => operation.generation === interviewGenerationRef.current
          && interviewRef.current?.id === operation.sessionId
          && interviewSyncQueueRef.current[0] === operation;
        if (!isCurrent()) {
          if (interviewSyncQueueRef.current[0] === operation) interviewSyncQueueRef.current.shift();
          continue;
        }
        try {
          const result = await operation.execute();
          if (!isCurrent()) continue;
          interviewSyncQueueRef.current.shift();
          mergeInterview(result);
          setInterviewSyncError(null);
        } catch {
          if (!isCurrent()) continue;
          setInterviewSyncError("Waiting for connection to sync the interview timeline.");
          break;
        }
      }
    } finally {
      interviewSyncRunningRef.current = false;
      if (!interviewSyncQueueRef.current.length && interviewSegmentQueueRef.current.length) {
        void runInterviewSegmentsRef.current?.();
      }
    }
  }, [mergeInterview]);

  const queueInterviewSync = useCallback((sessionId: string, execute: () => Promise<InterviewSession>) => {
    interviewSyncQueueRef.current.push({ generation: interviewGenerationRef.current, sessionId, execute });
    void runInterviewSync();
  }, [runInterviewSync]);

  const runInterviewSegments = useCallback((): Promise<void> => {
    if (interviewSegmentRunPromiseRef.current) return interviewSegmentRunPromiseRef.current;
    if (interviewSyncQueueRef.current.length) return Promise.resolve();
    let running: Promise<void>;
    running = (async () => {
      // Each queued answer gets at most one attempt per pass. A failed item is
      // rotated behind later answers so one bad WAV cannot starve the session.
      let remainingThisPass = interviewSegmentQueueRef.current.length;
      while (remainingThisPass > 0 && interviewSegmentQueueRef.current.length && !interviewSyncQueueRef.current.length) {
        remainingThisPass -= 1;
        const segment = interviewSegmentQueueRef.current[0];
        const isCurrent = () => segment.generation === interviewGenerationRef.current
          && interviewRef.current?.id === segment.sessionId
          && interviewSegmentQueueRef.current[0] === segment;
        if (!isCurrent()) {
          if (interviewSegmentQueueRef.current[0] === segment) interviewSegmentQueueRef.current.shift();
          continue;
        }
        try {
          if (!segment.transcriptSubmitted && segment.transcript) {
            try {
              const text = await segment.transcript;
              segment.transcript = null;
              if (text.trim()) {
                const session = await submitInterviewTurnTranscript(
                  segment.sessionId,
                  segment.seq,
                  text,
                  `${segment.key}:transcript`,
                );
                if (!isCurrent()) continue;
                segment.transcriptSubmitted = true;
                mergeInterview(session);
              }
            } catch {
              // The WAV below remains the durable batch fallback when the
              // realtime socket or transcript submission is unavailable.
              segment.transcript = null;
            }
          }
          // A successful realtime transcript is the complete per-turn result.
          // Keep only the uninterrupted recording for playback/history. Upload
          // this temporary WAV solely when the turn needs batch STT fallback.
          if (!segment.transcriptSubmitted && segment.blob) {
            await uploadInterviewTurnAudio(segment.sessionId, segment.seq, segment.blob, !isAuthenticated, segment.key);
          } else if (!segment.transcriptSubmitted) {
            throw new Error("No answer audio or realtime transcript is available.");
          }
          if (!isCurrent()) continue;
          interviewSegmentQueueRef.current.shift();
        } catch {
          if (!isCurrent()) continue;
          const rotated = rotateFailedInterviewSegment(
            interviewSegmentQueueRef.current,
            segment,
            MAX_LIVE_SEGMENT_ATTEMPTS,
          );
          if (rotated.dropped) {
            // Keep the durable WAV available for the next periodic/save retry.
            // A bounded pass still prevents one offline upload from starving
            // later answers, while reconnecting can recover without recording
            // the answer again.
            segment.attempts = 0;
            interviewSegmentQueueRef.current = [...rotated.queue, segment];
            setInterviewLiveWarning(
              "Some answers could not be transcribed. Keep this page open and retry saving after the connection recovers.",
            );
          } else {
            interviewSegmentQueueRef.current = rotated.queue;
          }
        }
      }
    })().finally(() => {
      if (interviewSegmentRunPromiseRef.current === running) interviewSegmentRunPromiseRef.current = null;
    });
    interviewSegmentRunPromiseRef.current = running;
    return running;
  }, [isAuthenticated, mergeInterview]);

  useEffect(() => {
    runInterviewSegmentsRef.current = runInterviewSegments;
    return () => {
      if (runInterviewSegmentsRef.current === runInterviewSegments) runInterviewSegmentsRef.current = null;
    };
  }, [runInterviewSegments]);

  const queueInterviewSegment = useCallback((sessionId: string, seq: number, captured: CapturedInterviewTurn, generation = interviewGenerationRef.current) => {
    const blob = captured.blob && captured.blob.size > 44 ? captured.blob : null;
    if ((!blob && !captured.transcript) || generation !== interviewGenerationRef.current || interviewRef.current?.id !== sessionId) return;
    interviewSegmentQueueRef.current.push({
      generation,
      sessionId,
      seq,
      blob,
      transcript: captured.transcript,
      transcriptSubmitted: false,
      key: newIdempotencyKey(`interview-answer-${seq}`),
      attempts: 0,
    });
    void runInterviewSegments();
  }, [runInterviewSegments]);

  const interviewElapsedMs = useCallback(() => Math.max(0, Math.floor(performance.now() - (interviewStartedAtRef.current ?? performance.now()))), []);

  const currentRecordingElapsedMs = useCallback(() =>
    recordingElapsedMs(recordingStartedAtRef.current, performance.now()), []);

  const releaseMedia = useCallback(() => {
    if (mediaStreamRef.current) {
      for (const track of mediaStreamRef.current.getTracks()) {
        track.stop();
      }
      mediaStreamRef.current = null;
    }
    mediaRecorderRef.current = null;
  }, []);

  const finishInterviewCapture = useCallback(() => {
    const session = interviewRef.current;
    if (!session || interviewStartedAtRef.current === null || interviewEndedAtMsRef.current !== null) return;
    const last = session.turns[session.turns.length - 1];
    const answerPresent = currentInterviewAnswerIsPresent(session);
    if (last && !answerPresent) {
      const message = "No spoken answer was detected for the current question. Re-record this interview before saving.";
      setInterviewAnswerWarning("Say an answer before finishing the interview.");
      setInterviewCaptureFailure(message);
      dispatch(setRecordingInputError(message));
    }
    const endedAtMs = Math.max(interviewElapsedMs(), last ? last.askedAtMs + 1 : 0);
    interviewEndedAtMsRef.current = endedAtMs;
    if (last) {
      updateInterview((current) => current ? {
        ...current,
        turns: current.turns.map((turn) => turn.seq === last.seq ? { ...turn, endedAtMs } : turn),
      } : current);
    }
    const capture = turnCaptureRef.current;
    const realtime = interviewRealtimeRef.current;
    turnCaptureRef.current = null;
    interviewRealtimeRef.current = null;
    if (capture && last) {
      const generation = interviewGenerationRef.current;
      turnCaptureStopRef.current = capture.stop(
        realtime ? () => realtime.finalizeTurn(last.seq) : undefined,
        () => {
          if (mountedRef.current) {
            setInterviewLiveWarning("Finishing the last answer is taking longer than expected…");
          }
        },
      )
        .then((captured) => {
          if (answerPresent) queueInterviewSegment(session.id, last.seq, captured, generation);
        })
        .catch((error: unknown) => {
          if (!mountedRef.current) return;
          const message = error instanceof Error
            ? error.message
            : "Answer capture could not finish.";
          const fatalMessage = `${message} Re-record this interview before saving.`;
          setLiveTranscriptionAvailable(false);
          setInterviewCaptureFailure((current) => current ?? fatalMessage);
          setInterviewLiveWarning(fatalMessage);
          dispatch(setRecordingInputError(fatalMessage));
        })
        .finally(() => {
          capture.setPCMListener(null);
          if (realtime) void realtime.close();
          interviewBoundaryPendingRef.current = false;
          if (mountedRef.current) {
            setInterviewBoundaryPending(false);
            setInterviewLiveWarning((current) => current === "Finishing the last answer is taking longer than expected…"
              ? null
              : current);
          }
        });
    } else if (realtime) {
      void realtime.close();
    }
    updateCurrentAnswerSpeech(false);
  }, [currentInterviewAnswerIsPresent, dispatch, interviewElapsedMs, queueInterviewSegment, updateCurrentAnswerSpeech, updateInterview]);

  const finishActiveRecording = useCallback((notice?: string) => {
    if (recordingStartedAtRef.current !== null && recordingEndedAtMsRef.current === null) {
      recordingEndedAtMsRef.current = Math.max(1, currentRecordingElapsedMs());
    }
    if (recordingLimitTimerRef.current !== null) window.clearTimeout(recordingLimitTimerRef.current);
    recordingLimitTimerRef.current = null;
    finishInterviewCapture();
    if (notice) setInterviewStopNotice(notice);
    dispatch(stopRecording());
    // Stop the encoder in the same task as the monotonic boundary. Waiting for
    // the Redux render/effect can add enough audio to cross a strict quota.
    stopMediaRecorderSafely(mediaRecorderRef.current, releaseMedia);
  }, [currentRecordingElapsedMs, dispatch, finishInterviewCapture, releaseMedia]);

  const armRecordingLimit = useCallback((limitSeconds: number) => {
    const hardLimitMs = resolveRecordingHardLimitMs(limitSeconds);
    recordingLimitMsRef.current = hardLimitMs;
    if (recordingLimitTimerRef.current !== null) window.clearTimeout(recordingLimitTimerRef.current);
    recordingLimitTimerRef.current = hardLimitMs > 0
      ? window.setTimeout(() => finishActiveRecording(), hardLimitMs)
      : null;
  }, [finishActiveRecording]);

  const waitForInterviewSegmentsBeforeSave = useCallback(async (timeoutMs = 4000): Promise<boolean> => {
    const deadline = performance.now() + timeoutMs;
    while (interviewSegmentQueueRef.current.length && !interviewSyncQueueRef.current.length) {
      const remaining = Math.max(0, deadline - performance.now());
      if (remaining <= 0) return false;
      const completed = await waitForPromiseWithin(runInterviewSegments(), remaining);
      if (!completed) return false;
      if (interviewSegmentQueueRef.current.length) {
        await new Promise<void>((resolve) => window.setTimeout(resolve, Math.min(500, Math.max(0, deadline - performance.now()))));
      }
    }
    return interviewSegmentQueueRef.current.length === 0;
  }, [runInterviewSegments]);

  const synchronizeInterviewBeforeSave = useCallback(async (): Promise<"ready" | "pending" | "failed"> => {
    const deadline = performance.now() + 15_000;
    if (turnCaptureStopRef.current) {
      const captureStopped = await waitForPromiseWithin(
        turnCaptureStopRef.current,
        Math.min(4000, Math.max(0, deadline - performance.now())),
      );
      if (!captureStopped) return "pending";
    }
    while (interviewSyncQueueRef.current.length && performance.now() < deadline) {
      const completed = await waitForPromiseWithin(
        runInterviewSync(),
        Math.min(1000, Math.max(0, deadline - performance.now())),
      );
      if (!interviewSyncQueueRef.current.length) break;
      if (completed) {
        await new Promise<void>((resolve) => window.setTimeout(
          resolve,
          Math.min(1000, Math.max(0, deadline - performance.now())),
        ));
      }
    }
    if (interviewSyncQueueRef.current.length) {
      return "pending";
    }
    const segmentsReady = await waitForInterviewSegmentsBeforeSave(
      Math.min(8000, Math.max(0, deadline - performance.now())),
    );
    if (!segmentsReady) return "pending";

    while (performance.now() < deadline) {
      const current = interviewRef.current;
      const endedTurns = current?.turns.filter((turn) => turn.endedAtMs !== null) ?? [];
      if (endedTurns.length > 0 && endedTurns.every((turn) => turn.transcriptStatus === "ready")) {
        return "ready";
      }
      if (endedTurns.some((turn) => turn.transcriptStatus === "failed") || !current?.id) {
        return "failed";
      }
      try {
        mergeInterview(await getInterview(current.id), true);
      } catch {
        // Keep polling within the save deadline; the user can retry without
        // re-recording if the connection remains unavailable.
      }
      const remaining = Math.max(0, deadline - performance.now());
      if (remaining > 0) {
        await new Promise<void>((resolve) => window.setTimeout(resolve, Math.min(500, remaining)));
      }
    }
    return "pending";
  }, [mergeInterview, runInterviewSync, waitForInterviewSegmentsBeforeSave]);

  const createRecordingFromMicrophone = useCallback(
    async (onRecordingStarted: () => void, captureInterview: boolean, attempt: number) => {
      const isCurrent = () => mountedRef.current && recordingAttemptRef.current === attempt;
      if (!isCurrent()) return;
      dispatch(setRecordingInputError(null));
      dispatch(setRecordingAudioDataUrl(null));

      const recordingSupportError = resolveBrowserRecordingSupportError();
      if (recordingSupportError) {
        dispatch(setRecordingInputError(recordingSupportError));
        return;
      }

      let stream: MediaStream | null = null;
      let recorder: MediaRecorder | null = null;
      let localCapture: InterviewTurnCapture | null = null;
      let localRealtime: CartesiaRealtimeTranscriber | null = null;
      const stopLocalStream = () => {
        if (stream) for (const track of stream.getTracks()) track.stop();
        if (mediaStreamRef.current === stream) mediaStreamRef.current = null;
        if (mediaRecorderRef.current === recorder) mediaRecorderRef.current = null;
      };
      try {
        const getUserMedia = navigator.mediaDevices?.getUserMedia?.bind(navigator.mediaDevices);
        if (!getUserMedia) {
          dispatch(setRecordingInputError("Your browser does not support microphone recording."));
          return;
        }

        stream = await getUserMedia({ audio: true });
        if (!isCurrent()) {
          stopLocalStream();
          return;
        }
        mediaStreamRef.current = stream;
        const mimeType = resolvePreferredAudioMimeType();
        recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);

        mediaRecorderRef.current = recorder;
        const chunks: Blob[] = [];

        if (captureInterview) {
          try {
            localCapture = await InterviewTurnCapture.start(
              stream,
              (error) => {
                if (!isCurrent()) return;
                if (turnCaptureRef.current === localCapture) turnCaptureRef.current = null;
                if (interviewRealtimeRef.current === localRealtime) interviewRealtimeRef.current = null;
                if (localRealtime) void localRealtime.close();
                const message = `${error.message} Re-record this interview so every answer can be analyzed.`;
                setLiveTranscriptionAvailable(false);
                setInterviewCaptureFailure(message);
                setInterviewLiveWarning(message);
                dispatch(setRecordingInputError(message));
                if (recordingStartedAtRef.current !== null && recordingEndedAtMsRef.current === null) {
                  finishActiveRecording("Answer capture failed. Re-record this interview before saving.");
                }
              },
              (active) => {
                if (isCurrent()) updateCurrentAnswerSpeech(active);
              },
            );
            if (!isCurrent()) {
              await localCapture.stop().catch(() => null);
              stopLocalStream();
              return;
            }
            turnCaptureRef.current = localCapture;
          } catch {
            if (!isCurrent()) {
              stopLocalStream();
              return;
            }
            turnCaptureRef.current = null;
            setLiveTranscriptionAvailable(false);
          }
        }

        if (!isCurrent()) {
          if (localCapture) await localCapture.stop().catch(() => null);
          stopLocalStream();
          return;
        }

        const activeRecorder = recorder;
        activeRecorder.ondataavailable = (event: BlobEvent) => {
          if (event.data && event.data.size > 0) {
            chunks.push(event.data);
          }
        };

        activeRecorder.onerror = () => {
          if (!isCurrent() || recordingEndedAtMsRef.current !== null) return;
          dispatch(setRecordingInputError("Recording stopped because the microphone failed. You can save the captured audio or try again."));
          finishActiveRecording("The microphone stopped unexpectedly. Only the captured part of the recording will be saved.");
        };

        activeRecorder.onstop = () => {
          const stoppedUnexpectedly = isCurrent() && recordingEndedAtMsRef.current === null;
          if (stoppedUnexpectedly) {
            finishActiveRecording("The microphone stopped unexpectedly. Only the captured part of the recording will be saved.");
          }
          const resultingType = activeRecorder.mimeType || "audio/webm";
          if (mediaRecorderRef.current === activeRecorder) releaseMedia();
          else stopLocalStream();
          if (!isCurrent()) return;

          if (chunks.length === 0) {
            dispatch(setRecordingInputError("No audio captured. Try recording again."));
            return;
          }

          const blob = new Blob(chunks, { type: resultingType });
          void readBlobAsDataUrl(blob)
            .then((dataUrl) => {
              if (isCurrent()) dispatch(setRecordingAudioDataUrl(dataUrl));
            })
            .catch(() => {
              if (isCurrent()) dispatch(setRecordingInputError("Failed to process recorded audio."));
            });
        };

        if (captureInterview && localCapture) {
          try {
            const session = interviewRef.current;
            if (!session) throw new Error("The interview session is unavailable.");
            localRealtime = await CartesiaRealtimeTranscriber.connect(
              await getInterviewTranscriptionToken(session.id),
              undefined,
              () => {
                if (!mountedRef.current || interviewRef.current?.id !== session.id) return;
                if (interviewRealtimeRef.current === localRealtime) {
                  interviewRealtimeRef.current = null;
                  turnCaptureRef.current?.setPCMListener(null);
                }
                setLiveTranscriptionAvailable(false);
                setInterviewLiveWarning(
                  "Realtime transcription disconnected. Completed answer audio will use the background fallback.",
                );
              },
              ({ turnSeq, finalText, interimText }) => {
                if (!isCurrent() || interviewRef.current?.id !== session.id) return;
                updateInterview((current) => current?.id === session.id ? {
                  ...current,
                  turns: current.turns.map((turn) => turn.seq === turnSeq ? {
                    ...turn,
                    liveTranscriptFinal: finalText,
                    liveTranscriptInterim: interimText,
                  } : turn),
                } : current);
              },
            );
            if (!isCurrent()) {
              await localRealtime.close();
              await localCapture.stop().catch(() => null);
              stopLocalStream();
              return;
            }
          } catch {
            localRealtime = null;
            setInterviewLiveWarning(
              "Realtime transcription is unavailable. Completed answer audio will use the background fallback.",
            );
            setLiveTranscriptionAvailable(false);
          }
        }
        if (captureInterview && localCapture) {
          try {
            // The worklet collects while the token and socket are prepared.
            // Discard that setup partition before attaching the PCM listener,
            // so Cartesia receives only audio from the actual interview.
            await localCapture.closeTurn();
          } catch {
            if (turnCaptureRef.current === localCapture) turnCaptureRef.current = null;
            await localCapture.stop().catch(() => null);
            localCapture = null;
            if (localRealtime) await localRealtime.close();
            localRealtime = null;
            if (isCurrent()) {
              setLiveTranscriptionAvailable(false);
              setInterviewLiveWarning("Answer capture could not start in this browser.");
            }
          }
        }
        if (captureInterview && !localCapture) {
          stopLocalStream();
          if (isCurrent()) {
            dispatch(setRecordingInputError(
              "This browser could not capture answer audio for transcription. Reload the page or use a current Chrome or Edge browser.",
            ));
          }
          return;
        }
        if (localCapture && localRealtime) {
          interviewRealtimeRef.current = localRealtime;
          localRealtime.beginTurn(1);
          localCapture.setPCMListener((pcm) => localRealtime?.sendPCM(pcm));
          setLiveTranscriptionAvailable(true);
        }
        if (!isCurrent()) {
          if (localRealtime) await localRealtime.close();
          if (localCapture) await localCapture.stop().catch(() => null);
          stopLocalStream();
          return;
        }

        activeRecorder.start();
        const startedAt = performance.now();
        recordingStartedAtRef.current = startedAt;
        recordingEndedAtMsRef.current = null;
        if (captureInterview) {
          interviewStartedAtRef.current = startedAt;
        }
        for (const track of stream.getAudioTracks()) {
          track.addEventListener("ended", () => {
            if (!isCurrent() || recordingEndedAtMsRef.current !== null) return;
            dispatch(setRecordingInputError("The microphone became unavailable. You can save the captured audio or try again."));
            finishActiveRecording("The microphone became unavailable. Only the captured part of the recording will be saved.");
          }, { once: true });
        }
        onRecordingStarted();
      } catch (error) {
        if (turnCaptureRef.current === localCapture) turnCaptureRef.current = null;
        if (interviewRealtimeRef.current === localRealtime) interviewRealtimeRef.current = null;
        if (localRealtime) void localRealtime.close();
        if (localCapture) void localCapture.stop();
        stopLocalStream();
        if (isCurrent()) dispatch(setRecordingInputError(resolveMicrophoneError(error)));
      }
    },
    [dispatch, finishActiveRecording, releaseMedia, updateCurrentAnswerSpeech, updateInterview]
  );

  const buildRecordingSaveDraft = useCallback((): RecordingSaveDraft | null => {
    const audioDataUrl = pendingRecordingAudioDataUrl?.trim() || null;
    if (!audioDataUrl) {
      return null;
    }
    if (recordingPracticeType === "topic" && interviewRef.current?.id
      && interviewSaveDraftRef.current?.interviewSessionId === interviewRef.current.id
      && interviewSaveDraftRef.current.audioDataUrl === audioDataUrl) {
      return interviewSaveDraftRef.current;
    }

    const normalizedPhotoObject = pendingPhotoObjectDraft
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, 120);
    const photoObject = normalizedPhotoObject || null;
    const topic =
      recordingPracticeType === "photo_description"
        ? photoObject
          ? `Photo description: ${photoObject}`
          : "Photo description"
        : selectedTopic ?? "Free talk";
    const timestamp = new Date().toISOString();

    const draft: RecordingSaveDraft = {
      localRecordingId: `local-${Date.now()}`,
      topic,
      duration: Math.max(1, Math.ceil((recordingEndedAtMsRef.current ?? recordingDuration * 1000) / 1000)),
      timestamp,
      practiceType: recordingPracticeType,
      audioDataUrl,
      photoDataUrl: recordingPracticeType === "photo_description" ? pendingPhotoDataUrl : null,
      photoObject,
      ...(recordingPracticeType === "topic" && interviewRef.current?.id ? {
        interviewSessionId: interviewRef.current.id,
        interviewEndedAtMs: interviewEndedAtMsRef.current ?? Math.max(0, Math.floor(recordingDuration * 1000)),
        interviewTurns: snapshotInterviewTurns(interviewRef.current),
      } : {}),
    };
    if (draft.interviewSessionId) interviewSaveDraftRef.current = draft;
    return draft;
  }, [
    pendingPhotoDataUrl,
    pendingPhotoObjectDraft,
    pendingRecordingAudioDataUrl,
    recordingDuration,
    recordingPracticeType,
    selectedTopic
  ]);

  const onSaveRecording = useCallback(() => {
    if (interviewSavingRef.current) return;
    if (recordingPracticeType === "topic" && interviewCaptureFailure) {
      dispatch(setRecordingInputError(interviewCaptureFailure));
      setInterviewSaveError("Re-record this interview before saving because one answer could not be captured.");
      return;
    }
    if (recordingPracticeType === "topic") finishInterviewCapture();
    const draft = buildRecordingSaveDraft();
    if (!draft?.localRecordingId) {
      dispatch(setRecordingInputError("Preparing audio, please wait a moment before saving."));
      return;
    }

    if (draft.practiceType === "topic" && draft.interviewSessionId) {
      const interviewSessionId = draft.interviewSessionId;
      const localRecordingId = draft.localRecordingId;
      const recovery = browserInterviewRecovery();
      const stored = recovery.read();
      const owned = stored?.ownerToken === recovery.ownerToken && stored.sessionId === interviewSessionId ? stored : null;
      interviewSavingRef.current = true;
      if (owned) recovery.update(owned.createKey, { phase: "saving" });
      const saveLeaseTimer = window.setInterval(() => {
        if (owned) recovery.renew(owned.principalId);
      }, 2000);
      setInterviewSaveError(null);
      setInterviewSaveStatus("uploading");
      void (async () => {
        const transcriptState = await synchronizeInterviewBeforeSave();
        if (transcriptState !== "ready") {
          const terminalMessage = "One answer could not be transcribed. Re-record the interview so every answer can be analyzed.";
          if (transcriptState === "failed") {
            setInterviewCaptureFailure(terminalMessage);
            dispatch(setRecordingInputError(terminalMessage));
          }
          setInterviewSaveStatus("failed");
          setInterviewSaveError(
            transcriptState === "failed"
              ? terminalMessage
              : "Your answers are still being transcribed. Keep this tab open for a moment, then retry saving.",
          );
          if (owned) recovery.update(owned.createKey, { phase: "active" });
          return;
        }

        const readyDraft: RecordingSaveDraft = {
          ...draft,
          interviewTurns: snapshotInterviewTurns(interviewRef.current),
        };
        interviewSaveDraftRef.current = readyDraft;

        if (isAuthenticated) {
          interviewSaveProtectedSessionIdRef.current = interviewSessionId;
          await saveAndNavigate(store, router, readyDraft, () => window.location.pathname);
          const saved = localRecordingId ? store.getState().app.recordingSaveResults[localRecordingId] : null;
          if (owned) {
            if (typeof saved === "string") recovery.clear(owned.createKey);
            else recovery.update(owned.createKey, { phase: "abandoned" });
            recovery.release(owned.principalId);
          }
          setInterviewSaveStatus("idle");
          return;
        }
        let previewId = savedInterviewPreviewIdRef.current;
        interviewSaveProtectedSessionIdRef.current = interviewSessionId;
        if (!previewId) {
          const preview = await createGuestPreview({
            topic: readyDraft.topic,
            duration: readyDraft.duration,
            timestamp: readyDraft.timestamp,
            practiceType: readyDraft.practiceType,
            audioDataUrl: readyDraft.audioDataUrl ?? "",
            interviewSessionId,
          });
          previewId = preview.id;
          savedInterviewPreviewIdRef.current = previewId;
        }
        await finalizeInterview(
          interviewSessionId,
          Math.max(0, Math.floor(readyDraft.interviewEndedAtMs ?? readyDraft.duration * 1000)),
          { guestPreviewId: previewId },
          `interview:${interviewSessionId}:finalize:${previewId}`,
        );
        if (owned) {
          recovery.clear(owned.createKey);
          recovery.release(owned.principalId);
        }
        setInterviewSaveStatus("ready");
        router.push(guestPreviewPath(previewId));
      })().catch((error: unknown) => {
        if (!isAuthenticated && !savedInterviewPreviewIdRef.current) {
          interviewSaveProtectedSessionIdRef.current = null;
        }
        if (owned && recovery.read()?.createKey === owned.createKey) {
          recovery.update(owned.createKey, {
            phase: savedInterviewPreviewIdRef.current ? "saving" : "active",
          });
        }
        setInterviewSaveStatus("failed");
        setInterviewSaveError(error instanceof Error ? error.message : "Could not save the interview. Please retry.");
      }).finally(() => {
        interviewSavingRef.current = false;
        window.clearInterval(saveLeaseTimer);
      });
      return;
    }

    if (!isAuthenticated) {
      if (draft.practiceType === "photo_description") {
        startGuestSave(store, router);
        return;
      }
      setGuestSaveError(null);
      setGuestSaveStatus("uploading");
      void createGuestPreview({
        topic: draft.topic,
        duration: draft.duration,
        timestamp: draft.timestamp,
        practiceType: draft.practiceType,
        audioDataUrl: draft.audioDataUrl ?? "",
      })
        .then((preview) => {
          setGuestSaveStatus("ready");
          router.push(guestPreviewPath(preview.id));
        })
        .catch((error: unknown) => {
          setGuestSaveStatus("failed");
          setGuestSaveError(error instanceof Error ? error.message : "Cannot create the guest preview. Please try again.");
        });
      return;
    }

    void saveAndNavigate(store, router, draft, () => window.location.pathname);
  }, [buildRecordingSaveDraft, dispatch, finishInterviewCapture, interviewCaptureFailure, isAuthenticated, recordingPracticeType, router, store, synchronizeInterviewBeforeSave]);

  const beginRecordingFromMicrophone = (onRecordingStarted: () => void, captureInterview = false) => {
    if (recordingStartingRef.current) {
      return;
    }
    if (!isAuthenticated && !captureInterview && recordingPracticeType !== "photo_description"
      && !startNewGuestPreviewSession()) {
      dispatch(setRecordingInputError("Guest access includes one preview. Sign in to record another sample."));
      return;
    }
    if (recordingLimitTimerRef.current !== null) window.clearTimeout(recordingLimitTimerRef.current);
    recordingLimitTimerRef.current = null;
    recordingLimitMsRef.current = null;
    recordingStartedAtRef.current = null;
    recordingEndedAtMsRef.current = null;
    setInterviewStopNotice(null);
    if (captureInterview) {
      setInterviewCaptureFailure(null);
      setInterviewAnswerWarning(null);
      interviewBoundaryPendingRef.current = false;
      setInterviewBoundaryPending(false);
      updateCurrentAnswerSpeech(false);
    }
    recordingStartingRef.current = true;
    setRecordingStarting(true);
    const attempt = ++recordingAttemptRef.current;
    void createRecordingFromMicrophone(onRecordingStarted, captureInterview, attempt).finally(() => {
      if (mountedRef.current && recordingAttemptRef.current === attempt) {
        recordingStartingRef.current = false;
        setRecordingStarting(false);
      }
    });
  };

  const onStartFreeTalk = () => {
    beginRecordingFromMicrophone(() => {
      dispatch(startFreeTalk());
      armRecordingLimit(isAuthenticated ? sessionLimitSeconds : MAX_GUEST_PREVIEW_SECONDS);
    });
  };

  const onStartTopicRecording = () => {
    const session = interviewRef.current;
    if (recordingPracticeType === "topic" && (!session || session.status !== "ready")) return;
    beginRecordingFromMicrophone(() => {
      dispatch(startRecording());
      const localLimitSeconds = isAuthenticated ? sessionLimitSeconds : MAX_GUEST_PREVIEW_SECONDS;
      const limitSeconds = recordingPracticeType === "topic"
        ? resolveInterviewRecordingLimitSeconds({
            isAuthenticated,
            authenticatedLimitSeconds: MAX_AUTHENTICATED_RECORDING_SECONDS,
            guestLimitSeconds: MAX_GUEST_PREVIEW_SECONDS,
            interviewLimitSeconds: session?.maxDurationSeconds ?? null,
          })
        : localLimitSeconds;
      armRecordingLimit(limitSeconds);
      if (!session || recordingPracticeType !== "topic") return;
      const opening: InterviewTurn = {
        seq: 1,
        question: session.openingQuestion,
        askedAtMs: 0,
        endedAtMs: null,
        provisionalTranscript: "",
      };
      updateInterview((current) => current ? { ...current, status: "recording", turns: [opening], currentTurnSeq: 1 } : current);
      queueInterviewSync(session.id, () => startInterview(session.id, `interview:${session.id}:start`));
    }, recordingPracticeType === "topic");
  };

  const onStopRecording = () => {
    if (recordingPracticeType === "topic" && !currentInterviewAnswerIsPresent()) {
      const message = "No spoken answer was detected for the current question. Re-record this interview before saving.";
      setInterviewAnswerWarning("No answer was detected before Stop. Re-record this interview to continue.");
      setInterviewCaptureFailure(message);
      dispatch(setRecordingInputError(message));
    }
    finishActiveRecording();
  };

  const onNextInterviewQuestion = () => {
    const session = interviewRef.current;
    if (!session || interviewEndedAtMsRef.current !== null || interviewBoundaryPendingRef.current) return;
    if (!currentInterviewAnswerIsPresent(session)) {
      setInterviewAnswerWarning("Say an answer before moving to the next question.");
      return;
    }
    const capture = turnCaptureRef.current;
    if (!capture) {
      const message = "Answer capture is unavailable. Re-record this interview before saving.";
      setInterviewCaptureFailure(message);
      dispatch(setRecordingInputError(message));
      finishActiveRecording(message);
      return;
    }
    const elapsedMs = interviewElapsedMs();
    const maxAtMs = recordingLimitMsRef.current ?? Number.POSITIVE_INFINITY;
    if (elapsedMs >= maxAtMs) {
      onStopRecording();
      return;
    }
    const advance = advanceInterviewTimeline(session, usedInterviewCandidateIdsRef.current, elapsedMs, maxAtMs);
    if (!advance) return;
    const { candidate, previousTurn } = advance;
    interviewBoundaryPendingRef.current = true;
    setInterviewBoundaryPending(true);
    setInterviewAnswerWarning(null);
    const realtime = interviewRealtimeRef.current;
    const nextTurnSeq = advance.session.turns[advance.session.turns.length - 1].seq;
    const generation = interviewGenerationRef.current;
    void capture.closeTurn(realtime ? () => {
      const transcript = realtime.finalizeTurn(previousTurn.seq);
      realtime.beginTurn(nextTurnSeq);
      return transcript;
    } : undefined, () => {
      if (mountedRef.current) {
        setInterviewLiveWarning("Finishing this answer is taking longer than expected…");
      }
    }).then((captured) => {
      if (generation !== interviewGenerationRef.current || interviewRef.current?.id !== session.id) return;
      let committed = false;
      updateInterview((current) => {
        if (!current) return current;
        const next = commitInterviewAdvance(current, advance);
        if (!next) return current;
        committed = true;
        return next;
      });
      if (!committed) {
        const fatalMessage = "The answer boundary could not be applied safely. Re-record this interview before saving.";
        setInterviewCaptureFailure(fatalMessage);
        setInterviewLiveWarning(fatalMessage);
        dispatch(setRecordingInputError(fatalMessage));
        finishActiveRecording(fatalMessage);
        return;
      }
      usedInterviewCandidateIdsRef.current.add(candidate.id);
      const key = newIdempotencyKey(`interview-advance-${previousTurn.seq}`);
      queueInterviewSync(session.id, async () => {
        const result = await advanceInterview(
          session.id,
          previousTurn.seq,
          candidate.id,
          advance.session.turns[advance.session.turns.length - 1].askedAtMs,
          key,
        );
        if (generation === interviewGenerationRef.current) {
          queueInterviewSegment(session.id, previousTurn.seq, captured, generation);
        }
        return result;
      });
    }).catch((error: unknown) => {
      if (mountedRef.current) {
        const message = error instanceof Error
          ? error.message
          : "Answer capture could not finish.";
        const fatalMessage = `${message} Re-record this interview before saving.`;
        setLiveTranscriptionAvailable(false);
        setInterviewCaptureFailure((current) => current ?? fatalMessage);
        setInterviewLiveWarning(fatalMessage);
        dispatch(setRecordingInputError(fatalMessage));
      }
      return { blob: null, transcript: null };
    }).finally(() => {
      interviewBoundaryPendingRef.current = false;
      if (mountedRef.current) {
        setInterviewBoundaryPending(false);
        setInterviewLiveWarning((current) => current === "Finishing this answer is taking longer than expected…"
          ? null
          : current);
      }
    });
  };

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      cancelCurrentInterview(true);
      mountedRef.current = false;
      recordingAttemptRef.current += 1;
      interviewGenerationRef.current += 1;
      interviewPreparationGenerationRef.current += 1;
      interviewSyncQueueRef.current = [];
      interviewSegmentQueueRef.current = [];
      const realtime = interviewRealtimeRef.current;
      interviewRealtimeRef.current = null;
      if (realtime) void realtime.close();
    };
  }, [cancelCurrentInterview]);

  useEffect(() => {
    const recovery = browserInterviewRecovery();
    const onPageHide = () => cancelCurrentInterview(true);
    const renew = () => {
      const stored = recovery.read();
      if (stored?.ownerToken === recovery.ownerToken && stored.phase !== "abandoned") {
        recovery.renew(stored.principalId);
      }
    };
    const timer = window.setInterval(renew, 2000);
    window.addEventListener("pagehide", onPageHide);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("pagehide", onPageHide);
    };
  }, [cancelCurrentInterview]);

  useEffect(() => {
    if (speakState !== "recording") {
      return;
    }

    const timer = window.setInterval(() => {
      dispatch(tickRecording());
    }, 1000);

    return () => window.clearInterval(timer);
  }, [dispatch, speakState]);

  useEffect(() => {
    if (speakState !== "recording") return;
    const checkDuration = () => {
      if (recordingEndedAtMsRef.current !== null) return;
      const hidden = document.visibilityState === "hidden";
      if (!recordingMustStop({
        startedAt: recordingStartedAtRef.current,
        hardLimitMs: recordingLimitMsRef.current,
        now: performance.now(),
        hidden,
      })) return;
      finishActiveRecording(hidden
        ? "Recording stopped when this tab was hidden so the audio stays within the session limit."
        : undefined);
    };
    document.addEventListener("visibilitychange", checkDuration);
    window.addEventListener("focus", checkDuration);
    const timer = window.setInterval(checkDuration, 250);
    return () => {
      document.removeEventListener("visibilitychange", checkDuration);
      window.removeEventListener("focus", checkDuration);
      window.clearInterval(timer);
    };
  }, [finishActiveRecording, speakState]);

  useEffect(() => {
    const dateKey = toDateKey(new Date());
    void dispatch(fetchDailyQuestions({ dateKey, interestIds: selectedInterestIds, englishLevel: selectedEnglishLevel }));
  }, [dispatch, selectedEnglishLevel, selectedInterestIds]);

  useEffect(() => {
    if (speakState === "idle") {
      if (interviewRef.current?.id && !interviewSavingRef.current) cancelCurrentInterview();
      interviewPreparationKeyRef.current = null;
      interviewPreparationGenerationRef.current += 1;
      interviewGenerationRef.current += 1;
      interviewSyncQueueRef.current = [];
      interviewSegmentQueueRef.current = [];
      interviewBoundaryPendingRef.current = false;
      setInterviewBoundaryPending(false);
      setInterviewAnswerWarning(null);
      updateCurrentAnswerSpeech(false);
      updateInterview(() => null);
      return;
    }
    if (speakState !== "readyToRecord" || recordingPracticeType !== "topic" || !selectedTopic) return;
    const key = [selectedTopic, selectedEnglishLevel, [...selectedInterestIds].sort().join("|"), isAuthenticated, interviewRefreshToken].join("::");
    if (interviewPreparationKeyRef.current === key) return;
    const createInput = [selectedTopic, selectedEnglishLevel, [...selectedInterestIds].sort().join("|"), isAuthenticated].join("::");
    if (interviewCreateInputRef.current !== createInput) {
      interviewCreateInputRef.current = createInput;
      interviewCreateKeyRef.current = null;
    }
    if (interviewRef.current?.id || pendingInterviewPreparationRef.current) cancelCurrentInterview();
    interviewPreparationKeyRef.current = key;
    const generation = ++interviewPreparationGenerationRef.current;
    interviewGenerationRef.current += 1;
    usedInterviewCandidateIdsRef.current.clear();
    interviewSyncQueueRef.current = [];
    interviewSegmentQueueRef.current = [];
    interviewEndedAtMsRef.current = null;
    interviewStartedAtRef.current = null;
    recordingLimitMsRef.current = null;
    savedInterviewPreviewIdRef.current = null;
    interviewSaveDraftRef.current = null;
    interviewSaveProtectedSessionIdRef.current = null;
    setInterviewPreparationError(null);
    setInterviewSyncError(null);
    setInterviewSaveError(null);
    setInterviewSaveStatus("idle");
    setInterviewLiveWarning(null);
    setInterviewCaptureFailure(null);
    setInterviewStopNotice(null);
    setInterviewAnswerWarning(null);
    interviewBoundaryPendingRef.current = false;
    setInterviewBoundaryPending(false);
    updateCurrentAnswerSpeech(false);
    updateInterview(() => null);
    let preparation: Promise<InterviewSession> | null = null;
    const recovery = browserInterviewRecovery();
    const isCurrentPreparation = () => mountedRef.current && interviewPreparationGenerationRef.current === generation;
    void (async () => {
      if (pendingInterviewCancelRef.current) {
        try {
          await pendingInterviewCancelRef.current;
        } catch {
          const id = interviewCancelSessionIdRef.current;
          if (!id) throw new Error("Could not close the previous interview. Retry preparation.");
          await cancelInterview(id);
          if (interviewCancelSessionIdRef.current === id) interviewCancelSessionIdRef.current = null;
          const stored = recovery.read();
          if (stored?.sessionId === id) {
            recovery.clear(stored.createKey);
            recovery.release(stored.principalId);
          }
          pendingInterviewCancelRef.current = null;
        }
      }
      if (!isCurrentPreparation()) return;
      const previous = recovery.read();
      let identity = browserIdentity() ?? await restoreBrowserIdentity().catch(() => null);
      if (!isCurrentPreparation()) return;
      await recoverPreviousInterview(recovery, identity, {
        rehydrate: (stored) => prepareInterview({
          ...stored.input,
          guest: stored.kind === "guest",
          idempotencyKey: stored.createKey,
        }),
        get: getInterview,
        cancel: cancelInterview,
      }, isCurrentPreparation);
      if (!isCurrentPreparation()) return;
      if (previous && !recovery.read()) {
        interviewCreateKeyRef.current = null;
        guestIdentityPreparedKeyRef.current = null;
      }
      const createKey = interviewCreateKeyRef.current ?? newIdempotencyKey("interview-prepare");
      interviewCreateKeyRef.current = createKey;
      if (!isAuthenticated) {
        if (identity?.kind === "guest" && recovery.heldByOther(identity.principalId)) {
          throw new Error("An interview is open in another tab. Finish it there before starting a guest interview.");
        }
        if (guestIdentityPreparedKeyRef.current !== createKey && !startNewGuestPreviewSession()) {
          throw new Error("Guest access includes one preview. Sign in to record another sample.");
        }
        guestIdentityPreparedKeyRef.current = createKey;
        await ensureGuestPreviewIdentity();
        identity = browserIdentity();
      }
      if (!isCurrentPreparation()) return;
      if (!identity || identity.kind !== (isAuthenticated ? "user" : "guest")) {
        throw new Error("Your session changed. Reload the page and retry the interview.");
      }
      if (!recovery.claim(identity.principalId)) {
        throw new Error("An interview is open in another tab. Finish it there, or retry after closing that tab.");
      }
      try {
        recovery.write({
          principalId: identity.principalId,
          kind: identity.kind,
          createKey,
          input: { topic: selectedTopic, level: selectedEnglishLevel, interestIds: [...selectedInterestIds] },
          sessionId: null,
          phase: "preparing",
        });
      } catch (error) {
        recovery.release(identity.principalId);
        throw error;
      }
      preparation = prepareInterview({
        topic: selectedTopic,
        level: selectedEnglishLevel,
        interestIds: selectedInterestIds,
        guest: !isAuthenticated,
        idempotencyKey: createKey,
      });
      pendingInterviewPreparationRef.current = preparation;
      const session = await preparation;
      if (pendingInterviewPreparationRef.current === preparation) pendingInterviewPreparationRef.current = null;
      recovery.update(createKey, { sessionId: session.id, phase: "active" });
      if (!isCurrentPreparation()) {
        const cancelled = await cancelInterview(session.id).then(() => true).catch(() => false);
        if (cancelled) {
          recovery.clear(createKey);
          if (!recovery.read()) recovery.release(identity.principalId);
        }
        return;
      }
      mergeInterview(session, true);
    })().catch((error: unknown) => {
      if (mountedRef.current && interviewPreparationGenerationRef.current === generation) {
        setInterviewPreparationError(error instanceof Error ? error.message : "Could not prepare this interview.");
      }
      if (pendingInterviewPreparationRef.current === preparation) pendingInterviewPreparationRef.current = null;
    });
  }, [cancelCurrentInterview, interviewRefreshToken, isAuthenticated, mergeInterview, recordingPracticeType, selectedEnglishLevel, selectedInterestIds, selectedTopic, speakState, updateCurrentAnswerSpeech, updateInterview]);

  useEffect(() => {
    if (!interview?.id || (speakState !== "readyToRecord" && speakState !== "recording" && speakState !== "recorded")) return;
    let active = true;
    const generation = interviewGenerationRef.current;
    let requestNumber = 0;
    let appliedNumber = 0;
    const refresh = () => {
      void runInterviewSync();
      void runInterviewSegments();
      const request = ++requestNumber;
      void getInterview(interview.id).then((session) => {
        if (active && mountedRef.current && generation === interviewGenerationRef.current
          && interviewRef.current?.id === session.id && request >= appliedNumber) {
          appliedNumber = request;
          mergeInterview(session, true);
        }
      }).catch(() => {
        // Local prepared questions remain available while the connection recovers.
      });
    };
    refresh();
    const timer = window.setInterval(refresh, 2500);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [interview?.id, mergeInterview, runInterviewSegments, runInterviewSync, speakState]);

  useEffect(() => {
    if (speakState === "recording") {
      return;
    }

    if (recordingStartedAtRef.current !== null && recordingEndedAtMsRef.current === null) {
      finishActiveRecording();
    } else {
      finishInterviewCapture();
    }
    if (recordingLimitTimerRef.current !== null) window.clearTimeout(recordingLimitTimerRef.current);
    recordingLimitTimerRef.current = null;
    stopMediaRecorderSafely(mediaRecorderRef.current, releaseMedia);
  }, [finishActiveRecording, finishInterviewCapture, releaseMedia, speakState]);

  useEffect(() => {
    return () => {
      finishInterviewCapture();
      if (recordingLimitTimerRef.current !== null) window.clearTimeout(recordingLimitTimerRef.current);
      recordingLimitTimerRef.current = null;
      stopMediaRecorderSafely(mediaRecorderRef.current, releaseMedia);
    };
  }, [finishInterviewCapture, releaseMedia]);

  const onRefreshQuestions = () => {
    const dateKey = toDateKey(new Date());
    dispatch(clearQuestionsError());
    void dispatch(
      fetchDailyQuestions({
        dateKey,
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds,
        avoidQuestions: topics,
        englishLevel: selectedEnglishLevel
      })
    );
  };

  const onRefreshTopicGuidance = () => {
    if (!selectedTopic || recordingPracticeType === "photo_description") {
      return;
    }
    setInterviewRefreshToken((value) => value + 1);
  };

  const onBackToQuestionsList = () => {
    cancelCurrentInterview();
    recordingAttemptRef.current += 1;
    recordingStartingRef.current = false;
    setRecordingStarting(false);
    dispatch(backToQuestionsList());
  };

  const onGenerateStudyWords = () => {
    dispatch(clearStudyError());
    void dispatch(
      fetchStudyWords({
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds,
        avoidWords: studyWords,
        englishLevel: selectedEnglishLevel
      })
    );
  };

  const onPhotoSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.currentTarget.value = "";

    if (!file) {
      return;
    }

    if (!PHOTO_ACCEPTED_TYPES.has(file.type.toLowerCase())) {
      dispatch(setPhotoUploadError("Supported formats: JPG, PNG, WEBP, GIF."));
      return;
    }

    if (file.size > PHOTO_PRACTICE_MAX_BYTES) {
      dispatch(setPhotoUploadError(`Photo must be under ${Math.floor(PHOTO_PRACTICE_MAX_BYTES / (1024 * 1024))}MB.`));
      return;
    }

    void readFileAsDataUrl(file)
      .then((dataUrl) => {
        dispatch(setPhotoForPractice(dataUrl));
      })
      .catch(() => {
        dispatch(setPhotoUploadError("Failed to read selected photo."));
      });
  };

  if (speakState === "idle") {
    const shouldShowQuestionsSkeleton = questionsStatus === "loading" && topics.length === 0;
    const shouldShowStudySkeleton = studyStatus === "loading" && studyWords.length === 0 && !studyText;
    const studyParagraphs = studyText
      ? studyText
          .split(/\n{2,}/)
          .map((item) => item.trim())
          .filter((item) => item.length > 0)
      : [];

    return (
      <section className="speak-screen">
        <div className="speak-card speak-hero-card">
          <div className="heading-sm">Daily practice</div>
          <h2 className="heading-xl speak-heading-tight">Start a new speaking session</h2>
          {quotaHint && <div className="notice">{quotaHint}</div>}
          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={onStartFreeTalk}
            disabled={recordingStarting}
          >
            {recordingStarting ? "Starting..." : "Start speaking"}
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Today&apos;s questions</div>
            <button
              className="btn btn-secondary btn-small"
              onClick={onRefreshQuestions}
              disabled={questionsStatus === "loading"}
            >
              {questionsStatus === "loading" ? "Generating..." : "↻ Regenerate"}
            </button>
          </div>

          {shouldShowQuestionsSkeleton ? (
            <div className="topics-grid topics-grid-skeleton" aria-hidden="true">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={`topic-skeleton-${index}`} className="topic-skeleton">
                  <div className="skeleton-line skeleton-line-wide" />
                  <div className="skeleton-line skeleton-line-medium" />
                </div>
              ))}
            </div>
          ) : topics.length === 0 && questionsStatus !== "loading" ? (
            <div className="empty-state speak-empty-state">No daily questions yet.</div>
          ) : (
            <div className="topics-grid">
              {topics.map((topic) => (
                <button key={topic} className="topic-btn" onClick={() => dispatch(selectTopic(topic))}>
                  {topic}
                </button>
              ))}
            </div>
          )}

          {questionsError && <div className="auth-error top-spaced">{questionsError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Photo description</div>
            {pendingPhotoDataUrl && (
              <button className="btn btn-secondary btn-small" onClick={() => dispatch(clearPhotoForPractice())}>
                Remove photo
              </button>
            )}
          </div>

          <div className="profile-value">Upload an image and practice describing what you see.</div>

          <label className="btn btn-secondary btn-small photo-upload-btn">
            Upload photo
            <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" onChange={onPhotoSelected} />
          </label>

          {pendingPhotoDataUrl ? (
            <img src={pendingPhotoDataUrl} alt="Selected for speaking practice" className="photo-practice-preview" />
          ) : (
            <div className="empty-state speak-empty-state">No photo selected yet.</div>
          )}

          <div className="photo-object-input">
            <input
              type="text"
              placeholder="Optional object name (example: red bicycle)"
              value={pendingPhotoObjectDraft}
              onChange={(event) => dispatch(setPhotoObjectDraft(event.target.value))}
            />
          </div>

          <button
            className="btn btn-primary"
            onClick={() => dispatch(startPhotoDescription())}
            disabled={!pendingPhotoDataUrl}
          >
            Start photo session
          </button>

          {pendingPhotoError && <div className="auth-error top-spaced">{pendingPhotoError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Words for study</div>
            <button className="btn btn-secondary btn-small" onClick={onGenerateStudyWords} disabled={studyStatus === "loading"}>
              {studyStatus === "loading" ? "Generating..." : studyWords.length === 10 ? "↻ Regenerate" : "Generate"}
            </button>
          </div>

          <div className="profile-value">
            Generate 10 words and a practical context text for level {selectedEnglishLevel.toUpperCase()}.
          </div>

          {shouldShowStudySkeleton && (
            <div className="study-pack-skeleton" aria-hidden="true">
              <div className="skeleton-line skeleton-line-wide" />
              <div className="skeleton-line skeleton-line-wide" />
              <div className="skeleton-line skeleton-line-medium" />
            </div>
          )}

          {!shouldShowStudySkeleton && studyWords.length > 0 && (
            <div className="study-words-grid">
              {studyWords.map((word) => (
                <div key={word.toLowerCase()} className="study-word-chip">
                  {word}
                </div>
              ))}
            </div>
          )}

          {!shouldShowStudySkeleton && studyParagraphs.length > 0 && (
            <div className="study-text-card">
              {studyParagraphs.map((paragraph, index) => (
                <p key={`study-paragraph-${index}`}>
                  {buildStudyTextSegments(paragraph, studyWords).map((segment, segmentIndex) =>
                    segment.isStudyWord ? (
                      <mark key={`study-segment-${index}-${segmentIndex}`} className="study-word-mark">
                        {segment.text}
                      </mark>
                    ) : (
                      <span key={`study-segment-${index}-${segmentIndex}`}>{segment.text}</span>
                    )
                  )}
                </p>
              ))}
            </div>
          )}

          {!shouldShowStudySkeleton && studyWords.length === 0 && (
            <div className="empty-state speak-empty-state">Generate vocabulary set to start learning words in context.</div>
          )}

          {studyError && <div className="auth-error top-spaced">{studyError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Custom topic</div>
            <button className="btn btn-secondary btn-small" onClick={() => dispatch(toggleAddTopicInput())}>
              {showAddTopicInput ? "Hide" : "+ Add topic"}
            </button>
          </div>

          {showAddTopicInput && (
            <div className="add-topic-input visible">
              <input
                type="text"
                placeholder="Write your topic..."
                value={customTopicDraft}
                onChange={(event) => dispatch(setCustomTopicDraft(event.target.value))}
              />
              <div className="topic-input-buttons">
                <button className="btn btn-secondary" onClick={() => dispatch(toggleAddTopicInput())}>
                  Cancel
                </button>
                <button className="btn btn-primary" onClick={() => dispatch(applyCustomTopic())}>
                  Use this topic
                </button>
              </div>
            </div>
          )}
        </div>
      </section>
    );
  }

  if (speakState === "readyToRecord") {
    const isPhotoPractice = recordingPracticeType === "photo_description";
    const preparationFailure = interviewPreparationError
      ?? (interview?.status === "failed" ? interview.error || "Could not prepare this interview." : null);
    const isTopicGuidancePreparing =
      !isPhotoPractice && !preparationFailure && interview?.status !== "ready";
    const shouldShowWords = !isPhotoPractice && showWords && Boolean(interview?.usefulWords.length);
    const shouldShowGuidanceSkeleton =
      !isPhotoPractice && isTopicGuidancePreparing && !interview?.usefulWords.length;

    return (
      <section className="speak-screen">
        <div className="speak-card speak-hero-card">
          <button
            className="btn btn-secondary btn-small"
            onClick={onBackToQuestionsList}
            disabled={recordingStarting}
          >
            ← Back to questions
          </button>
          <div className="heading-sm">Selected question</div>
          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo to describe" className="photo-practice-preview" />
          )}
          <h2 className="heading-xl speak-heading-tight">{isPhotoPractice ? selectedTopic : interview?.openingQuestion ?? selectedTopic}</h2>
          {!isPhotoPractice && <div className="profile-value">The next questions are prepared privately and will appear one at a time while you speak.</div>}

          {quotaHint && <div className="notice">{quotaHint}</div>}

          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={onStartTopicRecording}
            disabled={
              recordingStarting ||
                (isPhotoPractice && !pendingPhotoDataUrl) ||
                isTopicGuidancePreparing ||
                (!isPhotoPractice && interview?.status !== "ready")
            }
          >
            {recordingStarting ? "Starting..." : preparationFailure ? "Preparation failed" : isTopicGuidancePreparing ? "Preparing interview..." : "Start speaking"}
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
          {pendingPhotoError && <div className="auth-error top-spaced">{pendingPhotoError}</div>}
        </div>

        {isPhotoPractice ? (
          <div className="speak-card">
            <div className="section-title speak-section-title">Photo focus</div>
            <div className="question-item">Describe the object and what details you notice.</div>
            <div className="question-item">Mention color, shape, material, and where it is located.</div>
            <div className="question-item">Say how this object could be used in real life.</div>
          </div>
        ) : (
          <div className="speak-card">
            <div className="speak-section-header">
              <div className="section-title speak-section-title">Interview preparation</div>
              <button
                className="btn btn-secondary btn-small"
                onClick={onRefreshTopicGuidance}
                disabled={recordingStarting}
              >
                {recordingStarting ? "Starting..." : isTopicGuidancePreparing ? "Preparing..." : "↻ Regenerate"}
              </button>
            </div>

            {shouldShowGuidanceSkeleton && (
              <div className="guidance-skeleton" aria-hidden="true">
                <div className="guidance-skeleton-title skeleton-line skeleton-line-short" />
                <div className="guidance-skeleton-item skeleton-line skeleton-line-wide" />
                <div className="guidance-skeleton-item skeleton-line skeleton-line-wide" />
                <div className="guidance-skeleton-item skeleton-line skeleton-line-medium" />
              </div>
            )}

            {Boolean(interview?.usefulWords.length) && (
              <div className="collapsible-section">
                <button className="collapsible-header" onClick={() => dispatch(toggleWords())}>
                  <span>Useful words</span>
                  <span className={`toggle-arrow ${showWords ? "open" : ""}`}>↓</span>
                </button>
                <div className={`collapsible-content ${shouldShowWords ? "open" : ""}`}>
                  {interview?.usefulWords.map((word) => (
                    <div key={word} className="word-item">
                      {word}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {preparationFailure && (
              <div className="auth-error top-spaced">
                {preparationFailure}
                <button className="btn btn-secondary btn-small top-spaced" onClick={onRefreshTopicGuidance}>Retry preparation</button>
              </div>
            )}
          </div>
        )}
      </section>
    );
  }

  if (speakState === "recording") {
    const isPhotoPractice = recordingPracticeType === "photo_description";
    const isTopicInterview = recordingPracticeType === "topic" && Boolean(selectedTopic);
    const currentTurn = interview?.turns[interview.turns.length - 1];
    const hasCurrentAnswer = hasInterviewAnswerEvidence(currentTurn, currentAnswerHasSpeech);

    return (
      <section className="speak-screen">
        {isTopicInterview && Boolean(interview?.usefulWords.length) && <GuidanceWordTicker words={interview?.usefulWords ?? []} />}

        <div className="speak-card speak-center-card">
          <div className="recording-indicator">
            <div className="recording-dot" />
            <span>{isTopicInterview ? "Topic interview" : selectedTopic ?? "Free talk"}</span>
          </div>

          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo being described" className="photo-practice-preview" />
          )}

          <div className="timer">{formatTime(recordingDuration)}</div>
          <div className="recorded-subtitle">
            {isAuthenticated
              ? `Session limit: ${formatTime(Math.max(0, sessionLimitSeconds))}`
              : `Guest preview limit: ${formatTime(MAX_GUEST_PREVIEW_SECONDS)}`}
          </div>

          {isTopicInterview && interview && (
            <InterviewQuestionCard
              turns={interview.turns}
              canAdvance={interview.candidates.length > 0
                && hasCurrentAnswer
                && !interviewBoundaryPending
                && interviewElapsedMs() - (interview.turns[interview.turns.length - 1]?.askedAtMs ?? 0) >= MIN_ANSWER_MS
                && interviewElapsedMs() < (recordingLimitMsRef.current ?? Number.POSITIVE_INFINITY)}
              onNext={onNextInterviewQuestion}
              liveTranscriptionAvailable={liveTranscriptionAvailable}
              hasAnswerEvidence={hasCurrentAnswer}
              boundaryPending={interviewBoundaryPending}
            />
          )}

          {isTopicInterview && interviewSyncError && <div className="notice top-spaced">{interviewSyncError}</div>}
          {isTopicInterview && interviewLiveWarning && <div className="notice top-spaced">{interviewLiveWarning}</div>}
          {isTopicInterview && interviewAnswerWarning && <div className="auth-error top-spaced">{interviewAnswerWarning}</div>}

          <button className="btn btn-primary btn-large speak-primary-btn" onClick={onStopRecording}>
            Stop
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
        </div>

      </section>
    );
  }

  const isPhotoPractice = recordingPracticeType === "photo_description";

  return (
    <section className="speak-screen">
      <div className="speak-card speak-center-card">
        <div className="recorded-banner">
          <div className="recorded-title">{isPhotoPractice ? "Photo session complete" : "Recording complete"}</div>
          <div className="recorded-subtitle">Duration: {formatTime(recordingDuration)}</div>
        </div>

        {isPhotoPractice && pendingPhotoDataUrl && (
          <img src={pendingPhotoDataUrl} alt="Photo from completed session" className="photo-practice-preview" />
        )}

        {quotaHint && <div className="notice">{quotaHint}</div>}
        {interviewStopNotice && <div className="notice top-spaced">{interviewStopNotice}</div>}
        {interviewLiveWarning && <div className="notice top-spaced">{interviewLiveWarning}</div>}

        {!isAuthenticated && !isPhotoPractice && (
          <div className="notice">
            Continue as a guest to see your transcript and a limited error preview. Sign in afterwards to unlock the full analysis.
          </div>
        )}
        {!isAuthenticated && isPhotoPractice && (
          <div className="notice">Photo analysis currently requires an account.</div>
        )}

        <div className="btn-group speak-button-group">
          <button className="btn btn-secondary" onClick={() => {
            if (recordingPracticeType === "topic") cancelCurrentInterview();
            recordingAttemptRef.current += 1;
            setGuestSaveError(null);
            setGuestSaveStatus("idle");
            setInterviewSaveError(null);
            setInterviewSaveStatus("idle");
            interviewPreparationKeyRef.current = null;
            interviewPreparationGenerationRef.current += 1;
            interviewSaveDraftRef.current = null;
            setInterviewLiveWarning(null);
            setInterviewCaptureFailure(null);
            setInterviewAnswerWarning(null);
            interviewBoundaryPendingRef.current = false;
            setInterviewBoundaryPending(false);
            updateCurrentAnswerSpeech(false);
            updateInterview(() => null);
            dispatch(reRecord());
          }} disabled={guestSaveStatus === "uploading" || interviewSaveStatus === "uploading"}>
            Re-record
          </button>
          <button
            className="btn btn-primary"
            onClick={onSaveRecording}
            disabled={recordingSaveStatus === "loading" || guestSaveStatus === "uploading" || interviewSaveStatus === "uploading" || !pendingRecordingAudioDataUrl || Boolean(interviewCaptureFailure)}
          >
            {isAuthenticated
              ? recordingSaveStatus === "loading" || interviewSaveStatus === "uploading"
                ? "Saving..."
                : "Save and continue"
              : guestSaveStatus === "uploading" || interviewSaveStatus === "uploading"
                ? "Preparing preview..."
                : isPhotoPractice
                  ? "Sign in to analyze"
                  : "View guest preview"}
          </button>
        </div>
        {!pendingRecordingAudioDataUrl && !recordingInputError && (
          <div className="notice top-spaced">Preparing audio, please wait a moment before saving.</div>
        )}
        {pendingRecordingAudioDataUrl && (
          <div className="notice top-spaced">
            {recordingPracticeType === "topic"
              ? "Audio is ready. Saving will wait for each answer transcript."
              : "Audio is ready. Saving will start background transcription."}
          </div>
        )}
        {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
        {recordingSaveError && <div className="auth-error top-spaced">{recordingSaveError}</div>}
        {guestSaveError && <div className="auth-error top-spaced">{guestSaveError}</div>}
        {interviewSaveError && <div className="auth-error top-spaced">{interviewSaveError}</div>}
      </div>
    </section>
  );
}
