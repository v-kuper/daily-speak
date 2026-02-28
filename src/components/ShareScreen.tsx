"use client";

import { useMemo } from "react";
import { buildTranscriptSegments } from "../lib/transcriptHighlight";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import { backToHistory } from "../store/slices/appSlice";

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

export default function ShareScreen() {
  const dispatch = useAppDispatch();
  const { recordings, currentRecordingId } = useAppSelector((state) => state.app);

  const recording = useMemo(
    () => recordings.find((item) => item.id === currentRecordingId),
    [currentRecordingId, recordings]
  );
  const hasTranscript = recording?.transcript.trim().length ? true : false;
  const hasSuggestions = recording?.suggestions.length ? true : false;
  const transcriptSegments = useMemo(() => {
    if (!recording) {
      return [];
    }

    return buildTranscriptSegments(recording.transcript, recording.suggestions.map((item) => item.wrong));
  }, [recording]);

  if (!recording) {
    return (
      <section>
        <button className="back-btn" onClick={() => dispatch(backToHistory())}>
          ← Back
        </button>
        <h2>Shared Recording</h2>
        <div className="empty-state">Recording not found.</div>
      </section>
    );
  }

  return (
    <section>
      <button className="back-btn" onClick={() => dispatch(backToHistory())}>
        ← Back
      </button>
      <h2>Shared Recording</h2>

      {recording.photoDataUrl && (
        <div className="details-photo-card">
          <img src={recording.photoDataUrl} alt="Photo from shared recording" className="details-photo" />
          {recording.photoObject && <div className="details-photo-caption">Object: {recording.photoObject}</div>}
        </div>
      )}

      <div className="details-metadata">
        <div>
          <strong>{formatTime(recording.duration)}</strong>
        </div>
        <div>{recording.topic}</div>
        <div>{formatPracticeLabel(recording.practiceType)}</div>
      </div>

      <div className="player">
        <div className="player-controls">
          <button className="play-btn" disabled>
            ▶
          </button>
          <div className="progress-bar disabled-progress">
            <div className="progress-bar-fill" style={{ width: "0%" }} />
          </div>
          <div className="time-display">0:00 / {formatTime(recording.duration)}</div>
        </div>
      </div>

      <div className="transcript-section">
        <div className="section-title">Transcript</div>
        {hasTranscript ? (
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
    </section>
  );
}
