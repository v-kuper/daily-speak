"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { deleteAndNavigate, reconcileRecordingSaveRoute, recordingDetailState, saveAndNavigate, startRecordingDetailLifecycle } from "../lib/routeFlows";

import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { buildTranscriptSegments } from "../lib/transcriptHighlight";
import { feedbackCardId, transcriptMarkId, type ReviewKind } from "../lib/feedbackAnchors";
import {
  recordingProcessingLabel,
  recordingRetryLabel,
  shouldShowShadowingProgress,
} from "../lib/recordingProcessing";
import {
  isShadowingStale,
  shadowingProgressLabel,
  shouldScheduleShadowing,
} from "../lib/shadowing";
import { useProtectedMediaURL } from "../lib/useProtectedMediaURL";
import { formatTime } from "../lib/utils";
import { loadRecordingDraftAudio } from "../lib/recordingDraftAudio";
import { useAppDispatch, useAppSelector, useAppStore } from "../store/hooks";
import {
  clearRecordingDeleteError,
  generateShadowingAudio,
  resetPlaybackState,
  retryRecordingProcessing,
  setPlaybackPlaying,
  setPlaybackPosition,
} from "../store/slices/appSlice";
import SuggestionCard from "./SuggestionCard";
import RecordingLoadError from "./RecordingLoadError";
import ProtectedMediaImage from "./ProtectedMediaImage";
import ConversationTranscript from "./ConversationTranscript";
import StrengthCard from "./StrengthCard";

const formatPracticeLabel = (value: "free_talk" | "topic" | "photo_description"): string => {
  switch (value) {
    case "free_talk":
      return "Free talk";
    case "photo_description":
      return "Photo description";
    default:
      return "Topic";
  }
};

const resolvePlaybackStartError = (error: unknown): string => {
  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") {
      return "Browser blocked playback. Click play again.";
    }
    if (error.name === "NotSupportedError") {
      return "This audio format is not supported by your browser.";
    }
    if (error.name === "AbortError") {
      return "Playback was interrupted. Try again.";
    }
  }

  return "Cannot start playback. Try again.";
};

const waitForAudioCanPlay = (audio: HTMLAudioElement): Promise<void> => {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      audio.removeEventListener("canplay", onCanPlay);
      audio.removeEventListener("error", onError);
    };

    const onCanPlay = () => {
      cleanup();
      resolve();
    };

    const onError = () => {
      cleanup();
      reject(new Error("Audio failed to load."));
    };

    audio.addEventListener("canplay", onCanPlay, { once: true });
    audio.addEventListener("error", onError, { once: true });
    audio.load();
  });
};

export default function DetailsScreen({ recordingId: routeRecordingId }: { recordingId: string }) {
  const dispatch = useAppDispatch();
  const store = useAppStore();
  const router = useRouter();
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const shadowingAudioRef = useRef<HTMLAudioElement | null>(null);
  const autoShadowingRequestedRef = useRef(new Set<string>());
  const [localAudioSrc, setLocalAudioSrc] = useState<string | null>(null);
  const [playbackError, setPlaybackError] = useState<string | null>(null);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [originalTranscriptOpen, setOriginalTranscriptOpen] = useState(true);
  const [correctedTranscriptOpen, setCorrectedTranscriptOpen] = useState(false);
  const [selectedReview, setSelectedReview] = useState<string | null>(null);
  const {
    isPlaying,
    playbackPosition,
    backgroundSaveRecordingId,
    recordingSaveDrafts,
    recordingSaveStatus,
    recordingSaveError,
    recordingDeleteStatus,
    recordingDeleteError,
    recordingRetryStatuses,
    recordingRetryErrors,
    shadowingRequestStatus,
    shadowingRequestError,
  } = useAppSelector(
    (state) => state.app
  );

  const { recording, error: recordingError } = useAppSelector(
    (state) => recordingDetailState(state.app, routeRecordingId),
    (previous, next) => previous.recording === next.recording && previous.error === next.error && previous.shouldFetch === next.shouldFetch,
  );
  const saveResult = useAppSelector((state) => state.app.recordingSaveResults[routeRecordingId]);

  useEffect(() => {
    reconcileRecordingSaveRoute(store, router, routeRecordingId, () => window.location.pathname);
  }, [store, router, routeRecordingId, saveResult]);

  useEffect(() => {
    setDeleteModalOpen(false);
    return startRecordingDetailLifecycle(store, routeRecordingId);
  }, [store, routeRecordingId]);
  const recordingId = recording?.id ?? null;
  const recordingAudioStorageKey = recording?.localAudioStorageKey ?? null;
  const recordingAudioDownloadPath = recording?.media?.audio?.downloadPath ?? null;
  const recordingStatus = recording?.status;
  const correctedTranscript = recording?.correctedTranscript ?? "";
  const shadowingStatus = recording?.shadowingStatus ?? "pending";
  const shadowingUpdatedAt = recording?.shadowingUpdatedAt ?? "";
  const shadowingDownloadPath = recording?.media?.shadowing?.downloadPath ?? null;
  const {
    url: recordingMediaURL,
    loading: recordingMediaLoading,
    error: recordingMediaError,
    reportReady: reportRecordingMediaReady,
    reportError: reportRecordingMediaError,
    retry: retryRecordingMedia,
  } = useProtectedMediaURL(recordingAudioDownloadPath);
  const {
    url: shadowingMediaURL,
    loading: shadowingMediaLoading,
    error: shadowingMediaError,
    reportReady: reportShadowingMediaReady,
    reportError: reportShadowingMediaError,
    retry: retryShadowingMedia,
  } = useProtectedMediaURL(
    shadowingDownloadPath,
    shadowingStatus === "ready",
  );
  const audioSrc = recordingAudioDownloadPath ? recordingMediaURL : localAudioSrc;
  const hasAudio = Boolean(audioSrc);
  const isProcessing = recording?.status === "processing";
  const isFailed = recording?.status === "failed";
  const failedUploadDraft = recordingId?.startsWith("local-") ? recordingSaveDrafts[recordingId] ?? null : null;
  const recordingDuration = recording?.duration ?? 0;
  const playbackPercent = recordingDuration > 0 ? Math.max(0, Math.min(100, (playbackPosition / recordingDuration) * 100)) : 0;
  const hasTranscript = recording ? recording.transcript.trim().length > 0 : false;
  const hasConversationTranscript = recording?.practiceType === "topic"
    && Boolean(recording.interviewTurns?.length);
  const hasCorrectedConversation = Boolean(
    recording?.practiceType === "topic"
    && recording.interviewTurns?.length
    && recording.interviewTurns.every((turn) => Boolean(turn.correctedAnswerText?.trim())),
  );
  const hasCorrectedTranscript = recording ? recording.correctedTranscript.trim().length > 0 : false;
  const hasSuggestions = recording ? recording.suggestions.length > 0 : false;
  const recordingStrengths = recording?.strengths ?? [];
  const hasStrengths = recordingStrengths.length > 0;
  const transcriptSegments = useMemo(() => {
    if (!recording) {
      return [];
    }

    return buildTranscriptSegments(recording.transcript, recording.suggestions, recording.strengths ?? []);
  }, [recording]);
  const isDeleteLoading = recordingDeleteStatus === "loading";
  const isRecordingRetryLoading = recordingId
    ? recordingRetryStatuses[recordingId] === "loading"
    : false;
  const recordingRetryError = recordingId ? recordingRetryErrors[recordingId] ?? null : null;
  const isShadowingRequestLoading = shadowingRequestStatus === "loading";
  const shadowingIsStale = isShadowingStale(shadowingStatus, shadowingUpdatedAt);
  const retryLabel = recordingRetryLabel(recording?.processingStage ?? null);
  const showShadowingProgress = shouldShowShadowingProgress({
    recordingStatus: recording?.status ?? "failed",
    correctedTranscript,
    shadowingStatus,
  });
  const canRetryShadowing =
    recordingStatus === "ready" &&
    hasCorrectedTranscript &&
    (shadowingStatus === "failed" || shadowingIsStale || Boolean(shadowingRequestError));
  const canDelete = Boolean(recordingId) && recordingId !== backgroundSaveRecordingId;

  useEffect(() => {
    if (
      !recordingId ||
      autoShadowingRequestedRef.current.has(recordingId) ||
      !shouldScheduleShadowing({
        recordingStatus: recordingStatus ?? "failed",
        correctedTranscript,
        shadowingStatus,
        requestLoading: isShadowingRequestLoading,
      })
    ) {
      return;
    }

    autoShadowingRequestedRef.current.add(recordingId);
    void dispatch(generateShadowingAudio(recordingId));
  }, [
    correctedTranscript,
    dispatch,
    isShadowingRequestLoading,
    recordingId,
    recordingStatus,
    shadowingStatus,
  ]);

  useEffect(() => {
    setPlaybackError(null);

    if (recordingAudioDownloadPath) {
      setLocalAudioSrc(null);
      return;
    }

    if (recordingAudioStorageKey) {
      let cancelled = false;
      let objectUrl: string | null = null;
      setLocalAudioSrc(null);
      void loadRecordingDraftAudio(recordingAudioStorageKey)
        .then((blob) => {
          if (cancelled) return;
          objectUrl = URL.createObjectURL(blob);
          setLocalAudioSrc(objectUrl);
        })
        .catch((error: unknown) => {
          if (cancelled) return;
          setPlaybackError(error instanceof Error ? error.message : "Recorded audio is no longer available.");
        });
      return () => {
        cancelled = true;
        if (objectUrl) URL.revokeObjectURL(objectUrl);
      };
    }

    setLocalAudioSrc(null);
  }, [recordingAudioDownloadPath, recordingAudioStorageKey, recordingId]);

  useEffect(() => {
    if (!recordingId) {
      return;
    }
    dispatch(resetPlaybackState());
  }, [dispatch, recordingId]);

  useEffect(() => {
    if (!recordingId || !hasAudio) {
      dispatch(setPlaybackPlaying(false));
      return;
    }

    const audio = audioRef.current;
    if (!audio) {
      return;
    }

    const handleTimeUpdate = () => {
      dispatch(setPlaybackPosition(Math.round(audio.currentTime)));
    };

    const handlePlay = () => {
      setPlaybackError(null);
      shadowingAudioRef.current?.pause();
      dispatch(setPlaybackPlaying(true));
    };

    const handlePause = () => {
      dispatch(setPlaybackPlaying(false));
    };

    const handleEnded = () => {
      dispatch(setPlaybackPlaying(false));
      dispatch(setPlaybackPosition(0));
      audio.currentTime = 0;
    };

    const handleCanPlay = () => {
      if (recordingAudioDownloadPath) {
        reportRecordingMediaReady();
      }
    };

    const handleError = () => {
      dispatch(setPlaybackPlaying(false));
      if (recordingAudioDownloadPath) {
        reportRecordingMediaError();
        return;
      }
      setPlaybackError("Cannot play this audio in your browser. Try recording again or use another browser.");
    };

    audio.addEventListener("timeupdate", handleTimeUpdate);
    audio.addEventListener("play", handlePlay);
    audio.addEventListener("pause", handlePause);
    audio.addEventListener("ended", handleEnded);
    audio.addEventListener("canplay", handleCanPlay);
    audio.addEventListener("error", handleError);

    return () => {
      audio.removeEventListener("timeupdate", handleTimeUpdate);
      audio.removeEventListener("play", handlePlay);
      audio.removeEventListener("pause", handlePause);
      audio.removeEventListener("ended", handleEnded);
      audio.removeEventListener("canplay", handleCanPlay);
      audio.removeEventListener("error", handleError);
    };
  }, [
    dispatch,
    hasAudio,
    recordingId,
    audioSrc,
    recordingAudioDownloadPath,
    reportRecordingMediaError,
    reportRecordingMediaReady,
  ]);

  const onTogglePlayback = () => {
    if (!recording || !hasAudio) {
      return;
    }

    const audio = audioRef.current;
    if (!audio) {
      dispatch(setPlaybackPlaying(false));
      return;
    }

    if (audio.paused) {
      setPlaybackError(null);
      void (async () => {
        try {
          if (audio.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
            await waitForAudioCanPlay(audio);
          }
          await audio.play();
        } catch (error) {
          dispatch(setPlaybackPlaying(false));
          setPlaybackError(resolvePlaybackStartError(error));
        }
      })();
      return;
    }

    audio.pause();
  };

  const scrollBehavior = (): ScrollBehavior =>
    window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth";

  const showReviewCard = (kind: ReviewKind, index: number) => {
    const id = feedbackCardId(kind, index);
    setSelectedReview(id);
    const card = document.getElementById(id);
    card?.scrollIntoView({ behavior: scrollBehavior(), block: "center" });
    card?.focus({ preventScroll: true });
  };

  const showTranscriptMark = (kind: ReviewKind, index: number) => {
    setOriginalTranscriptOpen(true);
    setSelectedReview(feedbackCardId(kind, index));
    window.requestAnimationFrame(() => {
      const mark = document.getElementById(transcriptMarkId(kind, index));
      mark?.scrollIntoView({ behavior: scrollBehavior(), block: "center" });
      mark?.focus({ preventScroll: true });
    });
  };

  const onSeek = (event: MouseEvent<HTMLDivElement>) => {
    if (!recording || !hasAudio) {
      return;
    }

    const bounds = event.currentTarget.getBoundingClientRect();
    const relative = (event.clientX - bounds.left) / bounds.width;
    const next = Math.round(relative * recordingDuration);
    dispatch(setPlaybackPosition(next));

    const audio = audioRef.current;
    if (audio) {
      audio.currentTime = next;
    }
  };

  const onOpenDeleteModal = () => {
    dispatch(clearRecordingDeleteError());
    setDeleteModalOpen(true);
  };

  const onCloseDeleteModal = () => {
    if (isDeleteLoading) {
      return;
    }
    dispatch(clearRecordingDeleteError());
    setDeleteModalOpen(false);
  };

  const onConfirmDelete = () => {
    if (!recordingId || !canDelete) {
      return;
    }
    void deleteAndNavigate(store, router, recordingId);
  };

  const onRetryShadowing = () => {
    if (!recording || isShadowingRequestLoading) {
      return;
    }
    void dispatch(generateShadowingAudio(recording.id)).unwrap().catch(() => undefined);
  };

  const onRetryRecordingProcessing = () => {
    if (!recording || isRecordingRetryLoading || !retryLabel) {
      return;
    }
    void dispatch(retryRecordingProcessing(recording.id)).unwrap().catch(() => undefined);
  };

  const onRetryRecordingUpload = () => {
    if (!failedUploadDraft || recordingSaveStatus === "loading") return;
    void saveAndNavigate(store, router, failedUploadDraft, () => window.location.pathname);
  };

  const renderProcessingRetry = (stage: "transcribing" | "suggestions" | "rewriting") => {
    if (!recording || !isFailed || recording.processingStage !== stage || !retryLabel) {
      return null;
    }

    return (
      <div className="processing-retry">
        <div className="auth-error">
          {recordingRetryError ?? recording.processingError ?? "Recording processing failed. Please try again."}
        </div>
        <button
          className="btn btn-secondary"
          onClick={onRetryRecordingProcessing}
          disabled={isRecordingRetryLoading}
        >
          {isRecordingRetryLoading ? "Retrying..." : retryLabel}
        </button>
      </div>
    );
  };

  const onDeleteModalKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      onCloseDeleteModal();
      return;
    }
    if (event.key !== "Tab") {
      return;
    }

    const focusable = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("button:not(:disabled)"));
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  if (!recording) {
    return (
      <section>
        <Link className="back-btn" href="/history">
          ← Back
        </Link>
        <h2>Recording</h2>
        {recordingError ? <RecordingLoadError recordingId={routeRecordingId} /> : <div className="empty-state" role="status">Loading recording...</div>}
      </section>
    );
  }

  const renderedTranscriptTargets = new Set<string>();
  const renderTranscriptSegment = (segment: (typeof transcriptSegments)[number], index: number) => {
    if (!segment.isError && !segment.isStrength) {
      return <span key={`segment-${index}`}>{segment.text}</span>;
    }
    const kind: ReviewKind = segment.isError ? "correction" : "strength";
    const feedbackIndex = segment.feedbackIndex ?? 0;
    const targetKey = `${kind}-${feedbackIndex}`;
    const firstOccurrence = !renderedTranscriptTargets.has(targetKey);
    renderedTranscriptTargets.add(targetKey);
    return (
      <mark
        key={`segment-${index}`}
        id={firstOccurrence ? transcriptMarkId(kind, feedbackIndex) : undefined}
        data-feedback-kind={kind}
        data-feedback-index={feedbackIndex}
        className={segment.isError
          ? `transcript-error-mark${segment.severity ? ` transcript-error-mark-${segment.severity}` : ""}`
          : "transcript-strength-mark"}
        role="button"
        tabIndex={0}
        aria-controls={feedbackCardId(kind, feedbackIndex)}
        onClick={() => showReviewCard(kind, feedbackIndex)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            showReviewCard(kind, feedbackIndex);
          }
        }}
      >
        {segment.text}
      </mark>
    );
  };

  return (
    <section>
      <Link className="back-btn" href="/history">
        ← Back
      </Link>
      <h2>Recording</h2>
      <RecordingLoadError recordingId={routeRecordingId} />

      {isProcessing && (
        <div className="notice">
          {recordingProcessingLabel(recording.processingStage)} You can leave this page and come back later.
          <div className="background-progress" aria-hidden="true">
            <div className="background-progress-fill" />
          </div>
        </div>
      )}
      {isFailed && failedUploadDraft && (
        <div className="processing-retry">
          <div className="auth-error">
            {recordingSaveError ?? recording.processingError ?? "The recording could not be uploaded."}
          </div>
          <button className="btn btn-secondary" onClick={onRetryRecordingUpload} disabled={recordingSaveStatus === "loading"}>
            {recordingSaveStatus === "loading" ? "Uploading..." : "Retry upload"}
          </button>
          <div className="hint">This unsaved audio is stored in this browser until the upload succeeds.</div>
        </div>
      )}
      {isFailed && !failedUploadDraft && !retryLabel && (
        <div className="auth-error">{recording.processingError ?? "Recording processing failed. Try recording again."}</div>
      )}

      {(recording.media?.photo || recording.localPhotoDataUrl) && (
        <div className="details-photo-card">
          <ProtectedMediaImage
            downloadPath={recording.media?.photo?.downloadPath ?? null}
            localURL={recording.localPhotoDataUrl}
            alt="Photo from speaking practice"
            className="details-photo"
          />
          {recording.photoObject && <div className="details-photo-caption">Object: {recording.photoObject}</div>}
        </div>
      )}

      <div className="details-metadata">
        <div>
          <strong>{formatTime(recordingDuration)}</strong>
        </div>
        <div>{recording.topic}</div>
        <div>{formatPracticeLabel(recording.practiceType)}</div>
      </div>

      <div className="details-workbench">
        <div className="details-materials">
          <section className="details-material-card details-original-material">
            <div className="material-heading">
              <div>
                <div className="material-kicker">Your recording</div>
                <h3>Original conversation</h3>
              </div>
            </div>
            {recordingMediaLoading ? (
              <div className="notice">Preparing protected recording audio...</div>
            ) : hasAudio ? (
              <audio ref={audioRef} src={audioSrc ?? undefined} preload="metadata" />
            ) : (
              <div className="notice">Audio is unavailable for this recording.</div>
            )}
            <div className="player material-player">
              <div className="player-controls">
                <button className="play-btn" onClick={onTogglePlayback} disabled={!hasAudio} aria-label={isPlaying ? "Pause" : "Play"}>
                  {isPlaying ? "Pause" : "Play"}
                </button>
                <div className={`progress-bar ${hasAudio ? "" : "disabled-progress"}`} onClick={onSeek}>
                  <div className="progress-bar-fill" style={{ width: `${playbackPercent}%` }} />
                </div>
                <div className="time-display">{formatTime(playbackPosition)} / {formatTime(recordingDuration)}</div>
              </div>
            </div>
            {playbackError && <div className="auth-error top-spaced">{playbackError}</div>}
            {recordingMediaError && (
              <div className="processing-retry top-spaced">
                <div className="auth-error">{recordingMediaError}</div>
                <button className="btn btn-secondary" onClick={retryRecordingMedia}>Reload audio</button>
              </div>
            )}
            <button
              className="transcript-disclosure"
              type="button"
              aria-expanded={originalTranscriptOpen}
              aria-controls="original-transcript-panel"
              onClick={() => setOriginalTranscriptOpen((open) => !open)}
            >
              <span>{hasConversationTranscript ? "Conversation transcription" : "Transcription"}</span>
              <span aria-hidden="true">{originalTranscriptOpen ? "−" : "+"}</span>
            </button>
            {originalTranscriptOpen && (
              <div id="original-transcript-panel" className="material-transcript-scroll">
                {hasConversationTranscript ? (
                  <ConversationTranscript
                    turns={recording.interviewTurns}
                    suggestions={recording.suggestions}
                    strengths={recordingStrengths}
                    processing={isProcessing}
                    onReviewSelect={showReviewCard}
                  />
                ) : hasTranscript ? (
                  <div className="transcript-text">{transcriptSegments.map(renderTranscriptSegment)}</div>
                ) : isProcessing ? (
                  <div className="empty-state">{recording.processingStage === "transcribing" ? "Transcribing audio. The transcript will appear here automatically." : "The transcript will appear here automatically."}</div>
                ) : (
                  <div className="empty-state">Transcript is unavailable for this recording.</div>
                )}
                {renderProcessingRetry("transcribing")}
              </div>
            )}
          </section>

          <section className="details-material-card details-shadowing-material">
            <div className="material-heading">
              <div>
                <div className="material-kicker">Practice version</div>
                <h3>Shadowing practice</h3>
              </div>
            </div>
            <p className="shadowing-hint">Listen, then repeat with the same rhythm and pronunciation.</p>
            {recording.shadowingStatus === "ready" && shadowingMediaLoading && <div className="empty-state">Preparing protected pronunciation audio...</div>}
            {recording.shadowingStatus === "ready" && shadowingMediaURL && (
              <audio
                ref={shadowingAudioRef}
                className="shadowing-audio"
                controls
                preload="metadata"
                src={shadowingMediaURL}
                onPlay={() => {
                  audioRef.current?.pause();
                  dispatch(setPlaybackPlaying(false));
                }}
                onCanPlay={reportShadowingMediaReady}
                onError={reportShadowingMediaError}
              />
            )}
            {recording.shadowingStatus === "ready" && !shadowingDownloadPath && <div className="auth-error">The protected pronunciation audio reference is unavailable.</div>}
            {recording.shadowingStatus === "ready" && shadowingMediaError && (
              <div className="processing-retry">
                <div className="auth-error">{shadowingMediaError}</div>
                <button className="btn btn-secondary" onClick={retryShadowingMedia}>Reload audio</button>
              </div>
            )}
            {showShadowingProgress && !shadowingRequestError && (
              <div className={shadowingIsStale ? "auth-error" : "empty-state"}>{shadowingProgressLabel(recording.shadowingStatus, shadowingIsStale)}</div>
            )}
            {recordingStatus === "ready" && hasCorrectedTranscript && (recording.shadowingStatus === "failed" || shadowingRequestError) && (
              <div className="auth-error">{shadowingRequestError ?? recording.shadowingError ?? "Pronunciation audio could not be generated. Please try again."}</div>
            )}
            {canRetryShadowing && (
              <div className="shadowing-actions">
                <button className="btn btn-secondary" onClick={onRetryShadowing} disabled={isShadowingRequestLoading}>
                  {isShadowingRequestLoading ? "Retrying..." : "Retry"}
                </button>
              </div>
            )}
            <button
              className="transcript-disclosure"
              type="button"
              aria-expanded={correctedTranscriptOpen}
              aria-controls="corrected-transcript-panel"
              onClick={() => setCorrectedTranscriptOpen((open) => !open)}
            >
              <span>Corrected transcription</span>
              <span aria-hidden="true">{correctedTranscriptOpen ? "−" : "+"}</span>
            </button>
            <div id="corrected-transcript-panel" className="material-transcript-scroll" hidden={!correctedTranscriptOpen}>
              {hasCorrectedConversation ? (
                <ConversationTranscript turns={recording.interviewTurns} suggestions={[]} processing={isProcessing} answerKind="corrected" />
              ) : hasCorrectedTranscript ? (
                <div className="transcript-text">{recording.correctedTranscript}</div>
              ) : isProcessing ? (
                <div className="empty-state">{recording.processingStage === "rewriting" ? "Creating a natural conversational version for your English level." : "This version will appear after the transcript and feedback are ready."}</div>
              ) : isFailed ? (
                <div className="empty-state">The natural version is unavailable, but completed results are still saved.</div>
              ) : (
                <div className="empty-state">The natural version is unavailable for this recording.</div>
              )}
              {renderProcessingRetry("rewriting")}
            </div>
          </section>
        </div>

        <section className="details-feedback">
          <div className="feedback-heading">
            <div className="material-kicker">Your feedback</div>
            <h3>Corrections and strengths</h3>
            <p>Choose a highlight in the transcript to open its explanation.</p>
          </div>
          {hasStrengths && (
            <div className="feedback-group">
              <div className="feedback-group-title">What you did well</div>
              {recordingStrengths.map((strength, index) => {
                const id = feedbackCardId("strength", index);
                return <StrengthCard key={`${strength.excerpt}-${index}`} id={id} active={selectedReview === id} strength={strength} onShowInTranscript={() => showTranscriptMark("strength", index)} />;
              })}
            </div>
          )}
          <div className="feedback-group">
            <div className="feedback-group-title">Corrections</div>
            {hasSuggestions ? recording.suggestions.map((suggestion, index) => {
              const id = feedbackCardId("correction", index);
              return (
                <SuggestionCard
                  key={`${suggestion.wrong}-${suggestion.right}-${suggestion.category ?? "uncategorized"}-${index}`}
                  id={id}
                  active={selectedReview === id}
                  suggestion={suggestion}
                  onShowInTranscript={() => showTranscriptMark("correction", index)}
                />
              );
            }) : isProcessing && recording.processingStage !== "rewriting" ? (
              <div className="empty-state">AI feedback will appear here after transcription.</div>
            ) : isFailed && recording.processingStage !== "rewriting" ? (
              <div className="empty-state">AI feedback is unavailable for this recording.</div>
            ) : (
              <div className="empty-state">No clear corrections were needed.</div>
            )}
          </div>
          {renderProcessingRetry("suggestions")}
        </section>
      </div>

      <button className="btn btn-danger btn-large delete-recording-btn" onClick={onOpenDeleteModal} disabled={!canDelete || isDeleteLoading}>
        {!canDelete ? "Saving recording..." : isDeleteLoading ? "Deleting..." : "Delete recording"}
      </button>

      {deleteModalOpen && (
        <div
          className="modal visible"
          role="presentation"
          onKeyDown={onDeleteModalKeyDown}
          onClick={(event) => {
            if (event.target === event.currentTarget) {
              onCloseDeleteModal();
            }
          }}
        >
          <div className="modal-content" role="dialog" aria-modal="true" aria-label="Delete recording">
            <div className="modal-title">Delete recording?</div>
            <p>
              This permanently deletes the recording and its audio files.
            </p>
            {isProcessing && <p className="auth-hint">The background analysis for this recording will be stopped.</p>}
            {recordingDeleteError && <div className="auth-error top-spaced">{recordingDeleteError}</div>}
            <div className="modal-buttons top-spaced">
              <button className="btn btn-secondary" onClick={onCloseDeleteModal} disabled={isDeleteLoading} autoFocus>
                Cancel
              </button>
              <button className="btn btn-danger" onClick={onConfirmDelete} disabled={isDeleteLoading}>
                {isDeleteLoading ? "Deleting..." : "Delete permanently"}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
