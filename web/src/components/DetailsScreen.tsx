"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { deleteAndNavigate, reconcileRecordingSaveRoute, recordingDetailState, saveAndNavigate, startRecordingDetailLifecycle } from "../lib/routeFlows";

import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
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
  requestRecordingFeedback,
  setPlaybackPlaying,
  setPlaybackPosition,
} from "../store/slices/appSlice";
import RecordingLoadError from "./RecordingLoadError";
import ProtectedMediaImage from "./ProtectedMediaImage";
import ConversationTranscript from "./ConversationTranscript";
import FocusedFeedbackReview, { FeedbackLegend } from "./FocusedFeedbackReview";
import InterviewRetakes from "./InterviewRetakes";

const formatPracticeLabel = (value: "free_talk" | "topic" | "photo_description"): string => {
  switch (value) {
    case "free_talk":
      return "Свободная практика";
    case "photo_description":
      return "Описание фото";
    default:
      return "Интервью";
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
  const [correctedTranscriptOpen, setCorrectedTranscriptOpen] = useState(true);
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
    shadowingRequestError: globalShadowingError,
    shadowingRequestRecordingId,
    recordingFeedbackStatuses,
    recordingFeedbackErrors,
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
  const shadowingAudioReady = Boolean(recording?.focusedFeedback) && shadowingStatus === "ready";
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
    shadowingAudioReady,
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
  const isDeleteLoading = recordingDeleteStatus === "loading";
  const isRecordingRetryLoading = recordingId
    ? recordingRetryStatuses[recordingId] === "loading"
    : false;
  const recordingRetryError = recordingId ? recordingRetryErrors[recordingId] ?? null : null;
  const shadowingRequestError = shadowingRequestRecordingId === recordingId ? globalShadowingError : null;
  const isShadowingRequestLoading = shadowingRequestRecordingId === recordingId && shadowingRequestStatus === "loading";
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
  const isFeedbackRequestLoading = Boolean(recordingId && recordingFeedbackStatuses[recordingId] === "loading");
  const feedbackRequestError = recordingId ? recordingFeedbackErrors[recordingId] : null;
  const canDelete = Boolean(recordingId) && recordingId !== backgroundSaveRecordingId;

  useEffect(() => {
    if (
      !recordingId ||
      !recording?.focusedFeedback ||
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
    recording?.focusedFeedback,
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

  const recordingTitle = recording.practiceType === "topic"
    ? recording.interviewTurns?.[0]?.question || recording.topic
    : recording.topic;
  const recordedAt = new Date(recording.timestamp);
  const recordingDate = Number.isFinite(recordedAt.getTime())
    ? recordedAt.toLocaleString("ru", { day: "numeric", month: "long", year: "numeric", hour: "2-digit", minute: "2-digit" })
    : null;

  return (
    <section>
      <Link className="back-btn" href="/history">
        ← Back
      </Link>
      <header className="details-heading">
        <div className="material-kicker">{formatPracticeLabel(recording.practiceType)}</div>
        <h2>{recordingTitle}</h2>
        <div className="details-metadata" aria-label="Информация о записи">
          {recordingDate && <time dateTime={recording.timestamp}>{recordingDate}</time>}
          <span>Длительность <strong>{formatTime(recordingDuration)}</strong></span>
          {Boolean(recording.interviewTurns?.length) && <span>Вопросов: <strong>{recording.interviewTurns.length}</strong></span>}
        </div>
      </header>
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

      {recording.focusedFeedback && <aside className="details-review-guide" aria-label="Как читать разбор">
        <div className="material-kicker">Как читать разбор</div>
        <FeedbackLegend explain />
        <p>Подсвечиваем все уверенно найденные ошибки. Нажмите на цветной фрагмент или плашку, чтобы открыть правило и потренироваться на примере.</p>
      </aside>}

      <div className="details-workbench">
        <div className="details-materials">
          <section className="details-material-card details-original-material">
            <div className="material-heading">
              <div>
                <div className="material-kicker">Исходное аудио</div>
                <h3>Ваша запись</h3>
              </div>
            </div>
            {recordingMediaLoading ? (
              <div className="notice">Preparing protected recording audio...</div>
            ) : hasAudio ? (
              <audio ref={audioRef} src={audioSrc ?? undefined} preload="metadata" />
            ) : null}
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
                {recording.focusedFeedback ? (
                  <FocusedFeedbackReview feedback={recording.focusedFeedback} transcript={recording.transcript} turns={hasConversationTranscript ? recording.interviewTurns : []} audioBase={`/api/v1/recordings/${recording.id}/feedback`} showLegend={false} />
                ) : hasConversationTranscript ? (
                  <ConversationTranscript
                    turns={recording.interviewTurns}
                    processing={isProcessing}
                  />
                ) : hasTranscript ? (
                  <div className="transcript-text">{recording.transcript}</div>
                ) : isProcessing ? (
                  <div className="empty-state">{recording.processingStage === "transcribing" ? "Transcribing audio. The transcript will appear here automatically." : "The transcript will appear here automatically."}</div>
                ) : (
                  <div className="empty-state">Transcript is unavailable for this recording.</div>
                )}
                {renderProcessingRetry("transcribing")}
              </div>
            )}
            {!recording.focusedFeedback && !isProcessing && !isFailed && (
              <div className="feedback-empty" aria-label="Разбор ответа">
                <div>
                  <h4>Разбор ещё не готов</h4>
                  <p>Разберём ошибки, объясним правила и подготовим примеры для практики.</p>
                </div>
                {!recording.id.startsWith("local-") && hasTranscript && <button
                  type="button"
                  className="practice-action-button"
                  disabled={isFeedbackRequestLoading}
                  onClick={() => { void dispatch(requestRecordingFeedback(recording.id)); }}
                >{isFeedbackRequestLoading ? "Запускаем разбор…" : feedbackRequestError ? "Повторить разбор" : "Сделать разбор"}</button>}
                {feedbackRequestError && <p className="auth-error" role="alert">{feedbackRequestError}</p>}
              </div>
            )}
            {renderProcessingRetry("suggestions")}
          </section>

          {(recording.focusedFeedback || isProcessing) && <section className="details-material-card details-shadowing-material">
            <div className="material-heading">
              <div>
                <div className="material-kicker">Исправленная версия</div>
                <h3>Shadowing practice</h3>
              </div>
            </div>
            <p className="shadowing-hint">Ваши мысли и детали в естественном английском: исправляем грамматику и построение фраз с учётом вашего уровня. Слушайте и повторяйте, следуя ритму и произношению.</p>
            {shadowingAudioReady && shadowingMediaLoading && <div className="empty-state">Preparing protected pronunciation audio...</div>}
            {shadowingAudioReady && shadowingMediaURL && (
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
            {shadowingAudioReady && !shadowingDownloadPath && <div className="auth-error">The protected pronunciation audio reference is unavailable.</div>}
            {shadowingAudioReady && shadowingMediaError && (
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
              <span>Ваш ответ в естественной форме</span>
              <span aria-hidden="true">{correctedTranscriptOpen ? "−" : "+"}</span>
            </button>
            <div id="corrected-transcript-panel" className="material-transcript-scroll" hidden={!correctedTranscriptOpen}>
              {hasCorrectedConversation ? (
                <ConversationTranscript turns={recording.interviewTurns} processing={isProcessing} answerKind="corrected" />
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
          </section>}
          {recording.focusedFeedback && recording.status === "ready" && !recording.id.startsWith("local-") && recording.interviewTurns?.some(turn => turn.answerText.trim()) && <InterviewRetakes key={recording.id} recording={recording} onOpen={() => {
            audioRef.current?.pause();
            shadowingAudioRef.current?.pause();
            dispatch(setPlaybackPlaying(false));
          }} />}
        </div>

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
