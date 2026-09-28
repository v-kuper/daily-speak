import { createAsyncThunk, createSlice, type PayloadAction } from "@reduxjs/toolkit";
import { apiFetch, readApiJSON } from "../../lib/apiClient";
import {
  type PracticeType,
  type Recording,
  type RecordingMedia,
  type RecordingMediaAsset,
  type RecordingStatus
} from "../../lib/data";
import {
  filterDeletedRecordings,
  removeRecording
} from "../../lib/recordingDeletion";
import { parseRecordingProcessingStage } from "../../lib/recordingProcessing";
import { parseShadowingStatus } from "../../lib/shadowing";
import { DEFAULT_ENGLISH_LEVEL, normalizeEnglishLevel, parseEnglishLevel, type EnglishLevel } from "../../lib/englishLevel";
import { isCurrentInterviewGuidanceRequest } from "../../lib/interviewGuidance";
import {
  INTEREST_OPTIONS,
  MAX_SELECTED_INTERESTS,
  getInterestOption,
  normalizeInterestIds,
  resolveInterestLabels,
  type InterestOption
} from "../../lib/interestCatalog";
import { formatTime } from "../../lib/utils";
import { parseSuggestions } from "../../lib/suggestions";
import {
  completeGuestPromotion,
  GuestPreviewError,
  MAX_GUEST_PREVIEW_SECONDS,
  type GuestPreviewPromotion,
} from "../../lib/guestPreview";
import {
  authenticateBrowserIdentity,
  IdentityError,
  logoutBrowserIdentity,
  restoreBrowserIdentity,
} from "../../lib/identity";
import { dataURLToBlob, MediaUploadError, uploadMedia } from "../../lib/mediaUpload";
import { finalizeInterview } from "../../lib/interviewSession";
import { parseInterviewTurns } from "../../lib/interviewTimeline";

export type SpeakMode = "idle" | "readyToRecord" | "recording" | "recorded";
export type AuthStatus = "idle" | "loading";
export type QuestionsStatus = "idle" | "loading" | "ready" | "failed";
export { INTEREST_OPTIONS, MAX_SELECTED_INTERESTS };
export type { InterestOption };

export type AppState = {
  speakState: SpeakMode;
  selectedTopic: string | null;
  showQuestions: boolean;
  showWords: boolean;
  recordingDuration: number;
  recordings: Recording[];
  deletedRecordingIds: string[];
  currentRecordingId: string | null;
  backgroundSaveRecordingId: string | null;
  isPlaying: boolean;
  playbackPosition: number;
  topics: string[];
  showAddTopicInput: boolean;
  customTopicDraft: string;
  calendarVisible: boolean;
  calendarMonth: number;
  calendarYear: number;
  isAuthenticated: boolean;
  userEmail: string | null;
  authEmailDraft: string;
  authPasswordDraft: string;
  authError: string | null;
  authStatus: AuthStatus;
  authInitialized: boolean;
  pendingSaveAfterAuth: boolean;
  pendingAuthSaveDraft: RecordingSaveDraft | null;
  questionsStatus: QuestionsStatus;
  questionsError: string | null;
  questionsDate: string | null;
  questionsInterestsKey: string;
  questionsEnglishLevel: EnglishLevel;
  topicGuidanceQuestions: string[];
  topicGuidanceWords: string[];
  topicGuidanceStatus: QuestionsStatus;
  topicGuidanceError: string | null;
  topicGuidanceTopic: string | null;
  topicGuidanceInterestsKey: string;
  topicGuidanceEnglishLevel: EnglishLevel;
  topicGuidanceRequestId: string | null;
  studyWords: string[];
  studyText: string;
  studyStatus: QuestionsStatus;
  studyError: string | null;
  studyInterestsKey: string;
  studyEnglishLevel: EnglishLevel;
  recordingPracticeType: PracticeType;
  pendingRecordingAudioDataUrl: string | null;
  recordingInputError: string | null;
  pendingPhotoDataUrl: string | null;
  pendingPhotoObjectDraft: string;
  pendingPhotoError: string | null;
  selectedInterestIds: string[];
  selectedEnglishLevel: EnglishLevel;
  userDataStatus: QuestionsStatus;
  userDataError: string | null;
  recordingSaveStatus: AuthStatus;
  recordingSaveError: string | null;
  // Local ID -> permanent ID. Used when a route mounts after saving finishes.
  recordingSaveResults: Record<string, string>;
  // Kept in memory until the server confirms creation, so a transient upload
  // failure can be retried without recording the audio again.
  recordingSaveDrafts: Record<string, RecordingSaveDraft>;
  recordingFetchStatuses: Record<string, "loading" | "ready" | "failed">;
  recordingFetchErrors: Record<string, string>;
  recordingFetchFailureKinds: Record<string, "transient" | "terminal">;
  recordingDeleteStatus: AuthStatus;
  recordingDeleteError: string | null;
  recordingRetryStatuses: Record<string, AuthStatus>;
  recordingRetryErrors: Record<string, string>;
  shadowingRequestStatus: AuthStatus;
  shadowingRequestError: string | null;
  interestsSaveStatus: AuthStatus;
  interestsSaveError: string | null;
  isSubscriber: boolean;
  weeklyLimitSeconds: number | null;
  weeklyUsedSeconds: number;
  weeklyRemainingSeconds: number | null;
  maxSessionSeconds: number;
  subscriptionExpiresAt: string | null;
  subscriptionCancelled: boolean;
  subscriptionActionStatus: AuthStatus;
  subscriptionActionError: string | null;
  englishLevelSaveStatus: AuthStatus;
  englishLevelSaveError: string | null;
};

export type RecordingSaveDraft = {
  localRecordingId?: string;
  topic: string;
  duration: number;
  timestamp: string;
  practiceType: PracticeType;
  audioDataUrl: string | null;
  photoDataUrl: string | null;
  photoObject: string | null;
  interviewSessionId?: string;
  interviewEndedAtMs?: number;
};

const today = new Date();
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const MIN_PASSWORD_LENGTH = 8;
const FREE_WEEKLY_LIMIT_SECONDS = 10 * 60;
const SESSION_LIMIT_SECONDS = 10 * 60;
const MIN_DAILY_QUESTIONS = 3;
const MIN_TOPIC_GUIDANCE_QUESTIONS = 10;
const MIN_TOPIC_GUIDANCE_WORDS = 8;
const PHOTO_PRACTICE_MAX_OBJECT_LENGTH = 120;
const MAX_RECORDING_AUDIO_BYTES = 80 * 1024 * 1024;
const AUDIO_DATA_URL_PATTERN = /^data:((?:audio|video)\/[a-z0-9.+-]+(?:;[^,]+)*);base64,([A-Za-z0-9+/_=-]+)$/i;
export const PHOTO_PRACTICE_MAX_BYTES = 4 * 1024 * 1024;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;
const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);

type FetchDailyQuestionsArgs = {
  dateKey: string;
  force?: boolean;
  refreshToken?: string;
  interestIds?: string[];
  avoidQuestions?: string[];
  englishLevel?: EnglishLevel;
};

type FetchDailyQuestionsResult = {
  dateKey: string;
  questions: string[];
};

type DailyQuestionsResponse = {
  questions?: unknown;
} & V1ErrorResponse;

export type FetchTopicGuidanceArgs = {
  topic: string;
  force?: boolean;
  refreshToken?: string;
  interestIds?: string[];
  avoidQuestions?: string[];
  avoidWords?: string[];
  englishLevel?: EnglishLevel;
};

type FetchTopicGuidanceResult = {
  topic: string;
  questions: string[];
  words: string[];
};

type TopicGuidanceResponse = {
  questions?: unknown;
  words?: unknown;
} & V1ErrorResponse;

type FetchStudyWordsArgs = {
  force?: boolean;
  refreshToken?: string;
  interestIds?: string[];
  avoidWords?: string[];
  englishLevel?: EnglishLevel;
};

type FetchStudyWordsResult = {
  words: string[];
  text: string;
};

type StudyWordsResponse = {
  words?: unknown;
  text?: unknown;
} & V1ErrorResponse;

type AuthResult = {
  email: string;
  isSubscriber: boolean;
  englishLevel: EnglishLevel;
  guestPreviewPromotion?: GuestPreviewPromotion;
};

type UserDataResponse = {
  interestIds?: unknown;
  quota?: unknown;
  subscription?: unknown;
  englishLevel?: unknown;
} & V1ErrorResponse;

type RecordingPageResponse = V1ErrorResponse & {
  items?: unknown;
};

type SaveInterestsResponse = {
  interestIds?: unknown;
} & V1ErrorResponse;

type SaveRecordingResponse = {
  recording?: unknown;
  quota?: unknown;
  error?: string | { message?: unknown };
};

type V1ErrorResponse = {
  error?: { message?: unknown };
};

const apiErrorMessage = (payload: V1ErrorResponse | null, fallback: string): string => {
  return typeof payload?.error?.message === "string" ? payload.error.message : fallback;
};

type DeleteRecordingResponse = V1ErrorResponse & {
  deletedRecordingId?: unknown;
  quota?: unknown;
};

type SubscriptionResponse = {
  subscription?: unknown;
  quota?: unknown;
} & V1ErrorResponse;

type EnglishLevelResponse = {
  level?: unknown;
} & V1ErrorResponse;

type RecordingQuota = {
  isSubscriber: boolean;
  weeklyLimitSeconds: number | null;
  weeklyUsedSeconds: number;
  weeklyRemainingSeconds: number | null;
  maxSessionSeconds: number;
};

type SubscriptionState = {
  isSubscriber: boolean;
  subscriptionExpiresAt: string | null;
  subscriptionCancelled: boolean;
};

const buildDefaultRecordingQuota = (isSubscriber: boolean): RecordingQuota => {
  if (isSubscriber) {
    return {
      isSubscriber: true,
      weeklyLimitSeconds: null,
      weeklyUsedSeconds: 0,
      weeklyRemainingSeconds: null,
      maxSessionSeconds: SESSION_LIMIT_SECONDS
    };
  }

  return {
    isSubscriber: false,
    weeklyLimitSeconds: FREE_WEEKLY_LIMIT_SECONDS,
    weeklyUsedSeconds: 0,
    weeklyRemainingSeconds: FREE_WEEKLY_LIMIT_SECONDS,
    maxSessionSeconds: SESSION_LIMIT_SECONDS
  };
};

const DEFAULT_RECORDING_QUOTA: RecordingQuota = buildDefaultRecordingQuota(false);
const DEFAULT_SUBSCRIPTION_STATE: SubscriptionState = {
  isSubscriber: false,
  subscriptionExpiresAt: null,
  subscriptionCancelled: false
};

const parseRecordingQuota = (value: unknown): RecordingQuota | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const payload = value as Record<string, unknown>;
  const isSubscriber = Boolean(payload.isSubscriber);
  const weeklyUsedSeconds = Number.parseInt(String(payload.weeklyUsedSeconds ?? 0), 10);
  const maxSessionSeconds = Number.parseInt(String(payload.maxSessionSeconds ?? SESSION_LIMIT_SECONDS), 10);
  const weeklyLimitRaw = payload.weeklyLimitSeconds;
  const weeklyRemainingRaw = payload.weeklyRemainingSeconds;

  if (!Number.isFinite(weeklyUsedSeconds) || weeklyUsedSeconds < 0) {
    return null;
  }

  if (!Number.isFinite(maxSessionSeconds) || maxSessionSeconds <= 0) {
    return null;
  }

  const weeklyLimitSeconds =
    weeklyLimitRaw === null || typeof weeklyLimitRaw === "undefined"
      ? null
      : Number.parseInt(String(weeklyLimitRaw), 10);
  if (weeklyLimitSeconds !== null && (!Number.isFinite(weeklyLimitSeconds) || weeklyLimitSeconds < 0)) {
    return null;
  }

  const weeklyRemainingSeconds =
    weeklyRemainingRaw === null || typeof weeklyRemainingRaw === "undefined"
      ? null
      : Number.parseInt(String(weeklyRemainingRaw), 10);
  if (weeklyRemainingSeconds !== null && (!Number.isFinite(weeklyRemainingSeconds) || weeklyRemainingSeconds < 0)) {
    return null;
  }

  return {
    isSubscriber,
    weeklyLimitSeconds,
    weeklyUsedSeconds,
    weeklyRemainingSeconds,
    maxSessionSeconds
  };
};

const parseSubscriptionState = (value: unknown): SubscriptionState | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const payload = value as Record<string, unknown>;
  const isSubscriber = Boolean(payload.isSubscriber);
  const subscriptionCancelled = Boolean(payload.subscriptionCancelled);
  const rawExpiresAt = payload.subscriptionExpiresAt;

  if (rawExpiresAt === null || typeof rawExpiresAt === "undefined") {
    return {
      isSubscriber,
      subscriptionExpiresAt: null,
      subscriptionCancelled: isSubscriber ? subscriptionCancelled : false
    };
  }

  if (typeof rawExpiresAt !== "string") {
    return null;
  }

  const parsedDate = new Date(rawExpiresAt);
  if (Number.isNaN(parsedDate.getTime())) {
    return null;
  }

  return {
    isSubscriber,
    subscriptionExpiresAt: parsedDate.toISOString(),
    subscriptionCancelled: isSubscriber ? subscriptionCancelled : false
  };
};

const parseStudyWordsResponse = (payload: StudyWordsResponse | null): { words: string[]; text: string } | null => {
  const wordsRaw = Array.isArray(payload?.words) ? payload.words : [];
  const words = wordsRaw
    .filter((item): item is string => typeof item === "string")
    .map((item) =>
      item
        .trim()
        .replace(/^\d+\s*[\)\.\-:]\s*/, "")
        .replace(/^[-*]\s*/, "")
        .replace(/^["'`]+/, "")
        .replace(/["'`]+$/, "")
        .replace(/[.,;:!?]+$/g, "")
        .replace(/\s+/g, " ")
    )
    .filter((item) => item.length > 0);

  const uniqueWords = Array.from(new Set(words.map((item) => item.toLowerCase()))).map((key) => {
    const original = words.find((item) => item.toLowerCase() === key);
    return original ?? key;
  });
  const text = typeof payload?.text === "string" ? payload.text.trim().replace(/\r\n/g, "\n").replace(/\n{3,}/g, "\n\n") : "";

  if (uniqueWords.length !== 10 || !text) {
    return null;
  }

  return {
    words: uniqueWords,
    text
  };
};

const parsePracticeType = (value: unknown, topic: string, hasPhoto: boolean): PracticeType => {
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase() as PracticeType;
    if (PRACTICE_TYPE_SET.has(normalized)) {
      return normalized;
    }
  }

  if (hasPhoto) {
    return "photo_description";
  }

  if (topic.toLowerCase() === "free talk") {
    return "free_talk";
  }

  return "topic";
};

const parseRecordingStatus = (value: unknown): RecordingStatus => {
  if (value === "processing" || value === "ready" || value === "failed") {
    return value;
  }
  return "ready";
};

const normalizeAudioDataUrl = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  const match = normalized.match(AUDIO_DATA_URL_PATTERN);
  if (!match) {
    return null;
  }

  const mediaType = match[1].toLowerCase().replace(/\s+/g, "");
  const payload = match[2].replace(/-/g, "+").replace(/_/g, "/");
  if (!/^[A-Za-z0-9+/=]+$/.test(payload)) {
    return null;
  }
  const padding = payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((payload.length * 3) / 4) - padding;
  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > MAX_RECORDING_AUDIO_BYTES) {
    return null;
  }

  return `data:${mediaType};base64,${payload}`;
};

const normalizePhotoDataUrl = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  if (!PHOTO_DATA_URL_PATTERN.test(normalized)) {
    return null;
  }

  const payload = normalized.split(",", 2)[1] ?? "";
  const padding = payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0;
  const bytes = Math.floor((payload.length * 3) / 4) - padding;
  if (!Number.isFinite(bytes) || bytes <= 0 || bytes > PHOTO_PRACTICE_MAX_BYTES) {
    return null;
  }

  return normalized;
};

const normalizePhotoObject = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim().replace(/\s+/g, " ").slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH);
  return normalized || null;
};

const parseRecordingMediaAsset = (value: unknown): RecordingMediaAsset | null => {
  if (!value || typeof value !== "object") {
    return null;
  }
  const candidate = value as Record<string, unknown>;
  const assetId = typeof candidate.assetId === "string" ? candidate.assetId.trim() : "";
  const downloadPath = typeof candidate.downloadPath === "string" ? candidate.downloadPath.trim() : "";
  if (!assetId || downloadPath !== `/api/v1/media/${encodeURIComponent(assetId)}/download`) {
    return null;
  }
  return { assetId, downloadPath };
};

const parseRecordingMedia = (value: unknown): RecordingMedia | null => {
  if (!value || typeof value !== "object") {
    return null;
  }
  const candidate = value as Record<string, unknown>;
  const media: RecordingMedia = {
    audio: parseRecordingMediaAsset(candidate.audio),
    photo: parseRecordingMediaAsset(candidate.photo),
    shadowing: parseRecordingMediaAsset(candidate.shadowing),
  };
  return media.audio || media.photo || media.shadowing ? media : null;
};

const parseRecording = (value: unknown): Recording | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const candidate = value as Record<string, unknown>;
  const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
  const topic = typeof candidate.topic === "string" ? candidate.topic.trim() : "";
  const status = parseRecordingStatus(candidate.status);
  const transcript = typeof candidate.transcript === "string" ? candidate.transcript : "";
  const correctedTranscript = typeof candidate.correctedTranscript === "string" ? candidate.correctedTranscript : "";
  const processingStage = parseRecordingProcessingStage(candidate.processingStage);
  const timestampRaw = typeof candidate.timestamp === "string" ? candidate.timestamp : "";
  const timestamp = new Date(timestampRaw);
  const duration = Number.parseInt(String(candidate.duration ?? 0), 10);
  const suggestions = parseSuggestions(candidate.suggestions);
  const photoObject = normalizePhotoObject(candidate.photoObject);
  const processingError = typeof candidate.processingError === "string" ? candidate.processingError.trim() || null : null;
  const shadowingStatus = parseShadowingStatus(candidate.shadowingStatus);
  const shadowingError =
    typeof candidate.shadowingError === "string" ? candidate.shadowingError.trim() || null : null;
  const media = parseRecordingMedia(candidate.media);
  const practiceType = parsePracticeType(candidate.practiceType, topic, Boolean(media?.photo));

  if (!id || !topic || Number.isNaN(timestamp.getTime()) || !Number.isFinite(duration) || duration < 0) {
    return null;
  }

  const shadowingUpdatedAtRaw =
    typeof candidate.shadowingUpdatedAt === "string" ? candidate.shadowingUpdatedAt : "";
  const shadowingUpdatedAtDate = new Date(shadowingUpdatedAtRaw);
  const shadowingUpdatedAt = Number.isNaN(shadowingUpdatedAtDate.getTime())
    ? timestamp.toISOString()
    : shadowingUpdatedAtDate.toISOString();

  return {
    id,
    topic,
    duration: Math.max(0, duration),
    timestamp: timestamp.toISOString(),
    status,
    transcript,
    interviewTurns: parseInterviewTurns(candidate.interviewTurns),
    correctedTranscript,
    suggestions,
    processingStage,
    practiceType,
    localAudioDataUrl: null,
    localPhotoDataUrl: null,
    photoObject,
    processingError,
    shadowingStatus,
    shadowingError,
    shadowingUpdatedAt,
    media
  };
};

const buildInterestsKey = (interestIds: string[]): string => {
  return [...interestIds].sort().join("|");
};

export const fetchDailyQuestions = createAsyncThunk<
  FetchDailyQuestionsResult,
  FetchDailyQuestionsArgs,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/fetchDailyQuestions",
  async ({ dateKey, refreshToken, interestIds = [], avoidQuestions = [], englishLevel = DEFAULT_ENGLISH_LEVEL }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ date: dateKey, level: englishLevel });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }
      const interestLabels = resolveInterestLabels(interestIds);
      interestLabels.forEach((label) => params.append("interest", label));
      avoidQuestions
        .filter((item) => item.trim().length > 0)
        .forEach((item) => params.append("avoid", item.trim()));

      const response = await apiFetch(`/api/v1/practice/daily-questions?${params.toString()}`, {
        cache: "no-store"
      });
      const payload = (await readApiJSON(response)) as DailyQuestionsResponse | null;

      if (!response.ok) {
        return rejectWithValue(apiErrorMessage(payload, "Failed to load daily questions from Ollama."));
      }

      const questions = Array.isArray(payload?.questions)
        ? payload.questions
            .filter((item): item is string => typeof item === "string")
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
        : [];

      if (questions.length !== MIN_DAILY_QUESTIONS) {
        return rejectWithValue(`Ollama must return exactly ${MIN_DAILY_QUESTIONS} questions.`);
      }

      return { dateKey, questions };
    } catch {
      return rejectWithValue("Cannot connect to local Ollama. Make sure Ollama is running.");
    }
  },
  {
    condition: ({ dateKey, force, interestIds = [], englishLevel = DEFAULT_ENGLISH_LEVEL }, { getState }) => {
      if (force) {
        return true;
      }
      const { app } = getState();
      if (app.questionsStatus === "loading") {
        return false;
      }
      const interestKey = buildInterestsKey(interestIds);
      if (
        app.questionsDate === dateKey &&
        app.questionsInterestsKey === interestKey &&
        app.questionsEnglishLevel === englishLevel &&
        app.topics.length === MIN_DAILY_QUESTIONS
      ) {
        return false;
      }
      return true;
    }
  }
);

export const fetchTopicGuidance = createAsyncThunk<
  FetchTopicGuidanceResult,
  FetchTopicGuidanceArgs,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/fetchTopicGuidance",
  async (
    { topic, refreshToken, interestIds = [], avoidQuestions = [], avoidWords = [], englishLevel = DEFAULT_ENGLISH_LEVEL },
    { rejectWithValue, signal }
  ) => {
    try {
      const params = new URLSearchParams({ topic, level: englishLevel });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }
      const interestLabels = resolveInterestLabels(interestIds);
      interestLabels.forEach((label) => params.append("interest", label));
      avoidQuestions
        .filter((item) => item.trim().length > 0)
        .forEach((item) => params.append("avoidQuestion", item.trim()));
      avoidWords
        .filter((item) => item.trim().length > 0)
        .forEach((item) => params.append("avoidWord", item.trim()));

      const response = await apiFetch(`/api/v1/practice/topic-guidance?${params.toString()}`, {
        cache: "no-store",
        signal
      });
      const payload = (await readApiJSON(response)) as TopicGuidanceResponse | null;

      if (!response.ok) {
        return rejectWithValue(apiErrorMessage(payload, "Failed to generate questions and useful words."));
      }

      const questions = Array.isArray(payload?.questions)
        ? payload.questions
            .filter((item): item is string => typeof item === "string")
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
        : [];

      const words = Array.isArray(payload?.words)
        ? payload.words
            .filter((item): item is string => typeof item === "string")
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
        : [];

      if (questions.length < MIN_TOPIC_GUIDANCE_QUESTIONS || words.length < MIN_TOPIC_GUIDANCE_WORDS) {
        return rejectWithValue(
          `Ollama must return ${MIN_TOPIC_GUIDANCE_QUESTIONS} follow-up questions and ${MIN_TOPIC_GUIDANCE_WORDS} useful words for this topic.`
        );
      }

      return {
        topic,
        questions: questions.slice(0, MIN_TOPIC_GUIDANCE_QUESTIONS),
        words: words.slice(0, MIN_TOPIC_GUIDANCE_WORDS)
      };
    } catch {
      return rejectWithValue("Cannot connect to local Ollama. Make sure Ollama is running.");
    }
  },
  {
    condition: ({ topic, force, interestIds = [], englishLevel = DEFAULT_ENGLISH_LEVEL }, { getState }) => {
      if (force) {
        return true;
      }
      const normalizedTopic = topic.trim();
      if (!normalizedTopic) {
        return false;
      }
      const { app } = getState();
      const interestKey = buildInterestsKey(interestIds);
      if (
        app.topicGuidanceTopic === normalizedTopic &&
        app.topicGuidanceInterestsKey === interestKey &&
        app.topicGuidanceEnglishLevel === englishLevel &&
        app.topicGuidanceStatus === "ready" &&
        app.topicGuidanceQuestions.length >= MIN_TOPIC_GUIDANCE_QUESTIONS &&
        app.topicGuidanceWords.length >= MIN_TOPIC_GUIDANCE_WORDS
      ) {
        return false;
      }
      return true;
    }
  }
);

export const fetchStudyWords = createAsyncThunk<
  FetchStudyWordsResult,
  FetchStudyWordsArgs,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/fetchStudyWords",
  async ({ refreshToken, interestIds = [], avoidWords = [], englishLevel = DEFAULT_ENGLISH_LEVEL }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ level: englishLevel });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }
      const interestLabels = resolveInterestLabels(interestIds);
      interestLabels.forEach((label) => params.append("interest", label));
      avoidWords
        .map((item) => item.trim())
        .filter((item) => item.length > 0)
        .forEach((item) => params.append("avoidWord", item));

      const response = await apiFetch(`/api/v1/practice/study-words?${params.toString()}`, {
        cache: "no-store"
      });
      const payload = (await readApiJSON(response)) as StudyWordsResponse | null;

      if (!response.ok) {
        return rejectWithValue(apiErrorMessage(payload, "Failed to generate study words."));
      }

      const parsed = parseStudyWordsResponse(payload);
      if (!parsed) {
        return rejectWithValue("Invalid words payload from Ollama.");
      }

      return parsed;
    } catch {
      return rejectWithValue("Cannot connect to local Ollama. Make sure Ollama is running.");
    }
  },
  {
    condition: ({ force, interestIds = [], englishLevel = DEFAULT_ENGLISH_LEVEL }, { getState }) => {
      if (force) {
        return true;
      }

      const { app } = getState();
      if (app.studyStatus === "loading") {
        return false;
      }

      const interestKey = buildInterestsKey(interestIds);
      if (
        app.studyStatus === "ready" &&
        app.studyWords.length === 10 &&
        app.studyText &&
        app.studyInterestsKey === interestKey &&
        app.studyEnglishLevel === englishLevel
      ) {
        return false;
      }

      return true;
    }
  }
);

export const restoreSession = createAsyncThunk<
  { email: string | null; isSubscriber: boolean; englishLevel: EnglishLevel },
  void,
  { rejectValue: string }
>("app/restoreSession", async (_, { rejectWithValue }) => {
  try {
    const identity = await restoreBrowserIdentity();
    if (identity.kind !== "user" || !identity.user) {
      return { email: null, isSubscriber: false, englishLevel: DEFAULT_ENGLISH_LEVEL };
    }
    return {
      email: identity.user.email,
      isSubscriber: identity.user.isSubscriber,
      englishLevel: normalizeEnglishLevel(identity.user.englishLevel),
    };
  } catch (error) {
    if (error instanceof IdentityError && new Set([
      "invalid_refresh_token", "refresh_token_expired", "refresh_token_reused",
    ]).has(error.code)) {
      return { email: null, isSubscriber: false, englishLevel: DEFAULT_ENGLISH_LEVEL };
    }
    return rejectWithValue("Cannot connect to authentication service.");
  }
});

export const signIn = createAsyncThunk<
  AuthResult,
  void | { promoteGuest?: boolean },
  { state: { app: AppState }; rejectValue: string }
>(
  "app/signIn",
  async (options, { getState, rejectWithValue }) => {
    const { authEmailDraft, authPasswordDraft } = getState().app;
    const email = authEmailDraft.trim().toLowerCase();
    const password = authPasswordDraft.trim();

    if (!EMAIL_PATTERN.test(email)) {
      return rejectWithValue("Enter a valid email address.");
    }

    if (password.length < MIN_PASSWORD_LENGTH) {
      return rejectWithValue(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
    }

    try {
      const identity = await authenticateBrowserIdentity("signIn", email, password, options?.promoteGuest === true);
      if (!identity.user) return rejectWithValue("Invalid identity payload.");
      if (options?.promoteGuest) await completeGuestPromotion();
      return {
        email: identity.user.email,
        isSubscriber: identity.user.isSubscriber,
        englishLevel: normalizeEnglishLevel(identity.user.englishLevel),
        guestPreviewPromotion: identity.guestPreviewPromotion,
      };
    } catch (error) {
      if (error instanceof IdentityError || error instanceof GuestPreviewError) return rejectWithValue(error.message);
      return rejectWithValue("Cannot connect to authentication service.");
    }
  }
);

export const signUp = createAsyncThunk<
  AuthResult,
  void | { promoteGuest?: boolean },
  { state: { app: AppState }; rejectValue: string }
>(
  "app/signUp",
  async (options, { getState, rejectWithValue }) => {
    const { authEmailDraft, authPasswordDraft } = getState().app;
    const email = authEmailDraft.trim().toLowerCase();
    const password = authPasswordDraft.trim();

    if (!EMAIL_PATTERN.test(email)) {
      return rejectWithValue("Enter a valid email address.");
    }

    if (password.length < MIN_PASSWORD_LENGTH) {
      return rejectWithValue(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
    }

    try {
      const identity = await authenticateBrowserIdentity("signUp", email, password, options?.promoteGuest === true);
      if (!identity.user) return rejectWithValue("Invalid identity payload.");
      if (options?.promoteGuest) await completeGuestPromotion();
      return {
        email: identity.user.email,
        isSubscriber: identity.user.isSubscriber,
        englishLevel: normalizeEnglishLevel(identity.user.englishLevel),
        guestPreviewPromotion: identity.guestPreviewPromotion,
      };
    } catch (error) {
      if (error instanceof IdentityError || error instanceof GuestPreviewError) return rejectWithValue(error.message);
      return rejectWithValue("Cannot connect to authentication service.");
    }
  }
);

export const logout = createAsyncThunk("app/logout", async () => {
  try {
    await logoutBrowserIdentity();
  } catch {
    // Network failures should not block local logout.
  }
});

export const fetchUserData = createAsyncThunk<
  {
    interestIds: string[];
    recordings: Recording[];
    quota: RecordingQuota | null;
    subscription: SubscriptionState | null;
    englishLevel: EnglishLevel;
  },
  void,
  { rejectValue: string }
>("app/fetchUserData", async (_, { rejectWithValue }) => {
  try {
    const response = await apiFetch("/api/v1/profile", {
      cache: "no-store"
    });
    const payload = (await readApiJSON(response)) as UserDataResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(apiErrorMessage(payload, "Failed to load user data."));
    }

    const recordingsResponse = await apiFetch("/api/v1/recordings?limit=100", {
      cache: "no-store",
    });
    const recordingsPayload = (await readApiJSON(recordingsResponse)) as RecordingPageResponse | null;
    if (recordingsResponse.status === 401) {
      return rejectWithValue("Unauthorized");
    }
    if (!recordingsResponse.ok) {
      return rejectWithValue(
        typeof recordingsPayload?.error?.message === "string"
          ? recordingsPayload.error.message
          : "Failed to load recordings.",
      );
    }

    const interestIds = normalizeInterestIds(payload?.interestIds);
    const recordingsRaw = Array.isArray(recordingsPayload?.items) ? recordingsPayload.items : [];
    const recordings = recordingsRaw
      .map((item) => parseRecording(item))
      .filter((item): item is Recording => item !== null)
      .sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
    const quota = parseRecordingQuota(payload?.quota);
    const subscription = parseSubscriptionState(payload?.subscription);
    const englishLevel = normalizeEnglishLevel(payload?.englishLevel, DEFAULT_ENGLISH_LEVEL);

    return { interestIds, recordings, quota, subscription, englishLevel };
  } catch {
    return rejectWithValue("Cannot connect to user data service.");
  }
});

export const saveInterests = createAsyncThunk<string[], void, { state: { app: AppState }; rejectValue: string }>(
  "app/saveInterests",
  async (_, { getState, rejectWithValue }) => {
    const { selectedInterestIds, isAuthenticated } = getState().app;

    if (!isAuthenticated) {
      return rejectWithValue("Unauthorized");
    }

    try {
      const response = await apiFetch("/api/v1/profile/interests", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ interestIds: selectedInterestIds })
      });

      const payload = (await readApiJSON(response)) as SaveInterestsResponse | null;

      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }

      if (!response.ok) {
        return rejectWithValue(apiErrorMessage(payload, "Failed to save interests."));
      }

      return normalizeInterestIds(payload?.interestIds);
    } catch {
      return rejectWithValue("Cannot connect to user data service.");
    }
  }
);

export const saveRecording = createAsyncThunk<
  { recording: Recording; quota: RecordingQuota | null },
  RecordingSaveDraft | void,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/saveRecording",
  async (draftArg, { getState, rejectWithValue }) => {
    const draft = typeof draftArg === "object" && draftArg !== null ? draftArg : null;
    const {
      isAuthenticated,
      speakState,
      selectedTopic,
      recordingPracticeType,
      pendingRecordingAudioDataUrl,
      pendingPhotoDataUrl,
      pendingPhotoObjectDraft,
      recordingDuration,
      isSubscriber,
      weeklyRemainingSeconds,
      maxSessionSeconds
    } = getState().app;

    if (!draft && speakState !== "recorded") {
      return rejectWithValue("Recording is not ready to save.");
    }

    if (!isAuthenticated) {
      return rejectWithValue("Unauthorized");
    }

    const audioDataUrl = normalizeAudioDataUrl(draft ? draft.audioDataUrl : pendingRecordingAudioDataUrl);
    if (!audioDataUrl) {
      return rejectWithValue("Record your voice first.");
    }

    const normalizedDuration = Math.max(0, Math.floor(draft ? draft.duration : recordingDuration));
    const normalizedMaxSession = Math.max(0, Math.floor(maxSessionSeconds));
    const normalizedWeeklyRemaining = Math.max(0, Math.floor(weeklyRemainingSeconds ?? 0));

    if (isSubscriber) {
      if (normalizedDuration > normalizedMaxSession) {
        return rejectWithValue(`Subscribers can save recordings up to ${formatTime(normalizedMaxSession)} per session.`);
      }
    } else if (normalizedDuration > normalizedWeeklyRemaining) {
      return rejectWithValue(
        `Weekly free limit exceeded. You have ${formatTime(normalizedWeeklyRemaining)} left this week.`
      );
    }

    const practiceType = draft ? draft.practiceType : recordingPracticeType;
    const normalizedPhotoObject = draft
      ? (draft.photoObject ?? "").trim().replace(/\s+/g, " ").slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH)
      : pendingPhotoObjectDraft.trim().replace(/\s+/g, " ").slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH);
    const photoObject = normalizedPhotoObject || null;
    const photoDataUrl = practiceType === "photo_description" ? (draft ? draft.photoDataUrl : pendingPhotoDataUrl) : null;

    if (practiceType === "photo_description" && !photoDataUrl) {
      return rejectWithValue("Upload a photo before saving this practice.");
    }

    const topic = draft?.topic.trim()
      ? draft.topic.trim()
      : practiceType === "photo_description"
        ? photoObject
          ? `Photo description: ${photoObject}`
          : "Photo description"
        : selectedTopic ?? "Free talk";

    const recordingDraft = {
      topic,
      duration: normalizedDuration,
      timestamp: draft?.timestamp || new Date().toISOString(),
      practiceType,
      audioDataUrl,
      photoDataUrl,
      photoObject
    };

    try {
      const operationID = draft?.localRecordingId?.trim() || recordingDraft.timestamp;
      const audioAssetId = await uploadMedia({
        blob: dataURLToBlob(audioDataUrl),
        purpose: "recording_audio",
        idempotencyKey: `web-recording:${operationID}:audio`,
      });
      const photoAssetId = photoDataUrl
        ? await uploadMedia({
            blob: dataURLToBlob(photoDataUrl),
            purpose: "recording_photo",
            idempotencyKey: `web-recording:${operationID}:photo`,
          })
        : null;
      const response = await apiFetch("/api/v1/recordings", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": `web-recording:${operationID}`,
        },
        body: JSON.stringify({
          topic: recordingDraft.topic,
          duration: recordingDraft.duration,
          timestamp: recordingDraft.timestamp,
          practiceType: recordingDraft.practiceType,
          audioAssetId,
          ...(draft?.interviewSessionId ? { interviewSessionId: draft.interviewSessionId } : {}),
          photoAssetId,
          photoObject: recordingDraft.photoObject,
        }),
      });
      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }

      const payload = (await readApiJSON(response)) as SaveRecordingResponse | null;

      if (!response.ok) {
        const error = payload?.error;
        return rejectWithValue(
          typeof error === "string"
            ? error
            : typeof error?.message === "string"
              ? error.message
              : "Failed to save recording.",
        );
      }

      const recording = parseRecording(payload?.recording);
      if (!recording) {
        return rejectWithValue("Invalid recording payload from server.");
      }
      if (draft?.interviewSessionId) {
        try {
          await finalizeInterview(
            draft.interviewSessionId,
            Math.max(0, Math.floor(draft.interviewEndedAtMs ?? normalizedDuration * 1000)),
            { recordingId: recording.id },
            `interview:${draft.interviewSessionId}:finalize:${operationID}`,
          );
        } catch {
          return rejectWithValue("Recording saved, but the interview timeline could not be attached. Retry saving to finish it.");
        }
      }
      const quota = parseRecordingQuota(payload?.quota);

      return { recording, quota };
    } catch (error) {
      if (error instanceof MediaUploadError && error.code === "unauthorized") {
        return rejectWithValue("Unauthorized");
      }
      return rejectWithValue(
        error instanceof MediaUploadError ? error.message : "Cannot connect to user data service.",
      );
    }
  }
);

export const fetchRecording = createAsyncThunk<Recording, string, {
  state: { app: AppState };
  rejectValue: string;
  rejectedMeta: { failureKind: "transient" | "terminal" | "unauthorized" };
}>(
  "app/fetchRecording",
  async (recordingId, { rejectWithValue }) => {
    try {
      const response = await apiFetch(`/api/v1/recordings/${encodeURIComponent(recordingId)}`, {
        cache: "no-store"
      });
      if (response.status === 401) {
        return rejectWithValue("Unauthorized", { failureKind: "unauthorized" });
      }
      const failureKind = response.status === 403 || response.status === 404 ? "terminal" : "transient";
      const payload = (await readApiJSON(response).catch(() => null)) as {
        recording?: unknown;
        error?: { message?: unknown };
      } | null;
      if (!response.ok) {
        return rejectWithValue(
          typeof payload?.error?.message === "string" ? payload.error.message : "Failed to load recording.",
          { failureKind },
        );
      }
      const recording = parseRecording(payload?.recording);
      if (!recording) {
        return rejectWithValue("Invalid recording payload from server.", { failureKind: "transient" });
      }
      return recording;
    } catch {
      return rejectWithValue("Cannot connect to recording service.", { failureKind: "transient" });
    }
  },
  {
    condition: (recordingId, { getState }) => {
      const state = getState().app;
      return state.isAuthenticated && !recordingId.startsWith("local-") && !state.deletedRecordingIds.includes(recordingId)
        && state.recordingFetchStatuses[recordingId] !== "loading";
    },
  }
);

export const generateShadowingAudio = createAsyncThunk<Recording, string, { rejectValue: string }>(
  "app/generateShadowingAudio",
  async (recordingId, { rejectWithValue }) => {
    try {
      const response = await apiFetch(`/api/v1/recordings/${encodeURIComponent(recordingId)}/shadowing`, {
        method: "POST",
      });
      const payload = (await readApiJSON(response)) as {
        recording?: unknown;
      } & V1ErrorResponse | null;
      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }
      if (!response.ok) {
        return rejectWithValue(
          typeof payload?.error?.message === "string"
            ? payload.error.message
            : "Failed to generate pronunciation audio.",
        );
      }
      const recording = parseRecording(payload?.recording);
      if (!recording) {
        return rejectWithValue("Invalid recording payload from server.");
      }
      return recording;
    } catch {
      return rejectWithValue("Cannot connect to pronunciation service.");
    }
  },
);

export const retryRecordingProcessing = createAsyncThunk<Recording, string, { rejectValue: string }>(
  "app/retryRecordingProcessing",
  async (recordingId, { rejectWithValue }) => {
    try {
      const response = await apiFetch(`/api/v1/recordings/${encodeURIComponent(recordingId)}/retry`, {
        method: "POST",
      });
      const payload = (await readApiJSON(response)) as {
        recording?: unknown;
      } & V1ErrorResponse | null;
      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }
      if (!response.ok) {
        return rejectWithValue(
          typeof payload?.error?.message === "string"
            ? payload.error.message
            : "Failed to retry recording processing.",
        );
      }
      const recording = parseRecording(payload?.recording);
      if (!recording) {
        return rejectWithValue("Invalid recording payload from server.");
      }
      return recording;
    } catch {
      return rejectWithValue("Cannot connect to recording service.");
    }
  },
);

export const deleteRecording = createAsyncThunk<
  { recordingId: string; quota: RecordingQuota | null },
  string,
  { state: { app: AppState }; rejectValue: string }
>("app/deleteRecording", async (recordingId, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  const normalizedRecordingId = recordingId.trim();
  if (!normalizedRecordingId) {
    return rejectWithValue("Recording is missing.");
  }
  if (normalizedRecordingId.startsWith("local-")) {
    return { recordingId: normalizedRecordingId, quota: null };
  }

  try {
    const response = await apiFetch(`/api/v1/recordings/${encodeURIComponent(normalizedRecordingId)}`, {
      method: "DELETE"
    });
    const payload = (await readApiJSON(response)) as DeleteRecordingResponse | null;
    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }
    if (!response.ok) {
      return rejectWithValue(
        typeof payload?.error?.message === "string" ? payload.error.message : "Failed to delete recording.",
      );
    }
    const deletedRecordingId =
      typeof payload?.deletedRecordingId === "string" ? payload.deletedRecordingId.trim() : "";
    if (!deletedRecordingId || deletedRecordingId !== normalizedRecordingId) {
      return rejectWithValue("Invalid recording deletion response from server.");
    }
    return {
      recordingId: deletedRecordingId,
      quota: parseRecordingQuota(payload?.quota)
    };
  } catch {
    return rejectWithValue("Cannot connect to recording service.");
  }
});

export const subscribeMonthly = createAsyncThunk<
  { subscription: SubscriptionState; quota: RecordingQuota | null },
  void,
  { state: { app: AppState }; rejectValue: string }
>("app/subscribeMonthly", async (_, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  try {
    const response = await apiFetch("/api/v1/subscription", {
      method: "POST"
    });
    const payload = (await readApiJSON(response)) as SubscriptionResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(apiErrorMessage(payload, "Failed to activate subscription."));
    }

    const subscription = parseSubscriptionState(payload?.subscription);
    if (!subscription) {
      return rejectWithValue("Invalid subscription payload from server.");
    }
    const quota = parseRecordingQuota(payload?.quota);

    return { subscription, quota };
  } catch {
    return rejectWithValue("Cannot connect to subscription service.");
  }
});

export const cancelSubscription = createAsyncThunk<
  { subscription: SubscriptionState; quota: RecordingQuota | null },
  void,
  { state: { app: AppState }; rejectValue: string }
>("app/cancelSubscription", async (_, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  try {
    const response = await apiFetch("/api/v1/subscription", {
      method: "DELETE"
    });
    const payload = (await readApiJSON(response)) as SubscriptionResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(apiErrorMessage(payload, "Failed to cancel subscription."));
    }

    const subscription = parseSubscriptionState(payload?.subscription);
    if (!subscription) {
      return rejectWithValue("Invalid subscription payload from server.");
    }
    const quota = parseRecordingQuota(payload?.quota);

    return { subscription, quota };
  } catch {
    return rejectWithValue("Cannot connect to subscription service.");
  }
});

export const saveEnglishLevel = createAsyncThunk<
  EnglishLevel,
  EnglishLevel,
  { state: { app: AppState }; rejectValue: string }
>("app/saveEnglishLevel", async (level, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  const normalizedLevel = parseEnglishLevel(level);
  if (!normalizedLevel) {
    return rejectWithValue("English level is invalid.");
  }

  try {
    const response = await apiFetch("/api/v1/profile/english-level", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ level: normalizedLevel })
    });
    const payload = (await readApiJSON(response)) as EnglishLevelResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(apiErrorMessage(payload, "Failed to save English level."));
    }

    return normalizeEnglishLevel(payload?.level, normalizedLevel);
  } catch {
    return rejectWithValue("Cannot connect to English level service.");
  }
});

const initialState: AppState = {
  speakState: "idle",
  selectedTopic: null,
  showQuestions: false,
  showWords: false,
  recordingDuration: 0,
  recordings: [],
  deletedRecordingIds: [],
  currentRecordingId: null,
  backgroundSaveRecordingId: null,
  isPlaying: false,
  playbackPosition: 0,
  topics: [],
  showAddTopicInput: false,
  customTopicDraft: "",
  calendarVisible: false,
  calendarMonth: today.getMonth(),
  calendarYear: today.getFullYear(),
  isAuthenticated: false,
  userEmail: null,
  authEmailDraft: "",
  authPasswordDraft: "",
  authError: null,
  authStatus: "idle",
  authInitialized: false,
  pendingSaveAfterAuth: false,
  pendingAuthSaveDraft: null,
  questionsStatus: "idle",
  questionsError: null,
  questionsDate: null,
  questionsInterestsKey: "",
  questionsEnglishLevel: DEFAULT_ENGLISH_LEVEL,
  topicGuidanceQuestions: [],
  topicGuidanceWords: [],
  topicGuidanceStatus: "idle",
  topicGuidanceError: null,
  topicGuidanceTopic: null,
  topicGuidanceInterestsKey: "",
  topicGuidanceEnglishLevel: DEFAULT_ENGLISH_LEVEL,
  topicGuidanceRequestId: null,
  studyWords: [],
  studyText: "",
  studyStatus: "idle",
  studyError: null,
  studyInterestsKey: "",
  studyEnglishLevel: DEFAULT_ENGLISH_LEVEL,
  recordingPracticeType: "topic",
  pendingRecordingAudioDataUrl: null,
  recordingInputError: null,
  pendingPhotoDataUrl: null,
  pendingPhotoObjectDraft: "",
  pendingPhotoError: null,
  selectedInterestIds: [],
  selectedEnglishLevel: DEFAULT_ENGLISH_LEVEL,
  userDataStatus: "idle",
  userDataError: null,
  recordingSaveStatus: "idle",
  recordingSaveError: null,
  recordingSaveResults: {},
  recordingSaveDrafts: {},
  recordingFetchStatuses: {},
  recordingFetchErrors: {},
  recordingFetchFailureKinds: {},
  recordingDeleteStatus: "idle",
  recordingDeleteError: null,
  recordingRetryStatuses: {},
  recordingRetryErrors: {},
  shadowingRequestStatus: "idle",
  shadowingRequestError: null,
  interestsSaveStatus: "idle",
  interestsSaveError: null,
  isSubscriber: DEFAULT_RECORDING_QUOTA.isSubscriber,
  weeklyLimitSeconds: DEFAULT_RECORDING_QUOTA.weeklyLimitSeconds,
  weeklyUsedSeconds: DEFAULT_RECORDING_QUOTA.weeklyUsedSeconds,
  weeklyRemainingSeconds: DEFAULT_RECORDING_QUOTA.weeklyRemainingSeconds,
  maxSessionSeconds: DEFAULT_RECORDING_QUOTA.maxSessionSeconds,
  subscriptionExpiresAt: DEFAULT_SUBSCRIPTION_STATE.subscriptionExpiresAt,
  subscriptionCancelled: DEFAULT_SUBSCRIPTION_STATE.subscriptionCancelled,
  subscriptionActionStatus: "idle",
  subscriptionActionError: null,
  englishLevelSaveStatus: "idle",
  englishLevelSaveError: null,
};

const resetPlayback = (state: AppState): void => {
  state.isPlaying = false;
  state.playbackPosition = 0;
};

const resetShadowingRequest = (state: AppState): void => {
  state.shadowingRequestStatus = "idle";
  state.shadowingRequestError = null;
};

const resetRecordingRetry = (state: AppState): void => {
  state.recordingRetryStatuses = {};
  state.recordingRetryErrors = {};
};

const clearRecordingRetry = (state: AppState, recordingId: string | null): void => {
  if (!recordingId) {
    return;
  }
  delete state.recordingRetryStatuses[recordingId];
  delete state.recordingRetryErrors[recordingId];
};

const upsertRecording = (state: AppState, recording: Recording): void => {
  if (state.deletedRecordingIds.includes(recording.id)) {
    return;
  }
  const recordingIndex = state.recordings.findIndex((item) => item.id === recording.id);
  if (recordingIndex >= 0) {
    state.recordings[recordingIndex] = recording;
    return;
  }
  state.recordings.unshift(recording);
};

const clearTopicGuidanceState = (state: AppState): void => {
  state.topicGuidanceQuestions = [];
  state.topicGuidanceWords = [];
  state.topicGuidanceStatus = "idle";
  state.topicGuidanceError = null;
  state.topicGuidanceTopic = null;
  state.topicGuidanceInterestsKey = "";
  state.topicGuidanceEnglishLevel = state.selectedEnglishLevel;
  state.topicGuidanceRequestId = null;
};

const clearStudyWordsState = (state: AppState): void => {
  state.studyWords = [];
  state.studyText = "";
  state.studyStatus = "idle";
  state.studyError = null;
  state.studyInterestsKey = "";
  state.studyEnglishLevel = state.selectedEnglishLevel;
};

const openAuthFlow = (state: AppState, pendingSaveAfterAuth: boolean): void => {
  state.authPasswordDraft = "";
  state.authError = null;
  state.authStatus = "idle";
  state.pendingSaveAfterAuth = pendingSaveAfterAuth;
  resetPlayback(state);
};

const applySavedRecording = (state: AppState, recording: Recording, localRecordingId?: string): void => {
  const backgroundSaveRecordingId = localRecordingId ?? state.backgroundSaveRecordingId;
  const isBackgroundSave = Boolean(backgroundSaveRecordingId);
  state.recordings = [
    recording,
    ...state.recordings.filter((item) => item.id !== recording.id && item.id !== backgroundSaveRecordingId)
  ];
  if (isBackgroundSave) {
    if (backgroundSaveRecordingId) {
      state.recordingSaveResults[backgroundSaveRecordingId] = recording.id;
      delete state.recordingSaveDrafts[backgroundSaveRecordingId];
    }
    if (state.pendingAuthSaveDraft?.localRecordingId === localRecordingId) {
      state.pendingAuthSaveDraft = null;
      state.pendingSaveAfterAuth = false;
    }
    if (state.currentRecordingId === backgroundSaveRecordingId) {
      state.currentRecordingId = recording.id;
      const recordingDate = new Date(recording.timestamp);
      state.calendarMonth = recordingDate.getMonth();
      state.calendarYear = recordingDate.getFullYear();
    }
    state.backgroundSaveRecordingId = null;
    state.recordingSaveStatus = "idle";
    state.recordingSaveError = null;
    return;
  }

  state.currentRecordingId = recording.id;
  state.backgroundSaveRecordingId = null;
  state.speakState = "idle";
  state.selectedTopic = null;
  state.showQuestions = false;
  state.showWords = false;
  state.recordingDuration = 0;
  state.showAddTopicInput = false;
  state.customTopicDraft = "";
  const recordingDate = new Date(recording.timestamp);
  state.calendarMonth = recordingDate.getMonth();
  state.calendarYear = recordingDate.getFullYear();
  state.pendingSaveAfterAuth = false;
  state.pendingAuthSaveDraft = null;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.recordingDeleteStatus = "idle";
  state.recordingDeleteError = null;
  clearRecordingRetry(state, recording.id);
  clearRecordingRetry(state, backgroundSaveRecordingId);
  resetShadowingRequest(state);
  state.recordingPracticeType = "topic";
  state.pendingRecordingAudioDataUrl = null;
  state.recordingInputError = null;
  state.pendingPhotoDataUrl = null;
  state.pendingPhotoObjectDraft = "";
  state.pendingPhotoError = null;
  clearTopicGuidanceState(state);
  resetPlayback(state);
};

const applySubscriptionState = (state: AppState, subscription: SubscriptionState | null): void => {
  if (!subscription) {
    state.subscriptionExpiresAt = null;
    state.subscriptionCancelled = false;
    return;
  }

  state.isSubscriber = subscription.isSubscriber;
  state.subscriptionExpiresAt = subscription.subscriptionExpiresAt;
  state.subscriptionCancelled = subscription.isSubscriber ? subscription.subscriptionCancelled : false;
};

const applyRecordingQuotaState = (state: AppState, quota: RecordingQuota | null): void => {
  if (!quota) {
    const fallback = buildDefaultRecordingQuota(state.isSubscriber);
    state.weeklyLimitSeconds = fallback.weeklyLimitSeconds;
    state.weeklyUsedSeconds = fallback.weeklyUsedSeconds;
    state.weeklyRemainingSeconds = fallback.weeklyRemainingSeconds;
    state.maxSessionSeconds = fallback.maxSessionSeconds;
    return;
  }

  state.isSubscriber = quota.isSubscriber;
  state.weeklyLimitSeconds = quota.weeklyLimitSeconds;
  state.weeklyUsedSeconds = quota.weeklyUsedSeconds;
  state.weeklyRemainingSeconds = quota.weeklyRemainingSeconds;
  state.maxSessionSeconds = quota.maxSessionSeconds;
};

const resolveCurrentSessionLimit = (state: AppState): number => {
  if (!state.isAuthenticated) {
    return MAX_GUEST_PREVIEW_SECONDS;
  }
  if (state.isSubscriber) {
    return Math.max(0, state.maxSessionSeconds);
  }

  const weeklyRemaining = Math.max(0, state.weeklyRemainingSeconds ?? 0);
  return Math.min(Math.max(0, state.maxSessionSeconds), weeklyRemaining);
};

const completeAuthSuccess = (
  state: AppState,
  email: string,
  isSubscriber: boolean,
  englishLevel: EnglishLevel
): void => {
  state.isAuthenticated = true;
  state.userEmail = email;
  state.isSubscriber = isSubscriber;
  state.selectedEnglishLevel = englishLevel;
  state.authPasswordDraft = "";
  state.authError = null;
  state.authStatus = "idle";
  state.authInitialized = true;
  state.userDataStatus = "idle";
  state.userDataError = null;
  state.interestsSaveStatus = "idle";
  state.interestsSaveError = null;
  state.subscriptionActionStatus = "idle";
  state.subscriptionActionError = null;
  state.englishLevelSaveStatus = "idle";
  state.englishLevelSaveError = null;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.recordingDeleteStatus = "idle";
  state.recordingDeleteError = null;
  resetRecordingRetry(state);
  resetShadowingRequest(state);
  if (!state.pendingSaveAfterAuth && !state.pendingRecordingAudioDataUrl && state.speakState !== "recording") {
    state.pendingRecordingAudioDataUrl = null;
    state.recordingInputError = null;
    state.pendingPhotoError = null;
  }
  state.selectedInterestIds = [];
  state.questionsEnglishLevel = englishLevel;
  state.recordings = [];
  state.deletedRecordingIds = [];
  state.backgroundSaveRecordingId = null;
  state.currentRecordingId = null;
  state.recordingFetchStatuses = {};
  state.recordingFetchErrors = {};
  state.recordingFetchFailureKinds = {};
  state.recordingSaveResults = {};
  state.recordingSaveDrafts = {};
  applySubscriptionState(state, {
    isSubscriber,
    subscriptionExpiresAt: null,
    subscriptionCancelled: false
  });
  applyRecordingQuotaState(state, buildDefaultRecordingQuota(isSubscriber));
  clearStudyWordsState(state);
};

const clearAuthenticatedState = (state: AppState): void => {
  state.isAuthenticated = false;
  state.userEmail = null;
  state.authPasswordDraft = "";
  state.authError = null;
  state.authStatus = "idle";
  state.authInitialized = true;
  state.pendingSaveAfterAuth = false;
  state.pendingAuthSaveDraft = null;
  state.selectedInterestIds = [];
  state.questionsInterestsKey = "";
  state.questionsDate = null;
  state.questionsEnglishLevel = DEFAULT_ENGLISH_LEVEL;
  state.topics = [];
  state.questionsStatus = "idle";
  state.questionsError = null;
  state.recordingPracticeType = "topic";
  state.pendingRecordingAudioDataUrl = null;
  state.recordingInputError = null;
  state.pendingPhotoDataUrl = null;
  state.pendingPhotoObjectDraft = "";
  state.pendingPhotoError = null;
  state.selectedEnglishLevel = DEFAULT_ENGLISH_LEVEL;
  state.recordings = [];
  state.deletedRecordingIds = [];
  state.backgroundSaveRecordingId = null;
  state.currentRecordingId = null;
  state.recordingFetchStatuses = {};
  state.recordingFetchErrors = {};
  state.recordingFetchFailureKinds = {};
  state.recordingSaveResults = {};
  state.recordingSaveDrafts = {};
  state.userDataStatus = "idle";
  state.userDataError = null;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.recordingDeleteStatus = "idle";
  state.recordingDeleteError = null;
  resetRecordingRetry(state);
  resetShadowingRequest(state);
  state.interestsSaveStatus = "idle";
  state.interestsSaveError = null;
  state.subscriptionActionStatus = "idle";
  state.subscriptionActionError = null;
  state.englishLevelSaveStatus = "idle";
  state.englishLevelSaveError = null;
  state.isSubscriber = false;
  state.subscriptionExpiresAt = DEFAULT_SUBSCRIPTION_STATE.subscriptionExpiresAt;
  state.subscriptionCancelled = DEFAULT_SUBSCRIPTION_STATE.subscriptionCancelled;
  applyRecordingQuotaState(state, DEFAULT_RECORDING_QUOTA);
  clearTopicGuidanceState(state);
  clearStudyWordsState(state);
  resetPlayback(state);
};

// Session expiry is different from an intentional logout: keep unsent work for re-authentication.
const expireSessionKeepingDraft = (state: AppState): void => {
  const retryDraft = state.pendingAuthSaveDraft
    ?? (state.currentRecordingId ? state.recordingSaveDrafts[state.currentRecordingId] : null)
    ?? Object.values(state.recordingSaveDrafts)[0]
    ?? null;
  const draft = {
    speakState: state.speakState,
    selectedTopic: state.selectedTopic,
    recordingDuration: state.recordingDuration,
    recordingPracticeType: state.recordingPracticeType,
    pendingRecordingAudioDataUrl: state.pendingRecordingAudioDataUrl,
    pendingPhotoDataUrl: state.pendingPhotoDataUrl,
    pendingPhotoObjectDraft: state.pendingPhotoObjectDraft,
    recordingInputError: state.recordingInputError,
    pendingPhotoError: state.pendingPhotoError,
    pendingSaveAfterAuth: state.pendingSaveAfterAuth || retryDraft !== null,
    pendingAuthSaveDraft: retryDraft,
    recordingSaveError: state.recordingSaveError,
    authEmailDraft: state.authEmailDraft || state.userEmail || "",
  };
  clearAuthenticatedState(state);
  Object.assign(state, draft);
  state.authError = "Your session expired. Sign in again.";
};

const appSlice = createSlice({
  name: "app",
  initialState,
  reducers: {
    clearQuestionsError: (state) => {
      state.questionsError = null;
    },
    clearTopicGuidanceError: (state) => {
      state.topicGuidanceError = null;
    },
    clearStudyError: (state) => {
      state.studyError = null;
    },
    toggleInterest: (state, action: PayloadAction<string>) => {
      const interestId = action.payload;
      if (!getInterestOption(interestId)) {
        return;
      }

      const currentIndex = state.selectedInterestIds.indexOf(interestId);
      if (currentIndex >= 0) {
        state.selectedInterestIds.splice(currentIndex, 1);
      } else {
        if (state.selectedInterestIds.length >= MAX_SELECTED_INTERESTS) {
          return;
        }
        state.selectedInterestIds.push(interestId);
      }

      state.questionsError = null;
      state.showQuestions = false;
      state.showWords = false;
      state.interestsSaveError = null;
      clearTopicGuidanceState(state);
      clearStudyWordsState(state);
    },
    setPhotoUploadError: (state, action: PayloadAction<string | null>) => {
      state.pendingPhotoError = action.payload;
    },
    setRecordingInputError: (state, action: PayloadAction<string | null>) => {
      state.recordingInputError = action.payload;
    },
    setRecordingAudioDataUrl: (state, action: PayloadAction<string | null>) => {
      const normalized = normalizeAudioDataUrl(action.payload);
      state.pendingRecordingAudioDataUrl = normalized;
      if (action.payload && !normalized) {
        state.recordingInputError = "Audio recording is invalid or too large.";
        return;
      }
      if (normalized) {
        state.recordingInputError = null;
      }
    },
    clearRecordingDeleteError: (state) => {
      state.recordingDeleteError = null;
    },
    finishFailedRecordingSave: (state, action: PayloadAction<string>) => {
      // Keep the local route and its draft available for an explicit retry.
      delete state.recordingSaveResults[action.payload];
    },
    showBackgroundRecordingSave: (state, action: PayloadAction<RecordingSaveDraft>) => {
      const draft = action.payload;
      const localRecordingId = draft.localRecordingId?.trim();
      if (!localRecordingId) {
        return;
      }

      const timestamp = draft.timestamp || new Date().toISOString();
      const recordingDate = new Date(timestamp);
      const recording: Recording = {
        id: localRecordingId,
        topic: draft.topic.trim() || "Free talk",
        duration: Math.max(0, Math.floor(draft.duration)),
        timestamp,
        status: "processing",
        transcript: "",
        interviewTurns: [],
        correctedTranscript: "",
        suggestions: [],
        processingStage: null,
        practiceType: draft.practiceType,
        localAudioDataUrl: normalizeAudioDataUrl(draft.audioDataUrl),
        localPhotoDataUrl: normalizePhotoDataUrl(draft.photoDataUrl),
        photoObject: normalizePhotoObject(draft.photoObject),
        processingError: null,
        shadowingStatus: "pending",
        shadowingError: null,
        shadowingUpdatedAt: timestamp,
        media: null
      };

      state.recordings = [
        recording,
        ...state.recordings.filter((item) => item.id !== localRecordingId && item.id !== state.backgroundSaveRecordingId)
      ];
      state.recordingSaveDrafts[localRecordingId] = { ...draft, localRecordingId };
      state.currentRecordingId = localRecordingId;
      state.backgroundSaveRecordingId = localRecordingId;
      state.calendarMonth = recordingDate.getMonth();
      state.calendarYear = recordingDate.getFullYear();
      state.speakState = "idle";
      state.selectedTopic = null;
      state.showQuestions = false;
      state.showWords = false;
      state.recordingDuration = 0;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.pendingSaveAfterAuth = false;
      state.recordingSaveStatus = "loading";
      state.recordingSaveError = null;
      state.recordingPracticeType = "topic";
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoDataUrl = null;
      state.pendingPhotoObjectDraft = "";
      state.pendingPhotoError = null;
      clearTopicGuidanceState(state);
      resetPlayback(state);
    },
    setPhotoForPractice: (state, action: PayloadAction<string>) => {
      const normalized = action.payload.trim();
      if (!PHOTO_DATA_URL_PATTERN.test(normalized)) {
        state.pendingPhotoError = "Upload a valid image file.";
        return;
      }

      const payload = normalized.split(",", 2)[1] ?? "";
      const padding = payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0;
      const bytes = Math.floor((payload.length * 3) / 4) - padding;
      if (!Number.isFinite(bytes) || bytes <= 0 || bytes > PHOTO_PRACTICE_MAX_BYTES) {
        state.pendingPhotoError = `Photo must be under ${Math.floor(PHOTO_PRACTICE_MAX_BYTES / (1024 * 1024))}MB.`;
        return;
      }

      state.pendingPhotoDataUrl = normalized;
      state.pendingPhotoError = null;
    },
    clearPhotoForPractice: (state) => {
      const shouldResetSession = state.recordingPracticeType === "photo_description";
      state.pendingPhotoDataUrl = null;
      state.pendingPhotoObjectDraft = "";
      state.pendingPhotoError = null;
      if (shouldResetSession) {
        state.selectedTopic = null;
        state.speakState = "idle";
        state.recordingDuration = 0;
        state.pendingRecordingAudioDataUrl = null;
        state.recordingInputError = null;
        state.showQuestions = false;
        state.showWords = false;
        state.recordingPracticeType = "topic";
        clearTopicGuidanceState(state);
      }
    },
    setPhotoObjectDraft: (state, action: PayloadAction<string>) => {
      state.pendingPhotoObjectDraft = action.payload.slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH);
      state.pendingPhotoError = null;
    },
    startPhotoDescription: (state) => {
      if (!state.pendingPhotoDataUrl) {
        state.pendingPhotoError = "Upload a photo first.";
        return;
      }

      const normalizedObject = state.pendingPhotoObjectDraft
        .trim()
        .replace(/\s+/g, " ")
        .slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH);
      state.pendingPhotoObjectDraft = normalizedObject;
      state.recordingPracticeType = "photo_description";
      state.selectedTopic = normalizedObject ? `Photo description: ${normalizedObject}` : "Photo description";
      state.speakState = "readyToRecord";
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoError = null;
      clearTopicGuidanceState(state);
    },
    startFreeTalk: (state) => {
      state.selectedTopic = null;
      state.showQuestions = false;
      state.showWords = false;
      state.speakState = "recording";
      state.recordingDuration = 0;
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.recordingPracticeType = "free_talk";
      state.pendingPhotoError = null;
      clearTopicGuidanceState(state);
    },
    selectTopic: (state, action: PayloadAction<string>) => {
      state.selectedTopic = action.payload;
      state.speakState = "readyToRecord";
      state.showQuestions = false;
      state.showWords = true;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.recordingPracticeType = "topic";
      state.pendingPhotoError = null;
      if (state.topicGuidanceTopic !== action.payload) {
        clearTopicGuidanceState(state);
      }
    },
    toggleQuestions: (state) => {
      state.showQuestions = !state.showQuestions;
    },
    toggleWords: (state) => {
      state.showWords = !state.showWords;
    },
    startRecording: (state) => {
      state.speakState = "recording";
      state.recordingDuration = 0;
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoError = null;
    },
    tickRecording: (state) => {
      if (state.speakState === "recording") {
        const sessionLimit = resolveCurrentSessionLimit(state);
        if (sessionLimit <= 0) {
          state.speakState = "recorded";
          return;
        }

        if (state.recordingDuration < sessionLimit) {
          state.recordingDuration += 1;
        }

        if (state.recordingDuration >= sessionLimit) {
          state.speakState = "recorded";
        }
      }
    },
    stopRecording: (state) => {
      if (state.speakState === "recording") {
        state.speakState = "recorded";
      }
    },
    reRecord: (state) => {
      state.recordingDuration = 0;
      state.speakState = state.selectedTopic ? "readyToRecord" : "idle";
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoError = null;
    },
    backToQuestionsList: (state) => {
      state.selectedTopic = null;
      state.speakState = "idle";
      state.recordingDuration = 0;
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.recordingSaveError = null;
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoError = null;
      state.recordingPracticeType = "topic";
    },
    finishGuestPreviewFlow: (state, action: PayloadAction<string | null>) => {
      state.speakState = "idle";
      state.selectedTopic = null;
      state.recordingDuration = 0;
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = action.payload;
      state.recordingSaveError = null;
      state.pendingPhotoDataUrl = null;
      state.pendingPhotoObjectDraft = "";
      state.pendingPhotoError = null;
      state.pendingSaveAfterAuth = false;
      state.pendingAuthSaveDraft = null;
      state.recordingPracticeType = "topic";
    },
    openAuthForSave: (state) => {
      if (state.isAuthenticated) {
        return;
      }
      openAuthFlow(state, true);
    },
    toggleAddTopicInput: (state) => {
      state.showAddTopicInput = !state.showAddTopicInput;
      if (!state.showAddTopicInput) {
        state.customTopicDraft = "";
      }
    },
    setCustomTopicDraft: (state, action: PayloadAction<string>) => {
      state.customTopicDraft = action.payload;
    },
    useCustomTopic: (state) => {
      const normalized = state.customTopicDraft.trim();
      if (!normalized) {
        return;
      }
      state.selectedTopic = normalized;
      state.speakState = "readyToRecord";
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.recordingPracticeType = "topic";
      state.pendingRecordingAudioDataUrl = null;
      state.recordingInputError = null;
      state.pendingPhotoError = null;
      if (state.topicGuidanceTopic !== normalized) {
        clearTopicGuidanceState(state);
      }
    },
    toggleCalendar: (state) => {
      state.calendarVisible = !state.calendarVisible;
    },
    previousMonth: (state) => {
      if (state.calendarMonth === 0) {
        state.calendarMonth = 11;
        state.calendarYear -= 1;
      } else {
        state.calendarMonth -= 1;
      }
    },
    nextMonth: (state) => {
      if (state.calendarMonth === 11) {
        state.calendarMonth = 0;
        state.calendarYear += 1;
      } else {
        state.calendarMonth += 1;
      }
    },
    selectRecording: (state, action: PayloadAction<string>) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.currentRecordingId = action.payload;
      state.shadowingRequestError = null;
      resetPlayback(state);
    },
    togglePlayback: (state) => {
      state.isPlaying = !state.isPlaying;
    },
    setPlaybackPlaying: (state, action: PayloadAction<boolean>) => {
      state.isPlaying = action.payload;
    },
    tickPlayback: (state) => {
      if (!state.isPlaying) {
        return;
      }
      const recording = state.recordings.find((item) => item.id === state.currentRecordingId);
      if (!recording) {
        state.isPlaying = false;
        state.playbackPosition = 0;
        return;
      }
      if (state.playbackPosition < recording.duration) {
        state.playbackPosition += 1;
      } else {
        state.isPlaying = false;
        state.playbackPosition = 0;
      }
    },
    setPlaybackPosition: (state, action: PayloadAction<number>) => {
      const recording = state.recordings.find((item) => item.id === state.currentRecordingId);
      if (!recording) {
        state.playbackPosition = 0;
        state.isPlaying = false;
        return;
      }
      const nextValue = Math.max(0, Math.min(action.payload, recording.duration));
      state.playbackPosition = nextValue;
    },
    resetPlaybackState: (state) => {
      resetPlayback(state);
    },
    openAuth: (state) => {
      if (state.isAuthenticated) {
        return;
      }
      openAuthFlow(state, false);
    },
    cancelAuth: (state) => {
      state.authEmailDraft = "";
      state.authPasswordDraft = "";
      state.authError = null;
      state.authStatus = "idle";
      state.pendingSaveAfterAuth = false;
      state.pendingAuthSaveDraft = null;
      state.recordingSaveError = null;
    },
    setAuthEmailDraft: (state, action: PayloadAction<string>) => {
      state.authEmailDraft = action.payload;
      state.authError = null;
    },
    setAuthPasswordDraft: (state, action: PayloadAction<string>) => {
      state.authPasswordDraft = action.payload;
      state.authError = null;
    }
  },
  extraReducers: (builder) => {
    builder
      .addCase(restoreSession.pending, (state) => {
        state.authStatus = "loading";
        state.authError = null;
      })
      .addCase(restoreSession.fulfilled, (state, action) => {
        state.authStatus = "idle";
        state.authInitialized = true;
        const email = action.payload.email;
        const isSubscriber = action.payload.isSubscriber;
        const englishLevel = action.payload.englishLevel;

        if (email) {
          state.isAuthenticated = true;
          state.userEmail = email;
          state.isSubscriber = isSubscriber;
          state.selectedEnglishLevel = englishLevel;
          state.questionsEnglishLevel = englishLevel;
          applySubscriptionState(state, {
            isSubscriber,
            subscriptionExpiresAt: null,
            subscriptionCancelled: false
          });
          applyRecordingQuotaState(state, buildDefaultRecordingQuota(isSubscriber));
          state.englishLevelSaveStatus = "idle";
          state.englishLevelSaveError = null;
          state.userDataStatus = "idle";
          state.userDataError = null;
          clearStudyWordsState(state);
          return;
        }

        state.isAuthenticated = false;
        state.userEmail = null;
        state.isSubscriber = false;
        state.selectedEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        state.questionsEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        applySubscriptionState(state, DEFAULT_SUBSCRIPTION_STATE);
        applyRecordingQuotaState(state, DEFAULT_RECORDING_QUOTA);
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.userDataStatus = "idle";
        state.userDataError = null;
      state.selectedInterestIds = [];
      state.recordings = [];
      state.backgroundSaveRecordingId = null;
      clearTopicGuidanceState(state);
      clearStudyWordsState(state);
      })
      .addCase(restoreSession.rejected, (state, action) => {
        state.authStatus = "idle";
        state.authInitialized = true;
        state.authError = action.payload ?? null;
        state.isAuthenticated = false;
        state.userEmail = null;
        state.isSubscriber = false;
        state.selectedEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        state.questionsEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        applySubscriptionState(state, DEFAULT_SUBSCRIPTION_STATE);
        applyRecordingQuotaState(state, DEFAULT_RECORDING_QUOTA);
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.userDataStatus = "idle";
        state.userDataError = null;
      state.selectedInterestIds = [];
      state.recordings = [];
      state.backgroundSaveRecordingId = null;
      clearTopicGuidanceState(state);
      clearStudyWordsState(state);
      })
      .addCase(signIn.pending, (state) => {
        state.authStatus = "loading";
        state.authError = null;
      })
      .addCase(signIn.fulfilled, (state, action) => {
        completeAuthSuccess(state, action.payload.email, action.payload.isSubscriber, action.payload.englishLevel);
      })
      .addCase(signIn.rejected, (state, action) => {
        state.authStatus = "idle";
        state.authError = action.payload ?? "Failed to sign in.";
      })
      .addCase(signUp.pending, (state) => {
        state.authStatus = "loading";
        state.authError = null;
      })
      .addCase(signUp.fulfilled, (state, action) => {
        completeAuthSuccess(state, action.payload.email, action.payload.isSubscriber, action.payload.englishLevel);
      })
      .addCase(signUp.rejected, (state, action) => {
        state.authStatus = "idle";
        state.authError = action.payload ?? "Failed to create account.";
      })
      .addCase(logout.fulfilled, (state) => {
        clearAuthenticatedState(state);
      })
      .addCase(logout.rejected, (state) => {
        clearAuthenticatedState(state);
      })
      .addCase(fetchUserData.pending, (state) => {
        state.userDataStatus = "loading";
        state.userDataError = null;
      })
      .addCase(fetchUserData.fulfilled, (state, action) => {
        state.userDataStatus = "ready";
        state.userDataError = null;
        state.selectedInterestIds = action.payload.interestIds;
        state.selectedEnglishLevel = action.payload.englishLevel;
        // Local recordings are not yet in the server list, including failed saves.
        const localRecordings = state.recordings.filter((recording) => recording.id.startsWith("local-"));
        state.recordings = filterDeletedRecordings([...localRecordings, ...action.payload.recordings], state.deletedRecordingIds);
        applySubscriptionState(state, action.payload.subscription);
        applyRecordingQuotaState(state, action.payload.quota);
        clearStudyWordsState(state);
      })
      .addCase(fetchUserData.rejected, (state, action) => {
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.userDataStatus = "failed";
        state.userDataError = action.payload ?? "Failed to load user data.";
      })
      .addCase(saveInterests.pending, (state) => {
        state.interestsSaveStatus = "loading";
        state.interestsSaveError = null;
      })
      .addCase(saveInterests.fulfilled, (state) => {
        state.interestsSaveStatus = "idle";
        state.interestsSaveError = null;
      })
      .addCase(saveInterests.rejected, (state, action) => {
        state.interestsSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        if (action.payload && action.payload !== "Unauthorized") {
          state.interestsSaveError = action.payload;
        }
      })
      .addCase(saveRecording.pending, (state, action) => {
        state.recordingSaveStatus = "loading";
        state.recordingSaveError = null;
        const localRecordingId = action.meta.arg?.localRecordingId;
        if (localRecordingId) {
          state.backgroundSaveRecordingId = localRecordingId;
          const recording = state.recordings.find((item) => item.id === localRecordingId);
          if (recording) {
            recording.status = "processing";
            recording.processingError = null;
          }
        }
      })
      .addCase(saveRecording.fulfilled, (state, action) => {
        applySavedRecording(state, action.payload.recording, action.meta.arg?.localRecordingId);
        applyRecordingQuotaState(state, action.payload.quota);
      })
      .addCase(saveRecording.rejected, (state, action) => {
        state.recordingSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          state.pendingSaveAfterAuth = true;
          if (action.meta.arg) {
            // Keep a failed background save separate from any newer speaking session.
            state.pendingAuthSaveDraft = action.meta.arg;
          }
          state.recordingSaveError = "Your session expired. Sign in again to save your recording.";
          expireSessionKeepingDraft(state);
          return;
        }
        if (action.payload) {
          const backgroundSaveRecordingId = action.meta.arg?.localRecordingId ?? state.backgroundSaveRecordingId;
          if (backgroundSaveRecordingId) {
            state.recordings = state.recordings.map((item) =>
              item.id === backgroundSaveRecordingId
                ? { ...item, status: "failed", processingError: action.payload ?? "Failed to save recording." }
                : item
            );
            state.backgroundSaveRecordingId = null;
          }
          state.recordingSaveError = action.payload;
        }
      })
      .addCase(fetchRecording.pending, (state, action) => {
        state.recordingFetchStatuses[action.meta.arg] = "loading";
        delete state.recordingFetchErrors[action.meta.arg];
        delete state.recordingFetchFailureKinds[action.meta.arg];
      })
      .addCase(fetchRecording.fulfilled, (state, action) => {
        if (!state.isAuthenticated) return;
        state.recordingFetchStatuses[action.meta.arg] = "ready";
        delete state.recordingFetchErrors[action.meta.arg];
        delete state.recordingFetchFailureKinds[action.meta.arg];
        upsertRecording(state, action.payload);
      })
      .addCase(fetchRecording.rejected, (state, action) => {
        if (action.meta.failureKind === "unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        if (!state.isAuthenticated) return;
        state.recordingFetchStatuses[action.meta.arg] = "failed";
        state.recordingFetchErrors[action.meta.arg] = action.payload ?? "Failed to load recording.";
        state.recordingFetchFailureKinds[action.meta.arg] = action.meta.failureKind === "terminal" ? "terminal" : "transient";
      })
      .addCase(retryRecordingProcessing.pending, (state, action) => {
        state.recordingRetryStatuses[action.meta.arg] = "loading";
        delete state.recordingRetryErrors[action.meta.arg];
      })
      .addCase(retryRecordingProcessing.fulfilled, (state, action) => {
        clearRecordingRetry(state, action.meta.arg);
        resetShadowingRequest(state);
        upsertRecording(state, action.payload);
      })
      .addCase(retryRecordingProcessing.rejected, (state, action) => {
        delete state.recordingRetryStatuses[action.meta.arg];
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.recordingRetryErrors[action.meta.arg] = action.payload ?? "Failed to retry recording processing.";
      })
      .addCase(generateShadowingAudio.pending, (state) => {
        state.shadowingRequestStatus = "loading";
        state.shadowingRequestError = null;
      })
      .addCase(generateShadowingAudio.fulfilled, (state, action) => {
        state.shadowingRequestStatus = "idle";
        state.shadowingRequestError = null;
        upsertRecording(state, action.payload);
      })
      .addCase(generateShadowingAudio.rejected, (state, action) => {
        state.shadowingRequestStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.shadowingRequestError = action.payload ?? "Failed to generate pronunciation audio.";
      })
      .addCase(deleteRecording.pending, (state) => {
        state.recordingDeleteStatus = "loading";
        state.recordingDeleteError = null;
      })
      .addCase(deleteRecording.fulfilled, (state, action) => {
        const { recordingId, quota } = action.payload;
        if (!state.deletedRecordingIds.includes(recordingId)) {
          state.deletedRecordingIds.push(recordingId);
        }
        state.recordings = removeRecording(state.recordings, recordingId);
        clearRecordingRetry(state, recordingId);
        if (state.currentRecordingId === recordingId) {
          state.currentRecordingId = null;
        }
        if (state.backgroundSaveRecordingId === recordingId) {
          state.backgroundSaveRecordingId = null;
        }
        state.recordingDeleteStatus = "idle";
        state.recordingDeleteError = null;
        if (quota) {
          applyRecordingQuotaState(state, quota);
        }
        resetPlayback(state);
      })
      .addCase(deleteRecording.rejected, (state, action) => {
        state.recordingDeleteStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.recordingDeleteError = action.payload ?? "Failed to delete recording.";
      })
      .addCase(subscribeMonthly.pending, (state) => {
        state.subscriptionActionStatus = "loading";
        state.subscriptionActionError = null;
      })
      .addCase(subscribeMonthly.fulfilled, (state, action) => {
        state.subscriptionActionStatus = "idle";
        state.subscriptionActionError = null;
        applySubscriptionState(state, action.payload.subscription);
        applyRecordingQuotaState(state, action.payload.quota);
      })
      .addCase(subscribeMonthly.rejected, (state, action) => {
        state.subscriptionActionStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.subscriptionActionError = action.payload ?? "Failed to activate subscription.";
      })
      .addCase(cancelSubscription.pending, (state) => {
        state.subscriptionActionStatus = "loading";
        state.subscriptionActionError = null;
      })
      .addCase(cancelSubscription.fulfilled, (state, action) => {
        state.subscriptionActionStatus = "idle";
        state.subscriptionActionError = null;
        applySubscriptionState(state, action.payload.subscription);
        applyRecordingQuotaState(state, action.payload.quota);
      })
      .addCase(cancelSubscription.rejected, (state, action) => {
        state.subscriptionActionStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.subscriptionActionError = action.payload ?? "Failed to cancel subscription.";
      })
      .addCase(saveEnglishLevel.pending, (state) => {
        state.englishLevelSaveStatus = "loading";
        state.englishLevelSaveError = null;
      })
      .addCase(saveEnglishLevel.fulfilled, (state, action) => {
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.selectedEnglishLevel = action.payload;
        clearStudyWordsState(state);
      })
      .addCase(saveEnglishLevel.rejected, (state, action) => {
        state.englishLevelSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          expireSessionKeepingDraft(state);
          return;
        }
        state.englishLevelSaveError = action.payload ?? "Failed to save English level.";
      })
      .addCase(fetchStudyWords.pending, (state, action) => {
        const interestKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        const englishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;

        if (state.studyInterestsKey !== interestKey || state.studyEnglishLevel !== englishLevel) {
          state.studyWords = [];
          state.studyText = "";
        }

        state.studyInterestsKey = interestKey;
        state.studyEnglishLevel = englishLevel;
        state.studyStatus = "loading";
        state.studyError = null;
      })
      .addCase(fetchStudyWords.fulfilled, (state, action) => {
        state.studyWords = action.payload.words;
        state.studyText = action.payload.text;
        state.studyInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        state.studyEnglishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;
        state.studyStatus = "ready";
        state.studyError = null;
      })
      .addCase(fetchStudyWords.rejected, (state, action) => {
        state.studyStatus = state.studyWords.length === 10 && state.studyText ? "ready" : "failed";
        state.studyError = action.payload ?? "Failed to generate study words.";
      })
      .addCase(fetchDailyQuestions.pending, (state) => {
        state.questionsStatus = "loading";
        state.questionsError = null;
      })
      .addCase(fetchDailyQuestions.fulfilled, (state, action) => {
        state.topics = action.payload.questions;
        state.questionsDate = action.payload.dateKey;
        state.questionsInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        state.questionsEnglishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;
        state.questionsStatus = "ready";
        state.questionsError = null;
      })
      .addCase(fetchDailyQuestions.rejected, (state, action) => {
        state.questionsStatus = state.topics.length > 0 ? "ready" : "failed";
        state.questionsError = action.payload ?? "Failed to generate questions.";
      })
      .addCase(fetchTopicGuidance.pending, (state, action) => {
        const topic = action.meta.arg.topic.trim();
        const interestKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        const englishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;
        if (
          state.topicGuidanceTopic !== topic ||
          state.topicGuidanceInterestsKey !== interestKey ||
          state.topicGuidanceEnglishLevel !== englishLevel
        ) {
          state.topicGuidanceQuestions = [];
          state.topicGuidanceWords = [];
        }
        state.topicGuidanceTopic = topic;
        state.topicGuidanceInterestsKey = interestKey;
        state.topicGuidanceEnglishLevel = englishLevel;
        state.topicGuidanceRequestId = action.meta.requestId;
        state.topicGuidanceStatus = "loading";
        state.topicGuidanceError = null;
      })
      .addCase(fetchTopicGuidance.fulfilled, (state, action) => {
        if (!isCurrentInterviewGuidanceRequest(state.topicGuidanceRequestId, action.meta.requestId)) {
          return;
        }
        state.topicGuidanceTopic = action.payload.topic;
        state.topicGuidanceInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        state.topicGuidanceEnglishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;
        state.topicGuidanceQuestions = action.payload.questions;
        state.topicGuidanceWords = action.payload.words;
        state.topicGuidanceStatus = "ready";
        state.topicGuidanceError = null;
        state.topicGuidanceRequestId = null;
      })
      .addCase(fetchTopicGuidance.rejected, (state, action) => {
        if (!isCurrentInterviewGuidanceRequest(state.topicGuidanceRequestId, action.meta.requestId)) {
          return;
        }
        state.topicGuidanceStatus =
          state.topicGuidanceQuestions.length > 0 || state.topicGuidanceWords.length > 0 ? "ready" : "failed";
        state.topicGuidanceError = action.payload ?? "Failed to generate guidance.";
        state.topicGuidanceRequestId = null;
      });
  }
});

export const {
  clearQuestionsError,
  clearTopicGuidanceError,
  clearStudyError,
  setPhotoUploadError,
  setRecordingInputError,
  setRecordingAudioDataUrl,
  clearRecordingDeleteError,
  showBackgroundRecordingSave,
  finishFailedRecordingSave,
  setPhotoForPractice,
  clearPhotoForPractice,
  setPhotoObjectDraft,
  startPhotoDescription,
  toggleInterest,
  startFreeTalk,
  selectTopic,
  toggleQuestions,
  toggleWords,
  startRecording,
  tickRecording,
  stopRecording,
  reRecord,
  backToQuestionsList,
  finishGuestPreviewFlow,
  openAuthForSave,
  toggleAddTopicInput,
  setCustomTopicDraft,
  useCustomTopic,
  toggleCalendar,
  previousMonth,
  nextMonth,
  selectRecording,
  togglePlayback,
  setPlaybackPlaying,
  tickPlayback,
  setPlaybackPosition,
  resetPlaybackState,
  openAuth,
  cancelAuth,
  setAuthEmailDraft,
  setAuthPasswordDraft
} = appSlice.actions;

export default appSlice.reducer;
