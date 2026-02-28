"use client";

import { useEffect, useMemo, type MouseEvent } from "react";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  backToHistory,
  openShareModal,
  setPlaybackPosition,
  tickPlayback,
  togglePlayback
} from "../store/slices/appSlice";
import ShareModal from "./ShareModal";

export default function DetailsScreen() {
  const dispatch = useAppDispatch();
  const { recordings, currentRecordingId, isPlaying, playbackPosition, copyMessage } = useAppSelector(
    (state) => state.app
  );

  const recording = useMemo(
    () => recordings.find((item) => item.id === currentRecordingId),
    [currentRecordingId, recordings]
  );

  useEffect(() => {
    if (!isPlaying || !recording) {
      return;
    }

    const timer = window.setInterval(() => {
      dispatch(tickPlayback());
    }, 1000);

    return () => window.clearInterval(timer);
  }, [dispatch, isPlaying, recording]);

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

  const playbackPercent = recording.duration > 0 ? (playbackPosition / recording.duration) * 100 : 0;
  const hasTranscript = recording.transcript.trim().length > 0;
  const hasSuggestions = recording.suggestions.length > 0;

  const onSeek = (event: MouseEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const relative = (event.clientX - bounds.left) / bounds.width;
    const next = Math.round(relative * recording.duration);
    dispatch(setPlaybackPosition(next));
  };

  return (
    <section>
      <button className="back-btn" onClick={() => dispatch(backToHistory())}>
        ← Back
      </button>
      <h2>Recording</h2>

      {copyMessage && <div className="notice">{copyMessage}</div>}

      <div className="details-metadata">
        <div>
          <strong>{formatTime(recording.duration)}</strong>
        </div>
        <div>{recording.topic}</div>
      </div>

      <div className="player">
        <div className="player-controls">
          <button className="play-btn" onClick={() => dispatch(togglePlayback())}>
            {isPlaying ? "⏸" : "▶"}
          </button>
          <div className="progress-bar" onClick={onSeek}>
            <div className="progress-bar-fill" style={{ width: `${playbackPercent}%` }} />
          </div>
          <div className="time-display">
            {formatTime(playbackPosition)} / {formatTime(recording.duration)}
          </div>
        </div>
      </div>

      <div className="transcript-section">
        <div className="section-title">Transcript</div>
        {hasTranscript ? (
          <div className="transcript-text">{recording.transcript}</div>
        ) : (
          <div className="empty-state">Transcript is in progress and will be available soon.</div>
        )}
      </div>

      <div className="suggestions-section">
        <div className="section-title">AI Suggestions</div>
        {hasSuggestions ? (
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
          <div className="empty-state">AI error analysis is in progress and will be available soon.</div>
        )}
      </div>

      <button className="btn btn-primary btn-large" onClick={() => dispatch(openShareModal())}>
        Share recording
      </button>

      <ShareModal />
    </section>
  );
}
