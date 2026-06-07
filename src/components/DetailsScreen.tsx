"use client";

import { useEffect, useMemo, useRef, useState, type MouseEvent } from "react";
import { buildTranscriptSegments } from "../lib/transcriptHighlight";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  backToHistory,
  fetchRecording,
  fetchFeedPosts,
  openShareModal,
  resetPlaybackState,
  setPlaybackPlaying,
  setPlaybackPosition,
} from "../store/slices/appSlice";
import AudioWaveform from "./AudioWaveform";
import ShareModal from "./ShareModal";

type FeedThreadReply = {
  id: string;
  duration: number;
  audioDataUrl: string | null;
  authorMaskedEmail: string;
};

type FeedThreadResponse = {
  replies?: unknown;
  error?: string;
};

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
  const [audioSrc, setAudioSrc] = useState<string | null>(null);
  const [playbackError, setPlaybackError] = useState<string | null>(null);
  const [sharedReplies, setSharedReplies] = useState<FeedThreadReply[]>([]);
  const [sharedRepliesStatus, setSharedRepliesStatus] = useState<"idle" | "loading" | "ready" | "failed">("idle");
  const [sharedRepliesError, setSharedRepliesError] = useState<string | null>(null);
  const {
    recordings,
    currentRecordingId,
    isPlaying,
    playbackPosition,
    copyMessage,
    feedPosts,
    feedPostsStatus
  } = useAppSelector(
    (state) => state.app
  );

  const recording = useMemo(
    () => recordings.find((item) => item.id === currentRecordingId),
    [currentRecordingId, recordings]
  );
  const recordingId = recording?.id ?? null;
  const hasAudio = Boolean(audioSrc);
  const isProcessing = recording?.status === "processing";
  const isFailed = recording?.status === "failed";
  const recordingDuration = recording?.duration ?? 0;
  const playbackPercent = recordingDuration > 0 ? Math.max(0, Math.min(100, (playbackPosition / recordingDuration) * 100)) : 0;
  const hasTranscript = recording ? recording.transcript.trim().length > 0 : false;
  const hasSuggestions = recording ? recording.suggestions.length > 0 : false;
  const transcriptSegments = useMemo(() => {
    if (!recording) {
      return [];
    }

    return buildTranscriptSegments(recording.transcript, recording.suggestions.map((item) => item.wrong));
  }, [recording]);
  const sharedFeedPost = useMemo(() => {
    if (!recording) {
      return null;
    }

    return feedPosts.find((item) => item.sourceRecordingId === recording.id) ?? null;
  }, [feedPosts, recording]);
  const sharedFeedPostId = sharedFeedPost?.id ?? null;
  const isShareStatusLoading = Boolean(recording) && (feedPostsStatus === "idle" || feedPostsStatus === "loading");

  useEffect(() => {
    if (!recordingId || feedPostsStatus !== "idle") {
      return;
    }

    void dispatch(fetchFeedPosts());
  }, [dispatch, feedPostsStatus, recordingId]);

  useEffect(() => {
    if (!recordingId || recording?.status !== "processing") {
      return;
    }

    const interval = window.setInterval(() => {
      void dispatch(fetchRecording(recordingId));
    }, 3000);

    void dispatch(fetchRecording(recordingId));

    return () => {
      window.clearInterval(interval);
    };
  }, [dispatch, recording?.status, recordingId]);

  useEffect(() => {
    if (!sharedFeedPostId) {
      setSharedReplies([]);
      setSharedRepliesStatus("idle");
      setSharedRepliesError(null);
      return;
    }

    let cancelled = false;
    setSharedRepliesStatus("loading");
    setSharedRepliesError(null);

    void fetch(`/api/feed/posts/${encodeURIComponent(sharedFeedPostId)}`, { cache: "no-store" })
      .then(async (response) => {
        const payload = (await response.json().catch(() => null)) as FeedThreadResponse | null;

        if (!response.ok) {
          throw new Error(payload?.error ?? "Failed to load comments.");
        }

        const repliesRaw = Array.isArray(payload?.replies) ? payload?.replies : [];
        const replies = repliesRaw
          .map((item) => {
            if (typeof item !== "object" || item === null) {
              return null;
            }
            const candidate = item as Record<string, unknown>;
            const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
            const authorMaskedEmail = typeof candidate.authorMaskedEmail === "string" ? candidate.authorMaskedEmail.trim() : "";
            const duration = Number.parseInt(String(candidate.duration ?? 0), 10);
            const audioDataUrlRaw = candidate.audioDataUrl;
            const audioDataUrl = typeof audioDataUrlRaw === "string" && audioDataUrlRaw.trim() ? audioDataUrlRaw.trim() : null;

            if (!id || !authorMaskedEmail || !Number.isFinite(duration) || duration < 0) {
              return null;
            }

            return {
              id,
              duration: Math.max(0, duration),
              audioDataUrl,
              authorMaskedEmail
            } as FeedThreadReply;
          })
          .filter((item): item is FeedThreadReply => item !== null);

        if (cancelled) {
          return;
        }

        setSharedReplies(replies);
        setSharedRepliesStatus("ready");
      })
      .catch((error) => {
        if (cancelled) {
          return;
        }

        setSharedReplies([]);
        setSharedRepliesStatus("failed");
        setSharedRepliesError(error instanceof Error ? error.message : "Failed to load comments.");
      });

    return () => {
      cancelled = true;
    };
  }, [sharedFeedPostId]);

  useEffect(() => {
    setPlaybackError(null);

    if (!recording?.audioDataUrl) {
      setAudioSrc(null);
      return;
    }

    const objectUrl = createAudioObjectUrl(recording.audioDataUrl);
    if (!objectUrl) {
      setAudioSrc(recording.audioDataUrl);
      return;
    }

    setAudioSrc(objectUrl);
    return () => {
      URL.revokeObjectURL(objectUrl);
    };
  }, [recording?.audioDataUrl, recording?.id]);

  useEffect(() => {
    if (!recording) {
      return;
    }
    dispatch(resetPlaybackState());
  }, [dispatch, recording?.id]);

  useEffect(() => {
    if (!recording || !hasAudio) {
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
  }, [dispatch, hasAudio, recording?.id, audioSrc]);

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

      {copyMessage && <div className="notice">{copyMessage}</div>}
      {isProcessing && <div className="notice">Processing recording in the background. You can come back later.</div>}
      {isFailed && (
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

      <div className="player studio-player">
        <AudioWaveform variant="compact" active={isPlaying} />
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
        {isFailed ? (
          <div className="empty-state">{recording?.processingError ?? "Recording processing failed. Try recording again."}</div>
        ) : hasTranscript ? (
          <div className="transcript-text">
            {transcriptSegments.map((segment, index) =>
              segment.isError ? (
                <mark key={`segment-${index}`} className="transcript-error-mark">
                  {segment.text}
                </mark>
              ) : (
                <span key={`segment-${index}`}>{segment.text}</span>
              )
            )}
          </div>
        ) : (
          <div className="empty-state">
            {isProcessing ? "Processing recording. Transcript will appear here automatically." : "Transcript is unavailable."}
          </div>
        )}
      </div>

      <div className="suggestions-section">
        <div className="section-title">AI Suggestions</div>
        {isFailed ? (
          <div className="empty-state">AI suggestions are unavailable for this recording.</div>
        ) : hasSuggestions ? (
          recording.suggestions.map((suggestion) => (
            <div key={suggestion.wrong} className="suggestion-item">
              <div className="suggestion-wrong">
                <span className="suggestion-wrong-icon">❌</span>
                <span>{suggestion.wrong}</span>
              </div>
              <div className="suggestion-right">
                <span className="suggestion-right-icon">✅</span>
                <span>{suggestion.right}</span>
              </div>
              <div className="suggestion-explanation">{suggestion.explanation}</div>
            </div>
          ))
        ) : (
          <div className="empty-state">
            {isProcessing ? "AI error analysis is running in the background." : "AI suggestions are unavailable."}
          </div>
        )}
      </div>

      {sharedFeedPost ? (
        <div className="top-spaced">
          <div className="notice">Already shared to Feed.</div>
          <div className="section-title">People Comments</div>
          {sharedRepliesError && <div className="auth-error top-spaced">{sharedRepliesError}</div>}
          {sharedRepliesStatus === "loading" && <div className="empty-state">Loading comments...</div>}
          {sharedRepliesStatus !== "loading" && sharedReplies.length === 0 && (
            <div className="empty-state">No comments yet.</div>
          )}
          {sharedReplies.map((reply) => (
            <div key={reply.id} className="feed-reply-card">
              <div className="feed-reply-header">
                <div className="feed-card-author">{reply.authorMaskedEmail}</div>
                <div className="recording-duration">{formatTime(reply.duration)}</div>
              </div>
              {reply.audioDataUrl ? (
                <audio controls preload="metadata" src={reply.audioDataUrl} className="feed-audio" />
              ) : (
                <div className="empty-state">Audio is unavailable for this comment.</div>
              )}
            </div>
          ))}
        </div>
      ) : isShareStatusLoading ? (
        <div className="empty-state">Checking share status...</div>
      ) : (
        <>
          <button className="btn btn-primary btn-large" onClick={() => dispatch(openShareModal())}>
            Publish to Feed
          </button>
          <ShareModal />
        </>
      )}
    </section>
  );
}
