"use client";

import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { buildTranscriptSegments } from "../lib/transcriptHighlight";
import {
  recordingProcessingLabel,
  recordingRetryLabel,
  shouldShowShadowingProgress,
} from "../lib/recordingProcessing";
import {
  isShadowingStale,
  shadowingProgressLabel,
  shouldPollRecording,
  shouldScheduleShadowing,
} from "../lib/shadowing";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  backToHistory,
  clearRecordingDeleteError,
  deleteRecording,
  fetchRecording,
  generateShadowingAudio,
  resetPlaybackState,
  retryRecordingProcessing,
  setPlaybackPlaying,
  setPlaybackPosition,
} from "../store/slices/appSlice";
import SuggestionCard from "./SuggestionCard";

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

const createAudioObjectUrl = (audioDataUrl: string): string | null => {
  if (!audioDataUrl.startsWith("data:audio/") && !audioDataUrl.startsWith("data:video/")) {
    return null;
  }

  const commaIndex = audioDataUrl.indexOf(",");
  if (commaIndex <= 0) {
    return null;
  }

  const metadata = audioDataUrl.slice(5, commaIndex);
  const payload = audioDataUrl.slice(commaIndex + 1);
  if (!metadata.toLowerCase().endsWith(";base64") || !payload) {
    return null;
  }

  try {
    const mimeType = metadata.slice(0, -";base64".length) || "audio/webm";
    const binary = window.atob(payload);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index);
    }
    return URL.createObjectURL(new Blob([bytes], { type: mimeType }));
  } catch {
    return null;
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

export default function DetailsScreen() {
  const dispatch = useAppDispatch();
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const autoShadowingRequestedRef = useRef(new Set<string>());
  const [audioSrc, setAudioSrc] = useState<string | null>(null);
  const [playbackError, setPlaybackError] = useState<string | null>(null);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const {
    recordings,
    currentRecordingId,
    isPlaying,
    playbackPosition,
    backgroundSaveRecordingId,
    recordingDeleteStatus,
    recordingDeleteError,
    recordingRetryStatuses,
    recordingRetryErrors,
    shadowingRequestStatus,
    shadowingRequestError,
  } = useAppSelector(
    (state) => state.app
  );

  const recording = useMemo(
    () => recordings.find((item) => item.id === currentRecordingId),
    [currentRecordingId, recordings]
  );
  const recordingId = recording?.id ?? null;
  const recordingAudioDataUrl = recording?.audioDataUrl ?? null;
  const recordingStatus = recording?.status;
  const correctedTranscript = recording?.correctedTranscript ?? "";
  const shadowingStatus = recording?.shadowingStatus ?? "pending";
  const shadowingUpdatedAt = recording?.shadowingUpdatedAt ?? "";
  const hasAudio = Boolean(audioSrc);
  const isProcessing = recording?.status === "processing";
  const isFailed = recording?.status === "failed";
  const recordingDuration = recording?.duration ?? 0;
  const playbackPercent = recordingDuration > 0 ? Math.max(0, Math.min(100, (playbackPosition / recordingDuration) * 100)) : 0;
  const hasTranscript = recording ? recording.transcript.trim().length > 0 : false;
  const hasCorrectedTranscript = recording ? recording.correctedTranscript.trim().length > 0 : false;
  const hasSuggestions = recording ? recording.suggestions.length > 0 : false;
  const transcriptSegments = useMemo(() => {
    if (!recording) {
      return [];
    }

    return buildTranscriptSegments(recording.transcript, recording.suggestions);
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
    if (!recordingId || !shouldPollRecording(recordingStatus ?? "", shadowingStatus)) {
      return;
    }

    const interval = window.setInterval(() => {
      void dispatch(fetchRecording(recordingId));
    }, 3000);

    void dispatch(fetchRecording(recordingId));

    return () => {
      window.clearInterval(interval);
    };
  }, [dispatch, recordingId, recordingStatus, shadowingStatus]);

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

    if (!recordingAudioDataUrl) {
      setAudioSrc(null);
      return;
    }

    const objectUrl = createAudioObjectUrl(recordingAudioDataUrl);
    if (!objectUrl) {
      setAudioSrc(recordingAudioDataUrl);
      return;
    }

    setAudioSrc(objectUrl);
    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [recordingAudioDataUrl, recordingId]);

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

    const handleError = () => {
      dispatch(setPlaybackPlaying(false));
      setPlaybackError("Cannot play this audio in your browser. Try recording again or use another browser.");
    };

    audio.addEventListener("timeupdate", handleTimeUpdate);
    audio.addEventListener("play", handlePlay);
    audio.addEventListener("pause", handlePause);
    audio.addEventListener("ended", handleEnded);
    audio.addEventListener("error", handleError);

    return () => {
      audio.removeEventListener("timeupdate", handleTimeUpdate);
      audio.removeEventListener("play", handlePlay);
      audio.removeEventListener("pause", handlePause);
      audio.removeEventListener("ended", handleEnded);
      audio.removeEventListener("error", handleError);
    };
  }, [dispatch, hasAudio, recordingId, audioSrc]);

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
    void dispatch(deleteRecording(recordingId)).unwrap().catch(() => undefined);
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
        <button className="back-btn" onClick={() => dispatch(backToHistory())}>
          ← Back
        </button>
        <h2>Recording</h2>
        <div className="empty-state">Recording not found.</div>
      </section>
    );
  }

  return (
    <section>
      <button className="back-btn" onClick={() => dispatch(backToHistory())}>
        ← Back
      </button>
      <h2>Recording</h2>

      {isProcessing && (
        <div className="notice">
          {recordingProcessingLabel(recording.processingStage)} You can leave this page and come back later.
          <div className="background-progress" aria-hidden="true">
            <div className="background-progress-fill" />
          </div>
        </div>
      )}
      {isFailed && !retryLabel && (
        <div className="auth-error">{recording.processingError ?? "Recording processing failed. Try recording again."}</div>
      )}

      {recording.photoDataUrl && (
        <div className="details-photo-card">
          <img src={recording.photoDataUrl} alt="Photo from speaking practice" className="details-photo" />
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

      {hasAudio ? (
        <audio ref={audioRef} src={audioSrc ?? undefined} preload="metadata" />
      ) : (
        <div className="notice">Audio is unavailable for this recording.</div>
      )}
      {playbackError && <div className="auth-error top-spaced">{playbackError}</div>}

      <div className="player">
        <div className="player-controls">
          <button className="play-btn" onClick={onTogglePlayback} disabled={!hasAudio} aria-label={isPlaying ? "Pause" : "Play"}>
            {isPlaying ? "Pause" : "Play"}
          </button>
          <div className={`progress-bar ${hasAudio ? "" : "disabled-progress"}`} onClick={onSeek}>
            <div className="progress-bar-fill" style={{ width: `${playbackPercent}%` }} />
          </div>
          <div className="time-display">
            {formatTime(playbackPosition)} / {formatTime(recordingDuration)}
          </div>
        </div>
      </div>

      <div className="transcript-section">
        <div className="section-title">Transcript</div>
        {hasTranscript ? (
          <div className="transcript-text">
            {transcriptSegments.map((segment, index) =>
              segment.isError ? (
                <mark
                  key={`segment-${index}`}
                  className={`transcript-error-mark${segment.severity ? ` transcript-error-mark-${segment.severity}` : ""}`}
                >
                  {segment.text}
                </mark>
              ) : (
                <span key={`segment-${index}`}>{segment.text}</span>
              )
            )}
          </div>
        ) : isProcessing ? (
          <div className="empty-state">
            {recording.processingStage === "transcribing"
              ? "Transcribing audio. The transcript will appear here automatically."
              : "The transcript will appear here automatically."}
          </div>
        ) : (
          <div className="empty-state">Transcript is unavailable for this recording.</div>
        )}
        {renderProcessingRetry("transcribing")}
      </div>

      <div className="suggestions-section">
        <div className="section-title">AI Suggestions</div>
        {hasSuggestions ? (
          recording.suggestions.map((suggestion, index) => (
            <SuggestionCard
              key={`${suggestion.wrong}-${suggestion.right}-${suggestion.category ?? "legacy"}-${index}`}
              suggestion={suggestion}
            />
          ))
        ) : isProcessing && recording.processingStage !== "rewriting" ? (
          <div className="empty-state">
            AI error analysis will appear here after transcription.
          </div>
        ) : isFailed && recording.processingStage !== "rewriting" ? (
          <div className="empty-state">AI suggestions are unavailable for this recording.</div>
        ) : (
          <div className="empty-state">No clear corrections were needed.</div>
        )}
        {renderProcessingRetry("suggestions")}
      </div>

      <div className="shadowing-section">
        <div className="section-title">Shadowing practice</div>
        <p className="shadowing-hint">Listen, then repeat with the same rhythm and pronunciation.</p>
        {hasCorrectedTranscript ? (
          <div className="transcript-text">{recording.correctedTranscript}</div>
        ) : isProcessing ? (
          <div className="empty-state">
            {recording.processingStage === "rewriting"
              ? "Creating a natural conversational version for your English level."
              : "This version will appear after the transcript and AI suggestions are ready."}
          </div>
        ) : isFailed ? (
          <div className="empty-state">The natural version is unavailable, but completed results above are still saved.</div>
        ) : (
          <div className="empty-state">The natural version is unavailable for this recording.</div>
        )}
        {renderProcessingRetry("rewriting")}
        {recording.shadowingStatus === "ready" && recording.shadowingAudioUrl && (
          <audio
            className="shadowing-audio"
            controls
            preload="metadata"
            src={recording.shadowingAudioUrl}
          />
        )}
        {showShadowingProgress && !shadowingRequestError && (
            <div className={shadowingIsStale ? "auth-error" : "empty-state"}>
              {shadowingProgressLabel(recording.shadowingStatus, shadowingIsStale)}
            </div>
          )}
        {recordingStatus === "ready" && hasCorrectedTranscript &&
          (recording.shadowingStatus === "failed" || shadowingRequestError) && (
          <div className="auth-error">
            {shadowingRequestError ??
              recording.shadowingError ??
              "Pronunciation audio could not be generated. Please try again."}
          </div>
          )}
        {canRetryShadowing && (
          <div className="shadowing-actions">
            <button
              className="btn btn-secondary"
              onClick={onRetryShadowing}
              disabled={isShadowingRequestLoading}
            >
              {isShadowingRequestLoading ? "Retrying..." : "Retry"}
            </button>
          </div>
        )}
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
