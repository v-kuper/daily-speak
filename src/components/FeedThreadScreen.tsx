"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import { backToFeed, createFeedReply, fetchFeedThread, reactToFeedPost, reactToFeedReply } from "../store/slices/appSlice";
import FeedReactionBar from "./FeedReactionBar";

type ReplyRecordState = "idle" | "recording" | "recorded";

const AUDIO_MIME_CANDIDATES = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];

const readBlobAsDataUrl = (blob: Blob): Promise<string> => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = typeof reader.result === "string" ? reader.result : "";
      if (!result) {
        reject(new Error("Failed to read recorded audio."));
        return;
      }
      resolve(result);
    };
    reader.onerror = () => reject(new Error("Failed to read recorded audio."));
    reader.readAsDataURL(blob);
  });
};

const resolvePreferredAudioMimeType = (): string | null => {
  if (typeof MediaRecorder === "undefined" || typeof MediaRecorder.isTypeSupported !== "function") {
    return null;
  }

  for (const candidate of AUDIO_MIME_CANDIDATES) {
    if (MediaRecorder.isTypeSupported(candidate)) {
      return candidate;
    }
  }

  return null;
};

const resolveMicrophoneError = (error: unknown): string => {
  if (error && typeof error === "object" && "name" in error && typeof (error as { name?: unknown }).name === "string") {
    const name = (error as { name: string }).name;
    if (name === "NotAllowedError" || name === "PermissionDeniedError") {
      return "Microphone access denied. Allow microphone permission in browser settings.";
    }
    if (name === "NotFoundError" || name === "DevicesNotFoundError") {
      return "No microphone found on this device.";
    }
    if (name === "NotReadableError" || name === "TrackStartError") {
      return "Microphone is busy in another app. Close it and try again.";
    }
  }

  return "Cannot access microphone. Check browser permissions and device settings.";
};

export default function FeedThreadScreen() {
  const dispatch = useAppDispatch();
  const {
    currentFeedPostId,
    currentFeedPost,
    currentFeedReplies,
    feedThreadStatus,
    feedThreadError,
    feedReplyStatus,
    feedReplyError,
    feedReactionStatus,
    feedReactionError,
    isSubscriber,
    weeklyRemainingSeconds,
    maxSessionSeconds
  } = useAppSelector((state) => state.app);

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);
  const mediaChunksRef = useRef<Blob[]>([]);

  const [replyState, setReplyState] = useState<ReplyRecordState>("idle");
  const [replyDuration, setReplyDuration] = useState(0);
  const [replyAudioDataUrl, setReplyAudioDataUrl] = useState<string | null>(null);
  const [replyError, setReplyError] = useState<string | null>(null);
  const [showPostTranscript, setShowPostTranscript] = useState(false);

  const normalizedMaxSessionSeconds = Math.max(0, maxSessionSeconds);
  const normalizedWeeklyRemainingSeconds = Math.max(0, weeklyRemainingSeconds ?? 0);
  const sessionLimitSeconds = isSubscriber
    ? normalizedMaxSessionSeconds
    : Math.min(normalizedMaxSessionSeconds, normalizedWeeklyRemainingSeconds);
  const hasReplyBudget = sessionLimitSeconds > 0;

  const releaseMedia = useCallback(() => {
    if (mediaStreamRef.current) {
      for (const track of mediaStreamRef.current.getTracks()) {
        track.stop();
      }
      mediaStreamRef.current = null;
    }
    mediaRecorderRef.current = null;
    mediaChunksRef.current = [];
  }, []);

  const stopRecordingInternal = useCallback(() => {
    const recorder = mediaRecorderRef.current;
    if (recorder && recorder.state !== "inactive") {
      recorder.stop();
      return;
    }
    releaseMedia();
  }, [releaseMedia]);

  useEffect(() => {
    if (!currentFeedPostId) {
      return;
    }
    void dispatch(fetchFeedThread(currentFeedPostId));
  }, [currentFeedPostId, dispatch]);

  useEffect(() => {
    if (replyState !== "recording") {
      return;
    }

    const timer = window.setInterval(() => {
      setReplyDuration((prev) => {
        if (sessionLimitSeconds <= 0) {
          stopRecordingInternal();
          setReplyState("recorded");
          return prev;
        }
        const next = prev + 1;
        if (next >= sessionLimitSeconds) {
          stopRecordingInternal();
          setReplyState("recorded");
          return sessionLimitSeconds;
        }
        return next;
      });
    }, 1000);

    return () => window.clearInterval(timer);
  }, [replyState, sessionLimitSeconds, stopRecordingInternal]);

  useEffect(() => {
    return () => {
      releaseMedia();
    };
  }, [releaseMedia]);

  const onStartReplyRecording = () => {
    setReplyError(null);
    if (!hasReplyBudget) {
      setReplyError("You reached your current recording limit.");
      return;
    }

    if (typeof window === "undefined" || typeof navigator === "undefined") {
      setReplyError("Recording is available only in browser.");
      return;
    }

    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") {
      setReplyError("Your browser does not support microphone recording.");
      return;
    }

    void (async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        const mimeType = resolvePreferredAudioMimeType();
        const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);

        mediaStreamRef.current = stream;
        mediaRecorderRef.current = recorder;
        mediaChunksRef.current = [];
        setReplyDuration(0);
        setReplyAudioDataUrl(null);
        setReplyState("recording");

        recorder.ondataavailable = (event: BlobEvent) => {
          if (event.data && event.data.size > 0) {
            mediaChunksRef.current.push(event.data);
          }
        };

        recorder.onerror = () => {
          setReplyError("Recording failed. Please try again.");
        };

        recorder.onstop = () => {
          const chunks = [...mediaChunksRef.current];
          const resultingType = recorder.mimeType || "audio/webm";
          releaseMedia();

          if (chunks.length === 0) {
            setReplyError("No audio captured. Try recording again.");
            return;
          }

          const blob = new Blob(chunks, { type: resultingType });
          void readBlobAsDataUrl(blob)
            .then((dataUrl) => {
              setReplyAudioDataUrl(dataUrl);
            })
            .catch(() => {
              setReplyError("Failed to process recorded audio.");
            });
        };

        recorder.start();
      } catch (error) {
        releaseMedia();
        setReplyError(resolveMicrophoneError(error));
      }
    })();
  };

  const onStopReplyRecording = () => {
    setReplyState("recorded");
    stopRecordingInternal();
  };

  const onResetReply = () => {
    setReplyState("idle");
    setReplyDuration(0);
    setReplyAudioDataUrl(null);
    setReplyError(null);
    releaseMedia();
  };

  const onSendReply = () => {
    if (!currentFeedPost || !replyAudioDataUrl || !replyDuration) {
      setReplyError("Record your voice reply first.");
      return;
    }

    setReplyError(null);
    void dispatch(
      createFeedReply({
        postId: currentFeedPost.id,
        duration: replyDuration,
        audioDataUrl: replyAudioDataUrl
      })
    )
      .unwrap()
      .then(() => {
        onResetReply();
      })
      .catch(() => undefined);
  };

  return (
    <section>
      <button className="back-btn" onClick={() => dispatch(backToFeed())}>
        ← Back
      </button>
      <h2>Comments</h2>

      {feedThreadError && <div className="auth-error top-spaced">{feedThreadError}</div>}
      {feedThreadStatus === "loading" && !currentFeedPost && <div className="empty-state">Loading thread...</div>}

      {currentFeedPost && (
        <div className="feed-card feed-thread-post">
          <div className="feed-card-topic">{currentFeedPost.topic}</div>
          <div className="player feed-player">
            {currentFeedPost.audioDataUrl ? (
              <audio controls preload="metadata" src={currentFeedPost.audioDataUrl} className="feed-audio" />
            ) : (
              <div className="empty-state">Audio is unavailable for this post.</div>
            )}
            <div className="recording-duration">{formatTime(currentFeedPost.duration)}</div>
          </div>
          <div className="feed-post-actions">
            <button className="btn btn-secondary btn-small" onClick={() => setShowPostTranscript((prev) => !prev)}>
              {showPostTranscript ? "Hide text" : "Show text"}
            </button>
          </div>
          <FeedReactionBar
            reactions={currentFeedPost.reactions}
            disabled={feedReactionStatus === "loading"}
            onReact={(reaction) => {
              void dispatch(reactToFeedPost({ postId: currentFeedPost.id, reaction }));
            }}
          />
          {showPostTranscript && (
            <div className="feed-transcript-accordion">
              <div className="section-title">Transcript</div>
              <div className="transcript-text">{currentFeedPost.transcript || "Transcript is unavailable."}</div>
            </div>
          )}
        </div>
      )}

      <div className="feed-replies-section">
        <div className="section-title">Voice Replies</div>

        {currentFeedReplies.length === 0 ? (
          <div className="empty-state">No replies yet. Be the first to reply by voice.</div>
        ) : (
          currentFeedReplies.map((reply) => (
            <div key={reply.id} className="feed-reply-card">
              <div className="feed-reply-header">
                <div className="feed-card-author">{reply.authorMaskedEmail}</div>
                <div className="recording-duration">{formatTime(reply.duration)}</div>
              </div>
              {reply.audioDataUrl ? (
                <audio controls preload="metadata" src={reply.audioDataUrl} className="feed-audio" />
              ) : (
                <div className="empty-state">Audio is unavailable for this reply.</div>
              )}
              <FeedReactionBar
                reactions={reply.reactions}
                disabled={feedReactionStatus === "loading"}
                onReact={(reaction) => {
                  void dispatch(reactToFeedReply({ replyId: reply.id, reaction }));
                }}
              />
            </div>
          ))
        )}
      </div>

      <div className="feed-reply-composer">
        <div className="section-title">Your Voice Reply</div>
        {!hasReplyBudget && (
          <div className="notice">
            You reached your available limit for this week. Reply recording will unlock after quota resets.
          </div>
        )}

        {replyState === "recording" && <div className="recording-indicator">Recording...</div>}
        {replyState !== "idle" && <div className="timer">{formatTime(replyDuration)}</div>}
        {replyAudioDataUrl && <audio controls preload="metadata" src={replyAudioDataUrl} className="feed-audio" />}

        <div className="btn-group">
          {replyState === "recording" ? (
            <button className="btn btn-secondary" onClick={onStopReplyRecording}>
              Stop
            </button>
          ) : (
            <button className="btn btn-secondary" onClick={onStartReplyRecording} disabled={!hasReplyBudget || feedReplyStatus === "loading"}>
              {replyState === "recorded" ? "Re-record" : "Start recording"}
            </button>
          )}

          <button
            className="btn btn-primary"
            onClick={onSendReply}
            disabled={!replyAudioDataUrl || !replyDuration || feedReplyStatus === "loading"}
          >
            {feedReplyStatus === "loading" ? "Sending..." : "Send voice reply"}
          </button>
        </div>

        {(replyError || feedReplyError || feedReactionError) && (
          <div className="auth-error top-spaced">{replyError ?? feedReplyError ?? feedReactionError}</div>
        )}
      </div>
    </section>
  );
}
