"use client";

import { useEffect } from "react";
import { formatTime, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  clearQuestionsError,
  clearTopicGuidanceError,
  fetchDailyQuestions,
  fetchTopicGuidance,
  openAuthForSave,
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
    isAuthenticated,
    questionsStatus,
    questionsError,
    selectedInterestIds,
    topicGuidanceQuestions,
    topicGuidanceWords,
    topicGuidanceStatus,
    topicGuidanceError,
    recordingSaveStatus,
    recordingSaveError
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

  useEffect(() => {
    const dateKey = toDateKey(new Date());
    void dispatch(fetchDailyQuestions({ dateKey, interestIds: selectedInterestIds }));
  }, [dispatch, selectedInterestIds]);

  useEffect(() => {
    if (!selectedTopic) {
      return;
    }
    void dispatch(fetchTopicGuidance({ topic: selectedTopic, interestIds: selectedInterestIds }));
  }, [dispatch, selectedTopic, selectedInterestIds]);

  const onRefreshQuestions = () => {
    const dateKey = toDateKey(new Date());
    dispatch(clearQuestionsError());
    void dispatch(
      fetchDailyQuestions({
        dateKey,
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds
      })
    );
  };

  const onRefreshTopicGuidance = () => {
    if (!selectedTopic) {
      return;
    }
    dispatch(clearTopicGuidanceError());
    void dispatch(
      fetchTopicGuidance({
        topic: selectedTopic,
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds
      })
    );
  };

  if (speakState === "idle") {
    const shouldShowQuestionsSkeleton = questionsStatus === "loading" && topics.length === 0;

    return (
      <section>
        <button className="btn btn-primary btn-large" onClick={() => dispatch(startFreeTalk())}>
          Start speaking
        </button>

        <div className="section">
          <div className="section-title">Today&apos;s questions</div>
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
            <div className="empty-state">No daily questions yet.</div>
          ) : (
            <div className="topics-grid">
              {topics.map((topic) => (
                <button key={topic} className="topic-btn" onClick={() => dispatch(selectTopic(topic))}>
                  {topic}
                </button>
              ))}
            </div>
          )}
          <button
            className="btn btn-secondary btn-small"
            onClick={onRefreshQuestions}
            disabled={questionsStatus === "loading"}
          >
            {questionsStatus === "loading" ? "Generating..." : "↻ Regenerate questions"}
          </button>
          {questionsError && <div className="auth-error top-spaced">{questionsError}</div>}
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
    const shouldShowQuestions = showQuestions && topicGuidanceQuestions.length > 0;
    const shouldShowWords = showWords && topicGuidanceWords.length > 0;
    const shouldShowGuidanceSkeleton =
      topicGuidanceStatus === "loading" && topicGuidanceQuestions.length === 0 && topicGuidanceWords.length === 0;

    return (
      <section>
        <div className="heading-sm">Selected question</div>
        <h2 className="heading-xl">{selectedTopic}</h2>

        <button className="btn btn-primary btn-large" onClick={() => dispatch(startRecording())}>
          Start speaking
        </button>

        {shouldShowGuidanceSkeleton && (
          <div className="guidance-skeleton" aria-hidden="true">
            <div className="guidance-skeleton-title skeleton-line skeleton-line-short" />
            <div className="guidance-skeleton-item skeleton-line skeleton-line-wide" />
            <div className="guidance-skeleton-item skeleton-line skeleton-line-wide" />
            <div className="guidance-skeleton-item skeleton-line skeleton-line-medium" />
          </div>
        )}

        {topicGuidanceQuestions.length > 0 && (
          <div className="collapsible-section">
            <button className="collapsible-header" onClick={() => dispatch(toggleQuestions())}>
              <span>Follow-up questions</span>
              <span className={`toggle-arrow ${showQuestions ? "open" : ""}`}>↓</span>
            </button>
            <div className={`collapsible-content ${shouldShowQuestions ? "open" : ""}`}>
              {topicGuidanceQuestions.map((question) => (
                <div key={question} className="question-item">
                  {question}
                </div>
              ))}
            </div>
          </div>
        )}

        {topicGuidanceWords.length > 0 && (
          <div className="collapsible-section">
            <button className="collapsible-header" onClick={() => dispatch(toggleWords())}>
              <span>Useful words</span>
              <span className={`toggle-arrow ${showWords ? "open" : ""}`}>↓</span>
            </button>
            <div className={`collapsible-content ${shouldShowWords ? "open" : ""}`}>
              {topicGuidanceWords.map((word) => (
                <div key={word} className="word-item">
                  {word}
                </div>
              ))}
            </div>
          </div>
        )}

        <button
          className="btn btn-secondary btn-small"
          onClick={onRefreshTopicGuidance}
          disabled={topicGuidanceStatus === "loading"}
        >
          {topicGuidanceStatus === "loading" ? "Generating..." : "↻ Regenerate questions and words"}
        </button>

        {topicGuidanceError && <div className="auth-error top-spaced">{topicGuidanceError}</div>}
      </section>
    );
  }

  if (speakState === "recording") {
    const shouldShowQuestions = showQuestions && topicGuidanceQuestions.length > 0;

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
              {topicGuidanceQuestions.map((question) => (
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
        <button
          className="btn btn-primary"
          onClick={() => {
            if (!isAuthenticated) {
              dispatch(openAuthForSave());
              return;
            }
            void dispatch(saveRecording());
          }}
          disabled={recordingSaveStatus === "loading"}
        >
          {isAuthenticated
            ? recordingSaveStatus === "loading"
              ? "Saving..."
              : "Save and continue"
            : "Sign in to save"}
        </button>
      </div>
      {recordingSaveError && <div className="auth-error top-spaced">{recordingSaveError}</div>}
    </section>
  );
}
