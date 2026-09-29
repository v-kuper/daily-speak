"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
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
  recordingElapsedMs,
  recordingMustStop,
  resolveBrowserRecordingSupportError,
  resolveMicrophoneError,
  resolveRecordingHardLimitMs,
  resolvePreferredAudioMimeType,
  stopMediaRecorderSafely
} from "../lib/browserMedia";
import { storeRecordingDraftAudio } from "../lib/recordingDraftAudio";
import {
  advanceInterview,
  cancelInterview,
  finalizeInterview,
  getInterview,
  getInterviewQuestionSpeechToken,
  getInterviewTranscriptionToken,
  mergeInterviewTranscriptStatus,
  preserveLiveInterviewTurn,
  prepareInterview,
  skipInterviewTurn,
  startInterview,
  submitInterviewTurnTranscript,
  uploadInterviewTurnAudio,
  type InterviewSession,
  type InterviewTurn,
} from "../lib/interviewSession";
import {
  connectLiveTranscription,
  RealtimeTranscriptionError,
  type LiveTranscriptionConnection,
  type RealtimeFailureReason,
} from "../lib/liveTranscription";
import { EphemeralCaptionController } from "../lib/ephemeralCaption";
import { InterviewTurnCapture, type CapturedInterviewTurn } from "../lib/interviewTurnCapture";
import { fetchQuestionSpeech, QuestionSpeechPlayer, type QuestionSpeechState } from "../lib/questionSpeech";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import {
  advanceInterviewTimeline,
  closeInterviewTimeline,
  commitInterviewAdvance,
  hasInterviewAnswerEvidence,
  MAX_LIVE_SEGMENT_ATTEMPTS,
  MIN_ANSWER_MS,
  resolveInterviewRecordingLimitSeconds,
  rotateFailedInterviewSegment,
} from "../lib/interviewFlow";
import { newIdempotencyKey } from "../lib/mediaUpload";
import { collectRecentAnsweredQuestions, questionHistoryKey } from "../lib/dailyQuestionHistory";
import { formatTime, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector, useAppStore } from "../store/hooks";
import {
  backToQuestionsList,
  clearPhotoForPractice,
  clearQuestionsError,
  fetchDailyQuestions,
  MAX_AUTHENTICATED_RECORDING_SECONDS,
  PHOTO_PRACTICE_MAX_BYTES,
  reRecord,
  type RecordingSaveDraft,
  selectTopic,
  setCustomTopicDraft,
  setRecordingAudioStorageKey,
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
  const [liveTranscriptionIssue, setLiveTranscriptionIssue] = useState<RealtimeFailureReason | null>(null);
  const [interviewLiveCaption, setInterviewLiveCaption] = useState<string | null>(null);
  const [interviewLiveWarning, setInterviewLiveWarning] = useState<string | null>(null);
  const [interviewCaptureFailure, setInterviewCaptureFailure] = useState<string | null>(null);
  const [interviewStopNotice, setInterviewStopNotice] = useState<string | null>(null);
  const [currentAnswerHasSpeech, setCurrentAnswerHasSpeech] = useState(false);
  const [interviewBoundaryPending, setInterviewBoundaryPending] = useState(false);
  const [questionSpeechState, setQuestionSpeechState] = useState<QuestionSpeechState>("idle");
  const [questionSpeechError, setQuestionSpeechError] = useState<string | null>(null);
  const [questionSpeechMuted, setQuestionSpeechMuted] = useState(false);
  const [openingAudioError, setOpeningAudioError] = useState<string | null>(null);
  const [openingAudioRetryToken, setOpeningAudioRetryToken] = useState(0);
  const [interviewRefreshToken, setInterviewRefreshToken] = useState(0);
  const {
    speakState,
    selectedTopic,
    recordingDuration,
    topics,
    showAddTopicInput,
    customTopicDraft,
    isAuthenticated,
    questionsStatus,
    questionsError,
    selectedInterestIds,
    selectedEnglishLevel,
    recordings,
    userDataStatus,
    recordingSaveStatus,
    recordingSaveError,
    recordingPracticeType,
    pendingRecordingAudioStorageKey,
    recordingInputError,
    pendingPhotoDataUrl,
    pendingPhotoObjectDraft,
    pendingPhotoError
  } = useAppSelector((state) => state.app);

  const recentAnsweredQuestions = useMemo(
    () => collectRecentAnsweredQuestions(recordings),
    [recordings],
  );
  const recentAnsweredQuestionsKey = useMemo(
    () => questionHistoryKey(recentAnsweredQuestions),
    [recentAnsweredQuestions],
  );

  const sessionLimitSeconds = MAX_AUTHENTICATED_RECORDING_SECONDS;

  const quotaHint = isAuthenticated
    ? `Account recording limit: ${formatTime(MAX_AUTHENTICATED_RECORDING_SECONDS)} per recording.`
    : null;
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);
  const recordingStartingRef = useRef(false);
  const customTopicInputRef = useRef<HTMLInputElement | null>(null);
  const autoStartTopicRef = useRef<string | null>(null);
  const autoStartPhotoRef = useRef(false);
  const photoSelectionAttemptRef = useRef(0);
  const startTopicRecordingRef = useRef<() => void>(() => undefined);
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
  const skippedInterviewTurnSeqsRef = useRef(new Set<number>());
  const interviewSyncQueueRef = useRef<InterviewSyncOperation[]>([]);
  const interviewSyncRunningRef = useRef(false);
  const interviewSegmentQueueRef = useRef<InterviewSegmentOperation[]>([]);
  const interviewSegmentRunPromiseRef = useRef<Promise<void> | null>(null);
  const runInterviewSegmentsRef = useRef<(() => Promise<void>) | null>(null);
  const turnCaptureRef = useRef<InterviewTurnCapture | null>(null);
  const interviewRealtimeRef = useRef<LiveTranscriptionConnection | null>(null);
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
  const interviewCaptionControllerRef = useRef<EphemeralCaptionController | null>(null);
  const questionSpeechPlayerRef = useRef<QuestionSpeechPlayer | null>(null);
  const questionSpeechGenerationRef = useRef(0);
  const automaticallySpokenQuestionRef = useRef<string | null>(null);
  const prefetchedQuestionSpeechRef = useRef<string | null>(null);

  useEffect(() => {
    const controller = new EphemeralCaptionController(setInterviewLiveCaption);
    interviewCaptionControllerRef.current = controller;
    return () => {
      controller.dispose();
      if (interviewCaptionControllerRef.current === controller) {
        interviewCaptionControllerRef.current = null;
      }
    };
  }, []);

  const clearInterviewLiveCaption = useCallback(() => {
    interviewCaptionControllerRef.current?.clear();
  }, []);

  const updateInterview = useCallback((updater: (current: InterviewSession | null) => InterviewSession | null) => {
    const next = updater(interviewRef.current);
    interviewRef.current = next;
    setInterview(next);
  }, []);

  const updateCurrentAnswerSpeech = useCallback((active: boolean) => {
    currentAnswerHasSpeechRef.current = active;
    setCurrentAnswerHasSpeech(active);
  }, []);

  const currentInterviewAnswerIsPresent = useCallback((session = interviewRef.current): boolean => {
    const current = session?.turns[session.turns.length - 1];
    return hasInterviewAnswerEvidence(
      current,
      currentAnswerHasSpeechRef.current || Boolean(turnCaptureRef.current?.hasSpeechActivity()),
    );
  }, []);

  const setInterviewMicrophoneMuted = useCallback((muted: boolean) => {
    for (const track of mediaStreamRef.current?.getAudioTracks() ?? []) {
      track.enabled = !muted;
    }
  }, []);

  const stopQuestionSpeech = useCallback((clearError = true) => {
    questionSpeechGenerationRef.current += 1;
    questionSpeechPlayerRef.current?.stop();
    setInterviewMicrophoneMuted(false);
    if (mountedRef.current) {
      setQuestionSpeechState("idle");
      if (clearError) setQuestionSpeechError(null);
    }
  }, [setInterviewMicrophoneMuted]);

  const playInterviewQuestion = useCallback((question: string) => {
    const session = interviewRef.current;
    if (!session || interviewEndedAtMsRef.current !== null) return;
    const player = questionSpeechPlayerRef.current ?? new QuestionSpeechPlayer();
    questionSpeechPlayerRef.current = player;
    const generation = ++questionSpeechGenerationRef.current;
    setQuestionSpeechError(null);
    setQuestionSpeechState("loading");
    setInterviewMicrophoneMuted(true);
    void player.play(
      question,
      async () => fetchQuestionSpeech(await getInterviewQuestionSpeechToken(session.id), question),
      () => {
        if (mountedRef.current && generation === questionSpeechGenerationRef.current) {
          setQuestionSpeechState("playing");
        }
      },
    ).then(() => {
      if (!mountedRef.current || generation !== questionSpeechGenerationRef.current) return;
      setInterviewMicrophoneMuted(false);
      setQuestionSpeechState("idle");
    }).catch(() => {
      if (!mountedRef.current || generation !== questionSpeechGenerationRef.current) return;
      setInterviewMicrophoneMuted(false);
      setQuestionSpeechState("error");
      setQuestionSpeechError("Could not play this question. Try again.");
    });
  }, [setInterviewMicrophoneMuted]);

  const onListenInterviewQuestion = useCallback((question: string) => {
    if (questionSpeechMuted) return;
    if (questionSpeechPlayerRef.current?.isActive()) {
      stopQuestionSpeech();
      return;
    }
    playInterviewQuestion(question);
  }, [playInterviewQuestion, questionSpeechMuted, stopQuestionSpeech]);

  const onToggleQuestionSpeechMuted = useCallback(() => {
    setQuestionSpeechMuted((muted) => {
      if (!muted) stopQuestionSpeech();
      return !muted;
    });
  }, [stopQuestionSpeech]);

  const visibleInterviewTurn = speakState === "recording" && recordingPracticeType === "topic"
    ? interview?.turns[interview.turns.length - 1]
    : undefined;
  const visibleInterviewQuestionKey = visibleInterviewTurn && interview
    ? `${interview.id}:${visibleInterviewTurn.seq}:${visibleInterviewTurn.question}`
    : null;

  useEffect(() => {
    if (!visibleInterviewQuestionKey || !visibleInterviewTurn || interviewBoundaryPending) return;
    if (automaticallySpokenQuestionRef.current === visibleInterviewQuestionKey) return;
    automaticallySpokenQuestionRef.current = visibleInterviewQuestionKey;
    if (!questionSpeechMuted) playInterviewQuestion(visibleInterviewTurn.question);
  }, [
    interviewBoundaryPending,
    playInterviewQuestion,
    questionSpeechMuted,
    visibleInterviewQuestionKey,
    visibleInterviewTurn,
  ]);

  useEffect(() => {
    if (speakState === "recording" && recordingPracticeType === "topic") return;
    automaticallySpokenQuestionRef.current = null;
    prefetchedQuestionSpeechRef.current = null;
  }, [recordingPracticeType, speakState]);

  const nextInterviewCandidate = speakState === "recording" && recordingPracticeType === "topic"
    ? interview?.candidates[0]
    : undefined;
  const nextInterviewCandidateKey = nextInterviewCandidate && interview
    ? `${interview.id}:${nextInterviewCandidate.id}:${nextInterviewCandidate.question}`
    : null;

  useEffect(() => {
    if (!nextInterviewCandidate || !nextInterviewCandidateKey || !interview || questionSpeechMuted) return;
    if (prefetchedQuestionSpeechRef.current === nextInterviewCandidateKey) return;
    prefetchedQuestionSpeechRef.current = nextInterviewCandidateKey;
    const player = questionSpeechPlayerRef.current ?? new QuestionSpeechPlayer();
    questionSpeechPlayerRef.current = player;
    void player.preload(
      nextInterviewCandidate.question,
      async () => fetchQuestionSpeech(
        await getInterviewQuestionSpeechToken(interview.id),
        nextInterviewCandidate.question,
      ),
    ).catch(() => undefined);
  }, [interview, nextInterviewCandidate, nextInterviewCandidateKey, questionSpeechMuted]);

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
      const visibleServerTurns = server.turns.filter((turn) => !skippedInterviewTurnSeqsRef.current.has(turn.seq));
      if (!current || current.id !== server.id) return { ...server, turns: visibleServerTurns };
      const visibleCurrentTurns = current.turns.filter((turn) => !skippedInterviewTurnSeqsRef.current.has(turn.seq));
      const serverTurns = new Map(visibleServerTurns.map((turn) => [turn.seq, turn]));
      const turns = visibleCurrentTurns.length > visibleServerTurns.length
        ? visibleCurrentTurns.map((turn) => {
            const latest = serverTurns.get(turn.seq);
            return latest ? {
              ...preserveLiveInterviewTurn(latest, turn),
              askedAtMs: turn.askedAtMs,
              endedAtMs: turn.endedAtMs ?? latest.endedAtMs,
              transcriptStatus: mergeInterviewTranscriptStatus(latest.transcriptStatus, turn.transcriptStatus),
            } : turn;
          })
        : visibleServerTurns.map((turn) => {
            const local = visibleCurrentTurns.find((item) => item.seq === turn.seq);
            return local ? {
              ...preserveLiveInterviewTurn(turn, local),
              askedAtMs: local.askedAtMs,
              endedAtMs: local.endedAtMs ?? turn.endedAtMs,
              transcriptStatus: mergeInterviewTranscriptStatus(turn.transcriptStatus, local.transcriptStatus),
            } : turn;
          });
      const candidates = [...server.candidates, ...(authoritativeCandidates ? [] : current.candidates)]
        .filter((candidate) => !usedInterviewCandidateIdsRef.current.has(candidate.id))
        .filter((candidate, index, all) => all.findIndex((item) => item.id === candidate.id) === index)
        .slice(0, 1);
      return {
        ...server,
        turns,
        candidates,
        currentTurnSeq: turns.length ? turns[turns.length - 1].seq : null,
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
    stopQuestionSpeech();
    clearInterviewLiveCaption();
    const session = interviewRef.current;
    if (!session || interviewStartedAtRef.current === null || interviewEndedAtMsRef.current !== null) return;
    const last = session.turns[session.turns.length - 1];
    const answerPresent = currentInterviewAnswerIsPresent(session);
    const endedAtMs = Math.max(interviewElapsedMs(), last ? last.askedAtMs + 1 : 0);
    interviewEndedAtMsRef.current = endedAtMs;
    if (last) {
      const skipLast = !answerPresent;
      if (skipLast) {
        skippedInterviewTurnSeqsRef.current.add(last.seq);
        setInterviewStopNotice("The unanswered final question was skipped. Your completed answers can still be saved.");
        const skipKey = newIdempotencyKey(`interview-skip-${last.seq}`);
        queueInterviewSync(session.id, () => skipInterviewTurn(session.id, last.seq, endedAtMs, skipKey));
      }
      updateInterview((current) => current
        ? closeInterviewTimeline(current, endedAtMs, skipLast).session
        : current);
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
          if (!answerPresent) {
            setLiveTranscriptionAvailable(false);
            return;
          }
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
  }, [clearInterviewLiveCaption, currentInterviewAnswerIsPresent, dispatch, interviewElapsedMs, queueInterviewSegment, queueInterviewSync, stopQuestionSpeech, updateCurrentAnswerSpeech, updateInterview]);

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

  const synchronizeInterviewBeforeSave = useCallback(async (): Promise<"ready" | "pending" | "failed" | "empty"> => {
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
      if (current?.id && endedTurns.length === 0) {
        return "empty";
      }
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
      dispatch(setRecordingAudioStorageKey(null));

      const recordingSupportError = resolveBrowserRecordingSupportError();
      if (recordingSupportError) {
        dispatch(setRecordingInputError(recordingSupportError));
        return;
      }

      let stream: MediaStream | null = null;
      let recorder: MediaRecorder | null = null;
      let localCapture: InterviewTurnCapture | null = null;
      let localRealtime: LiveTranscriptionConnection | null = null;
      let realtimeFailureReported = false;
      const reportRealtimeFailure = (error: unknown, fallbackReason: RealtimeFailureReason = "unknown") => {
        if (!isCurrent() || realtimeFailureReported) return;
        realtimeFailureReported = true;
        const reason = error instanceof RealtimeTranscriptionError ? error.reason : fallbackReason;
        const closeCode = error instanceof RealtimeTranscriptionError ? error.closeCode : undefined;
        // Keep provider messages, tokens, and transcripts out of diagnostics.
        console.warn("interview.live_transcription_paused", { reason, closeCode });
        setLiveTranscriptionIssue(reason);
      };
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

        stream = await getUserMedia({
          audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        });
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
          void storeRecordingDraftAudio(blob)
            .then((storageKey) => {
              if (isCurrent()) dispatch(setRecordingAudioStorageKey(storageKey));
            })
            .catch(() => {
              if (isCurrent()) dispatch(setRecordingInputError("Failed to process recorded audio."));
            });
        };

        if (captureInterview && localCapture) {
          try {
            const session = interviewRef.current;
            if (!session) throw new Error("The interview session is unavailable.");
            localRealtime = await connectLiveTranscription(
              await getInterviewTranscriptionToken(session.id),
              (error) => {
                if (!mountedRef.current || interviewRef.current?.id !== session.id) return;
                if (interviewRealtimeRef.current === localRealtime) {
                  interviewRealtimeRef.current = null;
                  turnCaptureRef.current?.setPCMListener(null);
                }
                clearInterviewLiveCaption();
                reportRealtimeFailure(error);
                setLiveTranscriptionAvailable(false);
              },
              (snapshot) => {
                const { turnSeq, finalText, interimText } = snapshot;
                if (!isCurrent() || interviewRef.current?.id !== session.id) return;
                updateInterview((current) => current?.id === session.id ? {
                  ...current,
                  turns: current.turns.map((turn) => {
                    if (turn.seq !== turnSeq || turn.transcriptStatus === "ready") return turn;
                    return {
                      ...turn,
                      liveTranscriptFinal: finalText,
                      liveTranscriptInterim: interimText,
                    };
                  }),
                } : current);
                const visibleTurn = interviewRef.current?.turns[interviewRef.current.turns.length - 1];
                if (recordingEndedAtMsRef.current === null
                  && visibleTurn?.seq === turnSeq && visibleTurn.endedAtMs === null) {
                  interviewCaptionControllerRef.current?.update(snapshot);
                }
              },
            );
            if (!isCurrent()) {
              await localRealtime.close();
              await localCapture.stop().catch(() => null);
              stopLocalStream();
              return;
            }
          } catch (error) {
            localRealtime = null;
            clearInterviewLiveCaption();
            reportRealtimeFailure(error, "connect_failed");
            setLiveTranscriptionAvailable(false);
          }
        }
        if (captureInterview && localCapture) {
          try {
            // The worklet collects while the token and socket are prepared.
            // Discard that setup partition before attaching the PCM listener,
            // so the realtime service receives only audio from the actual interview.
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
        if (localCapture && localRealtime && !realtimeFailureReported) {
          interviewRealtimeRef.current = localRealtime;
          interviewCaptionControllerRef.current?.beginTurn(1);
          localRealtime.beginTurn(1);
          localCapture.setPCMListener((pcm) => localRealtime?.sendPCM(pcm));
          setLiveTranscriptionAvailable(true);
          setLiveTranscriptionIssue(null);
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
    [clearInterviewLiveCaption, dispatch, finishActiveRecording, releaseMedia, updateCurrentAnswerSpeech, updateInterview]
  );

  const buildRecordingSaveDraft = useCallback((): RecordingSaveDraft | null => {
    const audioStorageKey = pendingRecordingAudioStorageKey?.trim() || null;
    if (!audioStorageKey) {
      return null;
    }
    if (recordingPracticeType === "topic" && interviewRef.current?.id
      && interviewSaveDraftRef.current?.interviewSessionId === interviewRef.current.id
      && interviewSaveDraftRef.current.audioStorageKey === audioStorageKey) {
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
      audioStorageKey,
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
    pendingRecordingAudioStorageKey,
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
          const emptyMessage = "This interview has no answered questions. Record at least one answer before saving.";
          if (transcriptState === "failed") {
            setInterviewCaptureFailure(terminalMessage);
            dispatch(setRecordingInputError(terminalMessage));
          }
          setInterviewSaveStatus("failed");
          setInterviewSaveError(
            transcriptState === "failed"
              ? terminalMessage
              : transcriptState === "empty"
                ? emptyMessage
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
            audioStorageKey: readyDraft.audioStorageKey ?? "",
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
        audioStorageKey: draft.audioStorageKey ?? "",
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
      stopQuestionSpeech();
      clearInterviewLiveCaption();
      setLiveTranscriptionIssue(null);
      setInterviewCaptureFailure(null);
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
    if (recordingPracticeType === "topic" && !questionSpeechMuted) {
      const player = questionSpeechPlayerRef.current ?? new QuestionSpeechPlayer();
      questionSpeechPlayerRef.current = player;
      player.unlock();
    }
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
        usefulWords: session.openingUsefulWords,
        askedAtMs: 0,
        endedAtMs: null,
        provisionalTranscript: "",
      };
      updateInterview((current) => current ? { ...current, status: "recording", turns: [opening], currentTurnSeq: 1 } : current);
      queueInterviewSync(session.id, () => startInterview(session.id, `interview:${session.id}:start`));
    }, recordingPracticeType === "topic");
  };

  useEffect(() => {
    startTopicRecordingRef.current = onStartTopicRecording;
  });

  const prepareTopicForRecording = (topic: string) => {
    if (!topic) return;
    if (!questionSpeechMuted) {
      const player = questionSpeechPlayerRef.current ?? new QuestionSpeechPlayer();
      questionSpeechPlayerRef.current = player;
      player.unlock();
    }
    autoStartTopicRef.current = topic;
    setOpeningAudioError(null);
    setInterviewPreparationError(null);
  };

  const onSelectTopic = (topic: string) => {
    prepareTopicForRecording(topic);
    dispatch(selectTopic(topic));
  };

  const onStartCustomTopic = () => {
    const topic = customTopicDraft.trim();
    if (!topic) return;
    prepareTopicForRecording(topic);
    dispatch(applyCustomTopic());
  };

  const onStopRecording = () => {
    finishActiveRecording();
  };

  const onNextInterviewQuestion = () => {
    const session = interviewRef.current;
    if (!session || interviewEndedAtMsRef.current !== null || interviewBoundaryPendingRef.current) return;
    stopQuestionSpeech();
    const answerPresent = currentInterviewAnswerIsPresent(session);
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
    const advance = advanceInterviewTimeline(
      session,
      usedInterviewCandidateIdsRef.current,
      elapsedMs,
      maxAtMs,
      !answerPresent,
    );
    if (!advance) return;
    const { candidate, previousTurn } = advance;
    const nextTurnSeq = advance.session.turns[advance.session.turns.length - 1].seq;
    interviewCaptionControllerRef.current?.beginTurn(nextTurnSeq);
    interviewBoundaryPendingRef.current = true;
    setInterviewBoundaryPending(true);
    const realtime = interviewRealtimeRef.current;
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
      if (!answerPresent) skippedInterviewTurnSeqsRef.current.add(previousTurn.seq);
      updateCurrentAnswerSpeech(false);
      const key = newIdempotencyKey(`interview-advance-${previousTurn.seq}`);
      queueInterviewSync(session.id, async () => {
        const result = await advanceInterview(
          session.id,
          previousTurn.seq,
          candidate.id,
          advance.session.turns[advance.session.turns.length - 1].askedAtMs,
          key,
          !answerPresent,
        );
        if (answerPresent && generation === interviewGenerationRef.current) {
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
      questionSpeechPlayerRef.current?.dispose();
      questionSpeechPlayerRef.current = null;
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
    if (isAuthenticated && userDataStatus !== "ready" && userDataStatus !== "failed") return;
    const dateKey = toDateKey(new Date());
    void dispatch(fetchDailyQuestions({
      dateKey,
      interestIds: selectedInterestIds,
      avoidQuestions: recentAnsweredQuestions,
      englishLevel: selectedEnglishLevel,
    }));
  }, [
    dispatch,
    isAuthenticated,
    recentAnsweredQuestions,
    recentAnsweredQuestionsKey,
    selectedEnglishLevel,
    selectedInterestIds,
    userDataStatus,
  ]);

  useEffect(() => {
    if (speakState === "idle") {
      clearInterviewLiveCaption();
      if (interviewRef.current?.id && !interviewSavingRef.current) cancelCurrentInterview();
      interviewPreparationKeyRef.current = null;
      interviewPreparationGenerationRef.current += 1;
      interviewGenerationRef.current += 1;
      skippedInterviewTurnSeqsRef.current.clear();
      interviewSyncQueueRef.current = [];
      interviewSegmentQueueRef.current = [];
      interviewBoundaryPendingRef.current = false;
      stopQuestionSpeech();
      setQuestionSpeechMuted(false);
      setInterviewBoundaryPending(false);
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
    skippedInterviewTurnSeqsRef.current.clear();
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
    clearInterviewLiveCaption();
    setLiveTranscriptionIssue(null);
    setInterviewLiveWarning(null);
    setInterviewCaptureFailure(null);
    setInterviewStopNotice(null);
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
  }, [cancelCurrentInterview, clearInterviewLiveCaption, interviewRefreshToken, isAuthenticated, mergeInterview, recordingPracticeType, selectedEnglishLevel, selectedInterestIds, selectedTopic, speakState, stopQuestionSpeech, updateCurrentAnswerSpeech, updateInterview]);

  const openingInterviewId = interview?.id ?? null;
  const openingInterviewStatus = interview?.status ?? null;
  const openingQuestion = interview?.openingQuestion ?? null;

  useEffect(() => {
    if (speakState !== "readyToRecord" || recordingPracticeType !== "topic" || !selectedTopic ||
      autoStartTopicRef.current || recordingStarting || recordingStartingRef.current ||
      recordingInputError || openingAudioError || interviewPreparationError || openingInterviewStatus === "failed") return;
    // A restored or interrupted ready state still needs a way out of the loading screen.
    autoStartTopicRef.current = selectedTopic;
    setOpeningAudioRetryToken((value) => value + 1);
  }, [interviewPreparationError, openingAudioError, openingInterviewStatus, recordingInputError, recordingPracticeType, recordingStarting, selectedTopic, speakState]);

  useEffect(() => {
    const pendingTopic = autoStartTopicRef.current;
    if (!pendingTopic || speakState !== "readyToRecord" || recordingPracticeType !== "topic" ||
      selectedTopic !== pendingTopic || openingInterviewStatus !== "ready" || !openingInterviewId || !openingQuestion) return;
    if (questionSpeechMuted) {
      autoStartTopicRef.current = null;
      setOpeningAudioError(null);
      startTopicRecordingRef.current();
      return;
    }

    let active = true;
    const player = questionSpeechPlayerRef.current ?? new QuestionSpeechPlayer();
    questionSpeechPlayerRef.current = player;
    setOpeningAudioError(null);
    void player.preload(
      openingQuestion,
      async () => fetchQuestionSpeech(
        await getInterviewQuestionSpeechToken(openingInterviewId),
        openingQuestion,
      ),
    ).then(() => {
      if (!active || autoStartTopicRef.current !== pendingTopic || interviewRef.current?.id !== openingInterviewId) return;
      autoStartTopicRef.current = null;
      startTopicRecordingRef.current();
    }).catch(() => {
      if (active && autoStartTopicRef.current === pendingTopic) {
        setOpeningAudioError("The first question's audio could not be prepared.");
      }
    });
    return () => { active = false; };
  }, [openingAudioRetryToken, openingInterviewId, openingInterviewStatus, openingQuestion, questionSpeechMuted, recordingPracticeType, selectedTopic, speakState]);

  useEffect(() => {
    if (!autoStartPhotoRef.current || speakState !== "readyToRecord" ||
      recordingPracticeType !== "photo_description" || !pendingPhotoDataUrl) return;
    autoStartPhotoRef.current = false;
    startTopicRecordingRef.current();
  }, [pendingPhotoDataUrl, recordingPracticeType, speakState]);

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
    if (speakState === "idle" && showAddTopicInput) {
      customTopicInputRef.current?.focus();
    }
  }, [showAddTopicInput, speakState]);

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
        avoidQuestions: [...topics, ...recentAnsweredQuestions],
        englishLevel: selectedEnglishLevel
      })
    );
  };

  const onRefreshTopicGuidance = () => {
    if (!selectedTopic || recordingPracticeType === "photo_description") {
      return;
    }
    setOpeningAudioError(null);
    setInterviewRefreshToken((value) => value + 1);
  };

  const onBackToQuestionsList = () => {
    autoStartTopicRef.current = null;
    autoStartPhotoRef.current = false;
    photoSelectionAttemptRef.current += 1;
    cancelCurrentInterview();
    recordingAttemptRef.current += 1;
    recordingStartingRef.current = false;
    setRecordingStarting(false);
    if (recordingPracticeType === "photo_description") dispatch(clearPhotoForPractice());
    dispatch(backToQuestionsList());
  };

  const onPhotoSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.currentTarget.value = "";

    if (!file) {
      return;
    }
    const selectionAttempt = ++photoSelectionAttemptRef.current;

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
        if (selectionAttempt !== photoSelectionAttemptRef.current || store.getState().app.speakState !== "idle" || recordingStartingRef.current) return;
        dispatch(setPhotoObjectDraft(""));
        dispatch(setPhotoForPractice(dataUrl));
        const photoState = store.getState().app;
        if (photoState.pendingPhotoDataUrl !== dataUrl.trim() || photoState.pendingPhotoError) return;
        autoStartPhotoRef.current = true;
        dispatch(startPhotoDescription());
      })
      .catch(() => {
        if (selectionAttempt !== photoSelectionAttemptRef.current) return;
        dispatch(setPhotoUploadError("Failed to read selected photo."));
      });
  };

  if (speakState === "idle") {
    const shouldShowQuestionsSkeleton = questionsStatus === "loading" && topics.length === 0;

    return (
      <section className="speak-screen">
        <div className="speak-card">
          <button className="btn btn-primary speak-free-start" onClick={onStartFreeTalk} disabled={recordingStarting}>
            {recordingStarting ? "Starting..." : "Start free recording"}
          </button>
          <p className="profile-value">Talk about anything, read aloud, or practice a rule. No question needed.</p>
          {recordingInputError && <div className="auth-error">{recordingInputError}</div>}

          <div className="speak-section-header speak-choice-header">
            <div className="section-title speak-section-title">Or pick one of today&apos;s questions</div>
            <button
              className="btn btn-secondary btn-small"
              onClick={onRefreshQuestions}
              disabled={questionsStatus === "loading"}
            >
              {questionsStatus === "loading" ? "Finding..." : "↻ New questions"}
            </button>
          </div>

          <div className="topics-grid">
            {shouldShowQuestionsSkeleton ? (
              Array.from({ length: 3 }).map((_, index) => (
                <div key={`topic-skeleton-${index}`} className="topic-skeleton" aria-hidden="true">
                  <div className="skeleton-line skeleton-line-wide" />
                  <div className="skeleton-line skeleton-line-medium" />
                </div>
              ))
            ) : topics.map((topic) => (
              <button key={topic} className="topic-btn" onClick={() => onSelectTopic(topic)}>
                {topic}
              </button>
            ))}
            {showAddTopicInput ? (
              <form
                className="topic-btn custom-topic-form"
                onSubmit={(event) => {
                  event.preventDefault();
                  onStartCustomTopic();
                }}
              >
                <input
                  id="custom-topic-question"
                  ref={customTopicInputRef}
                  type="text"
                  aria-label="Your question"
                  placeholder="Write your question..."
                  value={customTopicDraft}
                  onChange={(event) => dispatch(setCustomTopicDraft(event.target.value))}
                  onKeyDown={(event) => {
                    if (event.key === "Escape") dispatch(toggleAddTopicInput());
                  }}
                />
                {Boolean(customTopicDraft.trim()) && <button type="submit" className="btn btn-primary btn-small">Start</button>}
              </form>
            ) : (
              <button type="button" className="topic-btn topic-btn-custom" onClick={() => dispatch(toggleAddTopicInput())}>
                <span aria-hidden="true">+ </span>Add your own question
              </button>
            )}
          </div>

          {topics.length === 0 && questionsStatus !== "loading" && (
            <div className="profile-value">No daily questions yet. You can still add your own.</div>
          )}
          {questionsError && <div className="auth-error top-spaced">{questionsError}</div>}
          <label className="topic-btn photo-topic-btn">
            <span>Describe a photo</span>
            <span className="profile-value">Choose an image and start recording</span>
            <input
              type="file"
              accept="image/jpeg,image/png,image/webp,image/gif"
              aria-label="Choose a photo to describe"
              onChange={onPhotoSelected}
            />
          </label>
          {pendingPhotoError && <div className="auth-error" role="alert">{pendingPhotoError}</div>}
        </div>
      </section>
    );
  }

  if (speakState === "readyToRecord") {
    if (recordingPracticeType === "topic") {
      const preparationFailure = interviewPreparationError
        ?? (interview?.status === "failed" ? interview.error || "Could not prepare your question." : null)
        ?? (interview?.status === "ready" && (!interview.id || !interview.openingQuestion.trim())
          ? "The first question is unavailable. Please try again."
          : null);
      const startupError = preparationFailure ?? openingAudioError ?? recordingInputError;

      return (
        <section className="speak-screen">
          <div className="speak-card speak-preparing-card">
            <button className="btn btn-secondary btn-small speak-preparing-back" onClick={onBackToQuestionsList} disabled={recordingStarting}>
              ← Back to questions
            </button>
            {startupError ? (
              <div className="speak-preparing-content">
                <h2 className="heading-xl">We couldn&apos;t start yet</h2>
                <p className="auth-error" role="alert">{startupError}</p>
                <div className="speak-preparing-actions">
                  {preparationFailure ? (
                    <button className="btn btn-primary" onClick={onRefreshTopicGuidance}>Try again</button>
                  ) : openingAudioError ? (
                    <>
                      <button className="btn btn-primary" onClick={() => setOpeningAudioRetryToken((value) => value + 1)}>Try audio again</button>
                      <button className="btn btn-secondary" onClick={() => setQuestionSpeechMuted(true)}>Start without audio</button>
                    </>
                  ) : (
                    <button className="btn btn-primary" onClick={onStartTopicRecording}>Try recording again</button>
                  )}
                </div>
              </div>
            ) : (
              <div className="speak-preparing-content" role="status" aria-live="polite">
                <span className="speak-preparing-spinner" aria-hidden="true" />
                <h2 className="heading-xl">Starting in a moment</h2>
                <p className="profile-value">
                  {recordingStarting
                    ? "Turning on your microphone..."
                    : interview?.status === "ready"
                      ? "Getting the first question ready to play..."
                      : "Preparing your first question and helpful words..."}
                </p>
                {selectedTopic && <p className="speak-preparing-topic">{selectedTopic}</p>}
              </div>
            )}
          </div>
        </section>
      );
    }

    const photoStartupError = recordingInputError ?? pendingPhotoError ?? (!pendingPhotoDataUrl ? "Choose a photo to begin." : null);
    return (
      <section className="speak-screen">
        <div className="speak-card speak-preparing-card">
          <button className="btn btn-secondary btn-small speak-preparing-back" onClick={onBackToQuestionsList} disabled={recordingStarting}>
            ← Back to questions
          </button>
          {photoStartupError ? (
            <div className="speak-preparing-content">
              <h2 className="heading-xl">We couldn&apos;t start yet</h2>
              <p className="auth-error" role="alert">{photoStartupError}</p>
              {pendingPhotoDataUrl && <button className="btn btn-primary" onClick={onStartTopicRecording}>Try recording again</button>}
            </div>
          ) : (
            <div className="speak-preparing-content" role="status" aria-live="polite">
              <span className="speak-preparing-spinner" aria-hidden="true" />
              <h2 className="heading-xl">Starting your photo recording</h2>
              <p className="profile-value">Turning on your microphone...</p>
              {pendingPhotoDataUrl && <img src={pendingPhotoDataUrl} alt="Photo to describe" className="photo-practice-preview" />}
            </div>
          )}
        </div>
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
        {isTopicInterview && Boolean(currentTurn?.usefulWords.length) && (
          <GuidanceWordTicker key={`${interview?.id}:${currentTurn?.seq}`} words={currentTurn?.usefulWords ?? []} />
        )}

        <div className="speak-card speak-center-card speak-recording-card">
          <div className="recording-session-meta">
            <div className="recording-indicator">
              <div className="recording-dot" />
              <span>{isTopicInterview ? "Topic interview" : selectedTopic ?? "Free talk"}</span>
            </div>
            <div className="recording-time-block">
              <div className="timer">{formatTime(recordingDuration)}</div>
              <div className="recorded-subtitle">
                {isAuthenticated
                  ? `Session limit: ${formatTime(Math.max(0, sessionLimitSeconds))}`
                  : `Guest preview limit: ${formatTime(MAX_GUEST_PREVIEW_SECONDS)}`}
              </div>
            </div>
          </div>

          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo being described" className="photo-practice-preview" />
          )}

          {isTopicInterview && interview && (
            <InterviewQuestionCard
              turns={interview.turns}
              canAdvance={interview.candidates.length > 0
                && !interviewBoundaryPending
                && interviewElapsedMs() - (interview.turns[interview.turns.length - 1]?.askedAtMs ?? 0) >= MIN_ANSWER_MS
                && interviewElapsedMs() < (recordingLimitMsRef.current ?? Number.POSITIVE_INFINITY)}
              onNext={onNextInterviewQuestion}
              onListen={onListenInterviewQuestion}
              onToggleSpeechMuted={onToggleQuestionSpeechMuted}
              speechState={questionSpeechState}
              speechError={questionSpeechError}
              speechMuted={questionSpeechMuted}
              liveTranscriptionAvailable={liveTranscriptionAvailable}
              liveTranscriptionIssue={liveTranscriptionIssue}
              liveCaption={interviewLiveCaption}
              hasAnswerEvidence={hasCurrentAnswer}
              boundaryPending={interviewBoundaryPending}
            />
          )}

          {isTopicInterview && interviewSyncError && <div className="notice top-spaced">{interviewSyncError}</div>}
          {isTopicInterview && interviewLiveWarning && <div className="notice top-spaced">{interviewLiveWarning}</div>}

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
            interviewBoundaryPendingRef.current = false;
            setInterviewBoundaryPending(false);
            updateCurrentAnswerSpeech(false);
            updateInterview(() => null);
            if (recordingPracticeType === "topic" && selectedTopic) {
              prepareTopicForRecording(selectedTopic);
            }
            if (recordingPracticeType === "photo_description" && pendingPhotoDataUrl) {
              autoStartPhotoRef.current = true;
            }
            dispatch(reRecord());
          }} disabled={guestSaveStatus === "uploading" || interviewSaveStatus === "uploading"}>
            Re-record
          </button>
          <button
            className="btn btn-primary"
            onClick={onSaveRecording}
            disabled={recordingSaveStatus === "loading" || guestSaveStatus === "uploading" || interviewSaveStatus === "uploading" || !pendingRecordingAudioStorageKey || Boolean(interviewCaptureFailure)}
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
        {!pendingRecordingAudioStorageKey && !recordingInputError && (
          <div className="notice top-spaced">Preparing audio, please wait a moment before saving.</div>
        )}
        {pendingRecordingAudioStorageKey && (
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
