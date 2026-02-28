"use client";

import { useEffect, type ChangeEvent } from "react";
import { formatTime, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  clearPhotoForPractice,
  clearQuestionsError,
  clearTopicGuidanceError,
  fetchDailyQuestions,
  fetchTopicGuidance,
  openAuthForSave,
  PHOTO_PRACTICE_MAX_BYTES,
  reRecord,
  saveRecording,
  selectTopic,
  setCustomTopicDraft,
  setPhotoForPractice,
  setPhotoObjectDraft,
  setPhotoUploadError,
  startFreeTalk,
  startPhotoDescription,
  startRecording,
  stopRecording,
  tickRecording,
  toggleAddTopicInput,
  toggleQuestions,
  toggleWords,
  useCustomTopic
} from "../store/slices/appSlice";

const PHOTO_ACCEPTED_TYPES = new Set(["image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif"]);

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
    selectedEnglishLevel,
    topicGuidanceQuestions,
    topicGuidanceWords,
    topicGuidanceStatus,
    topicGuidanceError,
    recordingSaveStatus,
    recordingSaveError,
    isSubscriber,
    weeklyLimitSeconds,
    weeklyUsedSeconds,
    weeklyRemainingSeconds,
    maxSessionSeconds,
    recordingPracticeType,
    pendingPhotoDataUrl,
    pendingPhotoObjectDraft,
    pendingPhotoError
  } = useAppSelector((state) => state.app);

  const normalizedMaxSessionSeconds = Math.max(0, maxSessionSeconds);
  const normalizedWeeklyLimitSeconds = Math.max(0, weeklyLimitSeconds ?? 0);
  const normalizedWeeklyUsedSeconds = Math.max(0, weeklyUsedSeconds);
  const normalizedWeeklyRemainingSeconds = Math.max(0, weeklyRemainingSeconds ?? 0);
  const sessionLimitSeconds = isSubscriber
    ? normalizedMaxSessionSeconds
    : Math.min(normalizedMaxSessionSeconds, normalizedWeeklyRemainingSeconds);
  const freeLimitReached = isAuthenticated && !isSubscriber && sessionLimitSeconds <= 0;
  const hasRecordingBudget = !isAuthenticated || sessionLimitSeconds > 0;

  const quotaHint = isAuthenticated
    ? isSubscriber
      ? `Subscriber: unlimited per week, up to ${formatTime(normalizedMaxSessionSeconds)} per recording.`
      : `Free: ${formatTime(normalizedWeeklyRemainingSeconds)} left this week (${formatTime(
          normalizedWeeklyUsedSeconds
        )} of ${formatTime(normalizedWeeklyLimitSeconds)} used).`
    : null;

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
    void dispatch(fetchDailyQuestions({ dateKey, interestIds: selectedInterestIds, englishLevel: selectedEnglishLevel }));
  }, [dispatch, selectedEnglishLevel, selectedInterestIds]);

  useEffect(() => {
    if (!selectedTopic || recordingPracticeType === "photo_description") {
      return;
    }

    void dispatch(
      fetchTopicGuidance({ topic: selectedTopic, interestIds: selectedInterestIds, englishLevel: selectedEnglishLevel })
    );
  }, [dispatch, recordingPracticeType, selectedEnglishLevel, selectedTopic, selectedInterestIds]);

  const onRefreshQuestions = () => {
    const dateKey = toDateKey(new Date());
    dispatch(clearQuestionsError());
    void dispatch(
      fetchDailyQuestions({
        dateKey,
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds,
        avoidQuestions: topics,
        englishLevel: selectedEnglishLevel
      })
    );
  };

  const onRefreshTopicGuidance = () => {
    if (!selectedTopic || recordingPracticeType === "photo_description") {
      return;
    }
    dispatch(clearTopicGuidanceError());
    void dispatch(
      fetchTopicGuidance({
        topic: selectedTopic,
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds,
        avoidQuestions: topicGuidanceQuestions,
        avoidWords: topicGuidanceWords,
        englishLevel: selectedEnglishLevel
      })
    );
  };

  const onPhotoSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.currentTarget.value = "";

    if (!file) {
      return;
    }

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
        dispatch(setPhotoForPractice(dataUrl));
      })
      .catch(() => {
        dispatch(setPhotoUploadError("Failed to read selected photo."));
      });
  };

  if (speakState === "idle") {
    const shouldShowQuestionsSkeleton = questionsStatus === "loading" && topics.length === 0;

    return (
      <section className="speak-screen">
        <div className="speak-card speak-hero-card">
          <div className="heading-sm">Daily practice</div>
          <h2 className="heading-xl speak-heading-tight">Start a new speaking session</h2>
          {quotaHint && <div className="notice">{quotaHint}</div>}
          {freeLimitReached && (
            <div className="auth-error">Free weekly limit reached. New quota will be available next week.</div>
          )}
          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={() => dispatch(startFreeTalk())}
            disabled={!hasRecordingBudget}
          >
            Start speaking
          </button>
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Today&apos;s questions</div>
            <button
              className="btn btn-secondary btn-small"
              onClick={onRefreshQuestions}
              disabled={questionsStatus === "loading"}
            >
              {questionsStatus === "loading" ? "Generating..." : "↻ Regenerate"}
            </button>
          </div>

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
            <div className="empty-state speak-empty-state">No daily questions yet.</div>
          ) : (
            <div className="topics-grid">
              {topics.map((topic) => (
                <button key={topic} className="topic-btn" onClick={() => dispatch(selectTopic(topic))}>
                  {topic}
                </button>
              ))}
            </div>
          )}

          {questionsError && <div className="auth-error top-spaced">{questionsError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Photo description</div>
            {pendingPhotoDataUrl && (
              <button className="btn btn-secondary btn-small" onClick={() => dispatch(clearPhotoForPractice())}>
                Remove photo
              </button>
            )}
          </div>

          <div className="profile-value">Upload an image and practice describing what you see.</div>

          <label className="btn btn-secondary btn-small photo-upload-btn">
            Upload photo
            <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" onChange={onPhotoSelected} />
          </label>

          {pendingPhotoDataUrl ? (
            <img src={pendingPhotoDataUrl} alt="Selected for speaking practice" className="photo-practice-preview" />
          ) : (
            <div className="empty-state speak-empty-state">No photo selected yet.</div>
          )}

          <div className="photo-object-input">
            <input
              type="text"
              placeholder="Optional object name (example: red bicycle)"
              value={pendingPhotoObjectDraft}
              onChange={(event) => dispatch(setPhotoObjectDraft(event.target.value))}
            />
          </div>

          <button
            className="btn btn-primary"
            onClick={() => dispatch(startPhotoDescription())}
            disabled={!pendingPhotoDataUrl || !hasRecordingBudget}
          >
            Start photo session
          </button>

          {pendingPhotoError && <div className="auth-error top-spaced">{pendingPhotoError}</div>}
        </div>

        <div className="speak-card">
          <div className="speak-section-header">
            <div className="section-title speak-section-title">Custom topic</div>
            <button className="btn btn-secondary btn-small" onClick={() => dispatch(toggleAddTopicInput())}>
              {showAddTopicInput ? "Hide" : "+ Add topic"}
            </button>
          </div>

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
    const isPhotoPractice = recordingPracticeType === "photo_description";
    const shouldShowQuestions = !isPhotoPractice && showQuestions && topicGuidanceQuestions.length > 0;
    const shouldShowWords = !isPhotoPractice && showWords && topicGuidanceWords.length > 0;
    const shouldShowGuidanceSkeleton =
      !isPhotoPractice && topicGuidanceStatus === "loading" && topicGuidanceQuestions.length === 0 && topicGuidanceWords.length === 0;

    return (
      <section className="speak-screen">
        <div className="speak-card speak-hero-card">
          <div className="heading-sm">Selected question</div>
          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo to describe" className="photo-practice-preview" />
          )}
          <h2 className="heading-xl speak-heading-tight">{selectedTopic}</h2>

          {quotaHint && <div className="notice">{quotaHint}</div>}
          {freeLimitReached && (
            <div className="auth-error">Free weekly limit reached. New quota will be available next week.</div>
          )}

          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={() => dispatch(startRecording())}
            disabled={!hasRecordingBudget || (isPhotoPractice && !pendingPhotoDataUrl)}
          >
            Start speaking
          </button>
          {pendingPhotoError && <div className="auth-error top-spaced">{pendingPhotoError}</div>}
        </div>

        {isPhotoPractice ? (
          <div className="speak-card">
            <div className="section-title speak-section-title">Photo focus</div>
            <div className="question-item">Describe the object and what details you notice.</div>
            <div className="question-item">Mention color, shape, material, and where it is located.</div>
            <div className="question-item">Say how this object could be used in real life.</div>
          </div>
        ) : (
          <div className="speak-card">
            <div className="speak-section-header">
              <div className="section-title speak-section-title">Topic guidance</div>
              <button
                className="btn btn-secondary btn-small"
                onClick={onRefreshTopicGuidance}
                disabled={topicGuidanceStatus === "loading"}
              >
                {topicGuidanceStatus === "loading" ? "Generating..." : "↻ Regenerate"}
              </button>
            </div>

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

            {topicGuidanceError && <div className="auth-error top-spaced">{topicGuidanceError}</div>}
          </div>
        )}
      </section>
    );
  }

  if (speakState === "recording") {
    const isPhotoPractice = recordingPracticeType === "photo_description";
    const shouldShowQuestions = !isPhotoPractice && showQuestions && topicGuidanceQuestions.length > 0;

    return (
      <section className="speak-screen">
        <div className="speak-card speak-center-card">
          <div className="recording-indicator">
            <div className="recording-dot" />
            <span>{selectedTopic ?? "Free talk"}</span>
          </div>

          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo being described" className="photo-practice-preview" />
          )}

          <div className="timer">{formatTime(recordingDuration)}</div>
          {isAuthenticated && (
            <div className="recorded-subtitle">Session limit: {formatTime(Math.max(0, sessionLimitSeconds))}</div>
          )}

          <button className="btn btn-primary btn-large speak-primary-btn" onClick={() => dispatch(stopRecording())}>
            Stop
          </button>
        </div>

        {shouldShowQuestions && (
          <div className="speak-card">
            <div className="section-title speak-section-title">Questions</div>
            <div className="section-content speak-question-list">
              {topicGuidanceQuestions.map((question) => (
                <div key={question} className="question-item">
                  {question}
                </div>
              ))}
            </div>
          </div>
        )}
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
        {isAuthenticated && !isSubscriber && normalizedWeeklyRemainingSeconds <= 0 && (
          <div className="auth-error">
            You spent all free minutes for this week. Additional recordings will unlock next week.
          </div>
        )}

        {!isAuthenticated && <div className="notice">Saving is available only for authorized users. Sign in to continue.</div>}

        <div className="btn-group speak-button-group">
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
      </div>
    </section>
  );
}
