"use client";

import { useEffect } from "react";
import { getTopicData } from "../lib/data";
import { formatTime } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  refreshTopics,
  reRecord,
  saveRecording,
  selectTopic,
  setCustomTopicDraft,
  startFreeTalk,
  startRecording,
  stopRecording,
  tickRecording,
  toggleAddTopicInput,
  toggleQuestions,
  toggleWords,
  useCustomTopic
} from "../store/slices/appSlice";

export default function SpeakScreen() {
  const dispatch = useAppDispatch();
  const {
    speakState,
    selectedTopic,
    showQuestions,
    showWords,
    recordingDuration,
    topics,
    showAddTopicInput,
    customTopicDraft,
    isAuthenticated
  } = useAppSelector((state) => state.app);

  useEffect(() => {
    if (speakState !== "recording") {
      return;
    }

    const timer = window.setInterval(() => {
      dispatch(tickRecording());
    }, 1000);

    return () => window.clearInterval(timer);
  }, [dispatch, speakState]);

  if (speakState === "idle") {
    return (
      <section>
        <button className="btn btn-primary btn-large" onClick={() => dispatch(startFreeTalk())}>
          Start speaking
        </button>

        <div className="section">
          <div className="section-title">Today&apos;s topics</div>
          <div className="topics-grid">
            {topics.map((topic) => (
              <button key={topic} className="topic-btn" onClick={() => dispatch(selectTopic(topic))}>
                {topic}
              </button>
            ))}
          </div>
          <button className="btn btn-secondary btn-small" onClick={() => dispatch(refreshTopics())}>
            ↻ Refresh topics
          </button>
        </div>

        <div className="section">
          <button className="btn btn-secondary btn-small" onClick={() => dispatch(toggleAddTopicInput())}>
            + Add your own topic
          </button>

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
                <button className="btn btn-primary" onClick={() => dispatch(useCustomTopic())}>
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
    const topicData = getTopicData(selectedTopic ?? "");

    return (
      <section>
        <div className="heading-sm">Selected topic</div>
        <h2 className="heading-xl">{selectedTopic}</h2>

        <button className="btn btn-primary btn-large" onClick={() => dispatch(startRecording())}>
          Start speaking
        </button>

        {topicData.questions.length > 0 && (
          <div className="collapsible-section">
            <button className="collapsible-header" onClick={() => dispatch(toggleQuestions())}>
              <span>Show questions</span>
              <span className={`toggle-arrow ${showQuestions ? "open" : ""}`}>↓</span>
            </button>
            <div className={`collapsible-content ${showQuestions ? "open" : ""}`}>
              {topicData.questions.map((question) => (
                <div key={question} className="question-item">
                  {question}
                </div>
              ))}
            </div>
          </div>
        )}

        {topicData.words.length > 0 && (
          <div className="collapsible-section">
            <button className="collapsible-header" onClick={() => dispatch(toggleWords())}>
              <span>Useful words (optional)</span>
              <span className={`toggle-arrow ${showWords ? "open" : ""}`}>↓</span>
            </button>
            <div className={`collapsible-content ${showWords ? "open" : ""}`}>
              {topicData.words.map((word) => (
                <div key={word} className="word-item">
                  {word}
                </div>
              ))}
            </div>
          </div>
        )}
      </section>
    );
  }

  if (speakState === "recording") {
    const topicData = selectedTopic ? getTopicData(selectedTopic) : { questions: [], words: [] };
    const shouldShowQuestions = showQuestions && topicData.questions.length > 0;

    return (
      <section>
        <div className="recording-indicator">
          <div className="recording-dot" />
          <span>{selectedTopic ?? "Free talk"}</span>
        </div>

        <div className="timer">{formatTime(recordingDuration)}</div>

        {shouldShowQuestions && (
          <div className="section">
            <div className="section-title">Questions</div>
            <div className="section-content">
              {topicData.questions.map((question) => (
                <div key={question} className="question-item">
                  {question}
                </div>
              ))}
            </div>
          </div>
        )}

        <button className="btn btn-primary btn-large" onClick={() => dispatch(stopRecording())}>
          Stop
        </button>
      </section>
    );
  }

  return (
    <section>
      <div className="recorded-banner">
        <div className="recorded-title">Recording complete</div>
        <div className="recorded-subtitle">Duration: {formatTime(recordingDuration)}</div>
      </div>

      {!isAuthenticated && (
        <div className="notice">Saving is available only for authorized users. Sign in to continue.</div>
      )}

      <div className="btn-group">
        <button className="btn btn-secondary" onClick={() => dispatch(reRecord())}>
          Re-record
        </button>
        <button className="btn btn-primary" onClick={() => dispatch(saveRecording())}>
          {isAuthenticated ? "Save and continue" : "Sign in to save"}
        </button>
      </div>
    </section>
  );
}
