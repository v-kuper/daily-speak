"use client";

import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import {
  readBlobAsDataUrl,
  resolveAudioFileExtension,
  resolveBrowserRecordingSupportError,
  resolveMicrophoneError,
  resolvePreferredAudioMimeType
} from "../lib/browserMedia";
import { formatTime, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  backToQuestionsList,
  clearPhotoForPractice,
  clearQuestionsError,
  clearStudyError,
  clearTopicGuidanceError,
  fetchDailyQuestions,
  fetchStudyWords,
  fetchTopicGuidance,
  openAuthForSave,
  PHOTO_PRACTICE_MAX_BYTES,
  reRecord,
  type RecordingSaveDraft,
  saveRecording,
  selectTopic,
  setCustomTopicDraft,
  setRecordingAudioDataUrl,
  setRecordingInputError,
  setRecordingUploadSessionId,
  showBackgroundRecordingSave,
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
  useCustomTopic as applyCustomTopic
} from "../store/slices/appSlice";
import AudioWaveform from "./AudioWaveform";

const PHOTO_ACCEPTED_TYPES = new Set(["image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif"]);
type FinalAudioUploadState = "idle" | "uploading" | "ready" | "failed";

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

const uploadRecordingChunk = async (sessionId: string, chunkIndex: number, blob: Blob): Promise<void> => {
  const form = new FormData();
  const extension = resolveAudioFileExtension(blob.type);
  form.append("chunkIndex", String(chunkIndex));
  form.append("audio", blob, `chunk-${chunkIndex}.${extension}`);
  const response = await fetch(`/api/recording-sessions/${encodeURIComponent(sessionId)}/chunks`, {
    method: "POST",
    body: form
  });
  if (!response.ok) {
    throw new Error("Failed to upload recording chunk.");
  }
};

const uploadRecordingFinalAudio = async (sessionId: string, blob: Blob): Promise<void> => {
  const form = new FormData();
  const extension = resolveAudioFileExtension(blob.type);
  form.append("audio", blob, `recording.${extension}`);
  const response = await fetch(`/api/recording-sessions/${encodeURIComponent(sessionId)}/audio`, {
    method: "POST",
    body: form
  });
  if (!response.ok) {
    throw new Error("Failed to upload final recording audio.");
  }
};

type StudyTextSegment = {
  text: string;
  isStudyWord: boolean;
};

const escapeRegExp = (value: string): string => {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
};

const buildStudyTextSegments = (text: string, words: string[]): StudyTextSegment[] => {
  if (!text) {
    return [];
  }

  const uniqueWords = Array.from(
    new Set(
      words
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
    )
  );
  if (uniqueWords.length === 0) {
    return [{ text, isStudyWord: false }];
  }

  const pattern = uniqueWords
    .sort((a, b) => b.length - a.length)
    .map((item) => escapeRegExp(item))
    .join("|");
  if (!pattern) {
    return [{ text, isStudyWord: false }];
  }

  const regex = new RegExp(`\\b(?:${pattern})\\b`, "gi");
  const segments: StudyTextSegment[] = [];
  let cursor = 0;
  let match = regex.exec(text);

  while (match) {
    const start = match.index;
    const end = start + match[0].length;

    if (start > cursor) {
      segments.push({
        text: text.slice(cursor, start),
        isStudyWord: false
      });
    }

    segments.push({
      text: text.slice(start, end),
      isStudyWord: true
    });

    cursor = end;
    if (regex.lastIndex === start) {
      regex.lastIndex += 1;
    }
    match = regex.exec(text);
  }

  if (cursor < text.length) {
    segments.push({
      text: text.slice(cursor),
      isStudyWord: false
    });
  }

  return segments.length > 0 ? segments : [{ text, isStudyWord: false }];
};

export default function SpeakScreen() {
  const dispatch = useAppDispatch();
  const [finalAudioUploadState, setFinalAudioUploadState] = useState<FinalAudioUploadState>("idle");
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
    studyWords,
    studyText,
    studyStatus,
    studyError,
    recordingSaveStatus,
    recordingSaveError,
    isSubscriber,
    weeklyLimitSeconds,
    weeklyUsedSeconds,
    weeklyRemainingSeconds,
    maxSessionSeconds,
    recordingPracticeType,
    pendingRecordingAudioDataUrl,
    recordingInputError,
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
  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const mediaStreamRef = useRef<MediaStream | null>(null);
  const mediaChunksRef = useRef<Blob[]>([]);
  const uploadSessionIdRef = useRef<string | null>(null);
  const uploadChunkIndexRef = useRef(0);
  const chunkUploadQueueRef = useRef<Promise<void>>(Promise.resolve());
  const chunkUploadFailedRef = useRef(false);
  const finalAudioUploadPromiseRef = useRef<Promise<void> | null>(null);

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

  const createUploadSession = useCallback(async (): Promise<string | null> => {
    if (!isAuthenticated) {
      dispatch(setRecordingUploadSessionId(null));
      return null;
    }

    const normalizedPhotoObject = pendingPhotoObjectDraft
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, 120);
    const photoObject = normalizedPhotoObject || null;
    const topic =
      recordingPracticeType === "photo_description"
        ? photoObject
          ? `Photo description: ${photoObject}`
          : "Photo description"
        : selectedTopic ?? "Free talk";
    const response = await fetch("/api/recording-sessions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({
        topic,
        duration: 0,
        timestamp: new Date().toISOString(),
        practiceType: recordingPracticeType,
        photoDataUrl: recordingPracticeType === "photo_description" ? pendingPhotoDataUrl : null,
        photoObject
      })
    });
    const payload = (await response.json().catch(() => null)) as { sessionId?: unknown; error?: string } | null;
    if (!response.ok || typeof payload?.sessionId !== "string" || !payload.sessionId.trim()) {
      throw new Error(payload?.error ?? "Failed to start recording upload.");
    }
    const sessionId = payload.sessionId.trim();
    dispatch(setRecordingUploadSessionId(sessionId));
    return sessionId;
  }, [dispatch, isAuthenticated, pendingPhotoDataUrl, pendingPhotoObjectDraft, recordingPracticeType, selectedTopic]);

  const createRecordingFromMicrophone = useCallback(
    async (onRecordingStarted: () => void) => {
      dispatch(setRecordingInputError(null));
      dispatch(setRecordingAudioDataUrl(null));
      dispatch(setRecordingUploadSessionId(null));
      uploadSessionIdRef.current = null;
      uploadChunkIndexRef.current = 0;
      chunkUploadFailedRef.current = false;
      chunkUploadQueueRef.current = Promise.resolve();
      finalAudioUploadPromiseRef.current = null;
      setFinalAudioUploadState("idle");

      const recordingSupportError = resolveBrowserRecordingSupportError();
      if (recordingSupportError) {
        dispatch(setRecordingInputError(recordingSupportError));
        return;
      }

      try {
        let uploadSessionId: string | null = null;
        try {
          uploadSessionId = await createUploadSession();
        } catch {
          dispatch(setRecordingInputError("Live upload is unavailable. The full recording will be uploaded when you save."));
        }

        const getUserMedia = navigator.mediaDevices?.getUserMedia?.bind(navigator.mediaDevices);
        if (!getUserMedia) {
          dispatch(setRecordingInputError("Your browser does not support microphone recording."));
          return;
        }

        const stream = await getUserMedia({ audio: true });
        const mimeType = resolvePreferredAudioMimeType();
        const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);

        mediaStreamRef.current = stream;
        mediaRecorderRef.current = recorder;
        mediaChunksRef.current = [];
        uploadSessionIdRef.current = uploadSessionId;

        recorder.ondataavailable = (event: BlobEvent) => {
          if (event.data && event.data.size > 0) {
            mediaChunksRef.current.push(event.data);
            const sessionId = uploadSessionIdRef.current;
            if (sessionId && !chunkUploadFailedRef.current) {
              const chunkIndex = uploadChunkIndexRef.current;
              uploadChunkIndexRef.current += 1;
              const blob = event.data;
              chunkUploadQueueRef.current = chunkUploadQueueRef.current
                .catch(() => undefined)
                .then(() => uploadRecordingChunk(sessionId, chunkIndex, blob))
                .catch(() => {
                  chunkUploadFailedRef.current = true;
                  dispatch(setRecordingInputError("Live chunk upload paused. The complete recording will be uploaded before saving."));
                });
            }
          }
        };

        recorder.onerror = () => {
          dispatch(setRecordingInputError("Recording failed. Please try again."));
        };

        recorder.onstop = () => {
          const chunks = [...mediaChunksRef.current];
          const resultingType = recorder.mimeType || "audio/webm";
          releaseMedia();

          if (chunks.length === 0) {
            dispatch(setRecordingInputError("No audio captured. Try recording again."));
            return;
          }

          const blob = new Blob(chunks, { type: resultingType });
          const finalUpload = chunkUploadQueueRef.current
            .catch(() => undefined)
            .then(async () => {
              const sessionId = uploadSessionIdRef.current;
              if (!sessionId) {
                return;
              }
              await uploadRecordingFinalAudio(sessionId, blob);
            });
          finalAudioUploadPromiseRef.current = finalUpload;
          setFinalAudioUploadState(uploadSessionIdRef.current ? "uploading" : "ready");

          void readBlobAsDataUrl(blob)
            .then((dataUrl) => {
              dispatch(setRecordingAudioDataUrl(dataUrl));
            })
            .catch(() => {
              dispatch(setRecordingInputError("Failed to process recorded audio."));
            });
          void finalUpload
            .then(() => {
              setFinalAudioUploadState("ready");
              if (chunkUploadFailedRef.current) {
                dispatch(setRecordingInputError(null));
              }
            })
            .catch(() => {
              uploadSessionIdRef.current = null;
              setFinalAudioUploadState("failed");
              dispatch(setRecordingUploadSessionId(null));
              dispatch(setRecordingInputError("Full recording will be uploaded when you save."));
            });
        };

        recorder.start(uploadSessionId ? 5000 : undefined);
        onRecordingStarted();
      } catch (error) {
        releaseMedia();
        dispatch(setRecordingInputError(resolveMicrophoneError(error)));
      }
    },
    [createUploadSession, dispatch, releaseMedia]
  );

  const buildRecordingSaveDraft = useCallback((): RecordingSaveDraft | null => {
    const audioDataUrl = pendingRecordingAudioDataUrl?.trim() || null;
    if (!audioDataUrl) {
      return null;
    }

    const normalizedPhotoObject = pendingPhotoObjectDraft
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, 120);
    const photoObject = normalizedPhotoObject || null;
    const topic =
      recordingPracticeType === "photo_description"
        ? photoObject
          ? `Photo description: ${photoObject}`
          : "Photo description"
        : selectedTopic ?? "Free talk";
    const timestamp = new Date().toISOString();

    return {
      localRecordingId: `local-${Date.now()}`,
      recordingUploadSessionId: uploadSessionIdRef.current,
      topic,
      duration: Math.max(0, Math.floor(recordingDuration)),
      timestamp,
      practiceType: recordingPracticeType,
      audioDataUrl,
      photoDataUrl: recordingPracticeType === "photo_description" ? pendingPhotoDataUrl : null,
      photoObject
    };
  }, [
    pendingPhotoDataUrl,
    pendingPhotoObjectDraft,
    pendingRecordingAudioDataUrl,
    recordingDuration,
    recordingPracticeType,
    selectedTopic
  ]);

  const onSaveRecording = useCallback(() => {
    if (!isAuthenticated) {
      dispatch(openAuthForSave());
      return;
    }

    const draft = buildRecordingSaveDraft();
    if (!draft) {
      dispatch(setRecordingInputError("Preparing audio, please wait a moment before saving."));
      return;
    }

    const finalUpload = finalAudioUploadPromiseRef.current;
    dispatch(showBackgroundRecordingSave(draft));

    void (async () => {
      try {
        if (finalUpload) {
          await finalUpload;
        }
        await dispatch(saveRecording(draft)).unwrap();
      } catch {
        if (!draft.audioDataUrl) {
          return;
        }
        try {
          await dispatch(saveRecording({ ...draft, recordingUploadSessionId: null })).unwrap();
        } catch {
          // The rejected thunk marks the optimistic recording as failed.
        }
      }
    })();
  }, [buildRecordingSaveDraft, dispatch, isAuthenticated]);

  const onStartFreeTalk = () => {
    void createRecordingFromMicrophone(() => {
      dispatch(startFreeTalk());
    });
  };

  const onStartTopicRecording = () => {
    void createRecordingFromMicrophone(() => {
      dispatch(startRecording());
    });
  };

  const onStopRecording = () => {
    dispatch(stopRecording());
    const recorder = mediaRecorderRef.current;
    if (recorder && recorder.state !== "inactive") {
      recorder.stop();
      return;
    }
    releaseMedia();
  };

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

  useEffect(() => {
    if (speakState === "recording") {
      return;
    }

    const recorder = mediaRecorderRef.current;
    if (recorder && recorder.state !== "inactive") {
      recorder.stop();
      return;
    }

    releaseMedia();
  }, [releaseMedia, speakState]);

  useEffect(() => {
    return () => {
      const recorder = mediaRecorderRef.current;
      if (recorder && recorder.state !== "inactive") {
        recorder.stop();
        return;
      }
      releaseMedia();
    };
  }, [releaseMedia]);

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

  const onGenerateStudyWords = () => {
    dispatch(clearStudyError());
    void dispatch(
      fetchStudyWords({
        force: true,
        refreshToken: String(Date.now()),
        interestIds: selectedInterestIds,
        avoidWords: studyWords,
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
    const shouldShowStudySkeleton = studyStatus === "loading" && studyWords.length === 0 && !studyText;
    const studyParagraphs = studyText
      ? studyText
          .split(/\n{2,}/)
          .map((item) => item.trim())
          .filter((item) => item.length > 0)
      : [];

    return (
      <section className="speak-screen">
        <div className="speak-card speak-hero-card">
          <div className="heading-sm">Daily practice</div>
          <h2 className="heading-xl speak-heading-tight">Start a new speaking session</h2>
          <div className="studio-focus-panel">
            <div className="studio-kicker">Ready when you are</div>
            <AudioWaveform variant="hero" />
            <div className="studio-timer-preview">00:00</div>
          </div>
          {quotaHint && <div className="notice">{quotaHint}</div>}
          {freeLimitReached && (
            <div className="auth-error">Free weekly limit reached. New quota will be available next week.</div>
          )}
          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={onStartFreeTalk}
            disabled={!hasRecordingBudget}
          >
            Start speaking
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
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
            <div className="section-title speak-section-title">Words for study</div>
            <button className="btn btn-secondary btn-small" onClick={onGenerateStudyWords} disabled={studyStatus === "loading"}>
              {studyStatus === "loading" ? "Generating..." : studyWords.length === 10 ? "↻ Regenerate" : "Generate"}
            </button>
          </div>

          <div className="profile-value">
            Generate 10 words and a practical context text for level {selectedEnglishLevel.toUpperCase()}.
          </div>

          {shouldShowStudySkeleton && (
            <div className="study-pack-skeleton" aria-hidden="true">
              <div className="skeleton-line skeleton-line-wide" />
              <div className="skeleton-line skeleton-line-wide" />
              <div className="skeleton-line skeleton-line-medium" />
            </div>
          )}

          {!shouldShowStudySkeleton && studyWords.length > 0 && (
            <div className="study-words-grid">
              {studyWords.map((word) => (
                <div key={word.toLowerCase()} className="study-word-chip">
                  {word}
                </div>
              ))}
            </div>
          )}

          {!shouldShowStudySkeleton && studyParagraphs.length > 0 && (
            <div className="study-text-card">
              {studyParagraphs.map((paragraph, index) => (
                <p key={`study-paragraph-${index}`}>
                  {buildStudyTextSegments(paragraph, studyWords).map((segment, segmentIndex) =>
                    segment.isStudyWord ? (
                      <mark key={`study-segment-${index}-${segmentIndex}`} className="study-word-mark">
                        {segment.text}
                      </mark>
                    ) : (
                      <span key={`study-segment-${index}-${segmentIndex}`}>{segment.text}</span>
                    )
                  )}
                </p>
              ))}
            </div>
          )}

          {!shouldShowStudySkeleton && studyWords.length === 0 && (
            <div className="empty-state speak-empty-state">Generate vocabulary set to start learning words in context.</div>
          )}

          {studyError && <div className="auth-error top-spaced">{studyError}</div>}
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
                <button className="btn btn-primary" onClick={() => dispatch(applyCustomTopic())}>
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
          <button className="btn btn-secondary btn-small" onClick={() => dispatch(backToQuestionsList())}>
            ← Back to questions
          </button>
          <div className="heading-sm">Selected question</div>
          {isPhotoPractice && pendingPhotoDataUrl && (
            <img src={pendingPhotoDataUrl} alt="Photo to describe" className="photo-practice-preview" />
          )}
          <h2 className="heading-xl speak-heading-tight">{selectedTopic}</h2>

          <div className="studio-focus-panel studio-ready-panel">
            <div className="studio-kicker">Session loaded</div>
            <AudioWaveform variant="compact" />
          </div>

          {quotaHint && <div className="notice">{quotaHint}</div>}
          {freeLimitReached && (
            <div className="auth-error">Free weekly limit reached. New quota will be available next week.</div>
          )}

          <button
            className="btn btn-primary btn-large speak-primary-btn"
            onClick={onStartTopicRecording}
            disabled={!hasRecordingBudget || (isPhotoPractice && !pendingPhotoDataUrl)}
          >
            Start speaking
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
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

          <div className="studio-focus-panel live">
            <div className="studio-kicker">Live recording</div>
            <AudioWaveform variant="hero" active />
          </div>

          <div className="timer">{formatTime(recordingDuration)}</div>
          {isAuthenticated && (
            <div className="recorded-subtitle">Session limit: {formatTime(Math.max(0, sessionLimitSeconds))}</div>
          )}

          <button className="btn btn-primary btn-large speak-primary-btn" onClick={onStopRecording}>
            Stop
          </button>
          {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
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

        <div className="studio-focus-panel">
          <div className="studio-kicker">Captured audio</div>
          <AudioWaveform variant="hero" />
        </div>

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
            onClick={onSaveRecording}
            disabled={recordingSaveStatus === "loading" || (isAuthenticated && !pendingRecordingAudioDataUrl)}
          >
            {isAuthenticated
              ? recordingSaveStatus === "loading"
                ? "Saving..."
                : "Save and continue"
              : "Sign in to save"}
          </button>
        </div>
        {!pendingRecordingAudioDataUrl && !recordingInputError && (
          <div className="notice top-spaced">Preparing audio, please wait a moment before saving.</div>
        )}
        {pendingRecordingAudioDataUrl && finalAudioUploadState === "uploading" && (
          <div className="notice top-spaced">
            Uploading audio in the background. You can save now and continue while processing runs.
            <div className="background-progress" aria-hidden="true">
              <div className="background-progress-fill" />
            </div>
          </div>
        )}
        {pendingRecordingAudioDataUrl && finalAudioUploadState === "ready" && (
          <div className="notice top-spaced">Audio is ready. Saving will start background transcription.</div>
        )}
        {recordingInputError && <div className="auth-error top-spaced">{recordingInputError}</div>}
        {recordingSaveError && <div className="auth-error top-spaced">{recordingSaveError}</div>}
      </div>
    </section>
  );
}
