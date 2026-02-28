import { createAsyncThunk, createSlice, type PayloadAction } from "@reduxjs/toolkit";
import { type PracticeType, type Recording, type Suggestion } from "../../lib/data";
import { DEFAULT_ENGLISH_LEVEL, normalizeEnglishLevel, parseEnglishLevel, type EnglishLevel } from "../../lib/englishLevel";
import { formatTime, toDateKey } from "../../lib/utils";

export type ScreenName = "speak" | "history" | "details" | "share" | "auth" | "profile" | "interests";
export type TabName = "speak" | "history";
export type SpeakMode = "idle" | "readyToRecord" | "recording" | "recorded";
export type ShareAction = "copy" | "preview";
export type AuthStatus = "idle" | "loading";
export type QuestionsStatus = "idle" | "loading" | "ready" | "failed";
export type InterestOption = {
  id: string;
  emoji: string;
  label: string;
};

export const INTEREST_OPTIONS: InterestOption[] = [
  { id: "travel", emoji: "✈️", label: "Travel" },
  { id: "technology", emoji: "💻", label: "Technology" },
  { id: "fitness", emoji: "🏃", label: "Fitness" },
  { id: "business", emoji: "📈", label: "Business" },
  { id: "music", emoji: "🎵", label: "Music" },
  { id: "books", emoji: "📚", label: "Books" },
  { id: "movies", emoji: "🎬", label: "Movies" },
  { id: "food", emoji: "🍜", label: "Food" },
  { id: "sport", emoji: "⚽", label: "Sport" },
  { id: "design", emoji: "🎨", label: "Design" },
  { id: "career", emoji: "🚀", label: "Career" },
  { id: "languages", emoji: "🗣️", label: "Languages" },
  { id: "gaming", emoji: "🎮", label: "Gaming" },
  { id: "photography", emoji: "📷", label: "Photography" },
  { id: "cooking", emoji: "👨‍🍳", label: "Cooking" },
  { id: "psychology", emoji: "🧠", label: "Psychology" },
  { id: "startups", emoji: "💡", label: "Startups" },
  { id: "marketing", emoji: "📣", label: "Marketing" },
  { id: "productivity", emoji: "⏱️", label: "Productivity" },
  { id: "ai", emoji: "🤖", label: "AI" },
  { id: "science", emoji: "🔬", label: "Science" },
  { id: "history", emoji: "🏛️", label: "History" },
  { id: "nature", emoji: "🌿", label: "Nature" },
  { id: "hiking", emoji: "🥾", label: "Hiking" },
  { id: "cycling", emoji: "🚴", label: "Cycling" },
  { id: "swimming", emoji: "🏊", label: "Swimming" },
  { id: "yoga", emoji: "🧘", label: "Yoga" },
  { id: "fashion", emoji: "👗", label: "Fashion" },
  { id: "art", emoji: "🖼️", label: "Art" },
  { id: "architecture", emoji: "🏗️", label: "Architecture" },
  { id: "finance", emoji: "💰", label: "Finance" },
  { id: "investing", emoji: "📊", label: "Investing" },
  { id: "crypto", emoji: "₿", label: "Crypto" },
  { id: "pets", emoji: "🐶", label: "Pets" },
  { id: "parenting", emoji: "👨‍👩‍👧", label: "Parenting" },
  { id: "education", emoji: "🎓", label: "Education" },
  { id: "philosophy", emoji: "📖", label: "Philosophy" },
  { id: "self-development", emoji: "🌱", label: "Self Development" },
  { id: "culture", emoji: "🎭", label: "Culture" },
  { id: "volunteering", emoji: "🤝", label: "Volunteering" },
  { id: "entrepreneurship", emoji: "🏢", label: "Entrepreneurship" },
  { id: "public-speaking", emoji: "🎤", label: "Public Speaking" },
  { id: "remote-work", emoji: "🏠", label: "Remote Work" },
  { id: "health", emoji: "❤️", label: "Health" },
  { id: "mindfulness", emoji: "🕊️", label: "Mindfulness" },
  { id: "news", emoji: "📰", label: "News" },
  { id: "podcasts", emoji: "🎙️", label: "Podcasts" },
  { id: "climate", emoji: "🌍", label: "Climate" },
  { id: "sustainability", emoji: "♻️", label: "Sustainability" },
  { id: "astronomy", emoji: "🔭", label: "Astronomy" },
  { id: "space", emoji: "🛰️", label: "Space" },
  { id: "robotics", emoji: "🦾", label: "Robotics" },
  { id: "programming", emoji: "⌨️", label: "Programming" },
  { id: "web-development", emoji: "🌐", label: "Web Development" },
  { id: "mobile-development", emoji: "📱", label: "Mobile Development" },
  { id: "cybersecurity", emoji: "🛡️", label: "Cybersecurity" },
  { id: "data-science", emoji: "📉", label: "Data Science" },
  { id: "machine-learning", emoji: "🧩", label: "Machine Learning" },
  { id: "mathematics", emoji: "➗", label: "Mathematics" },
  { id: "physics", emoji: "⚛️", label: "Physics" },
  { id: "chemistry", emoji: "🧪", label: "Chemistry" },
  { id: "biology", emoji: "🧬", label: "Biology" },
  { id: "medicine", emoji: "🩺", label: "Medicine" },
  { id: "nutrition", emoji: "🥗", label: "Nutrition" },
  { id: "mental-health", emoji: "💚", label: "Mental Health" },
  { id: "journaling", emoji: "📝", label: "Journaling" },
  { id: "minimalism", emoji: "🧱", label: "Minimalism" },
  { id: "home-decor", emoji: "🛋️", label: "Home Decor" },
  { id: "gardening", emoji: "🌻", label: "Gardening" },
  { id: "diy", emoji: "🛠️", label: "DIY" },
  { id: "woodworking", emoji: "🪵", label: "Woodworking" },
  { id: "cars", emoji: "🚗", label: "Cars" },
  { id: "motorcycles", emoji: "🏍️", label: "Motorcycles" },
  { id: "aviation", emoji: "🛫", label: "Aviation" },
  { id: "sailing", emoji: "⛵", label: "Sailing" },
  { id: "chess", emoji: "♟️", label: "Chess" },
  { id: "board-games", emoji: "🎲", label: "Board Games" },
  { id: "card-games", emoji: "🃏", label: "Card Games" },
  { id: "dance", emoji: "💃", label: "Dance" },
  { id: "theater", emoji: "🎭", label: "Theater" },
  { id: "comedy", emoji: "😂", label: "Comedy" },
  { id: "writing", emoji: "✍️", label: "Writing" },
  { id: "poetry", emoji: "📜", label: "Poetry" },
  { id: "language-teaching", emoji: "🧑‍🏫", label: "Language Teaching" },
  { id: "backpacking", emoji: "🎒", label: "Backpacking" },
  { id: "luxury-travel", emoji: "🏝️", label: "Luxury Travel" },
  { id: "coffee", emoji: "☕", label: "Coffee" },
  { id: "tea", emoji: "🍵", label: "Tea" },
  { id: "baking", emoji: "🥐", label: "Baking" },
  { id: "desserts", emoji: "🍰", label: "Desserts" },
  { id: "street-food", emoji: "🌮", label: "Street Food" },
  { id: "vegan-living", emoji: "🥦", label: "Vegan Living" },
  { id: "interior-design", emoji: "🪞", label: "Interior Design" },
  { id: "real-estate", emoji: "🏘️", label: "Real Estate" },
  { id: "law", emoji: "⚖️", label: "Law" },
  { id: "economics", emoji: "🏦", label: "Economics" },
  { id: "geopolitics", emoji: "🗺️", label: "Geopolitics" },
  { id: "social-media", emoji: "📲", label: "Social Media" },
  { id: "content-creation", emoji: "🎥", label: "Content Creation" },
  { id: "audio-production", emoji: "🎛️", label: "Audio Production" }
];

export type AppState = {
  currentScreen: ScreenName;
  activeTab: TabName;
  speakState: SpeakMode;
  selectedTopic: string | null;
  showQuestions: boolean;
  showWords: boolean;
  recordingDuration: number;
  recordings: Recording[];
  selectedDate: string | null;
  currentRecordingId: string | null;
  isPlaying: boolean;
  playbackPosition: number;
  topics: string[];
  showAddTopicInput: boolean;
  customTopicDraft: string;
  calendarVisible: boolean;
  calendarMonth: number;
  calendarYear: number;
  shareModalOpen: boolean;
  shareAction: ShareAction;
  copyMessage: string | null;
  isAuthenticated: boolean;
  userEmail: string | null;
  authEmailDraft: string;
  authPasswordDraft: string;
  authError: string | null;
  authStatus: AuthStatus;
  authInitialized: boolean;
  pendingSaveAfterAuth: boolean;
  screenBeforeAuth: TabName;
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
  recordingPracticeType: PracticeType;
  pendingPhotoDataUrl: string | null;
  pendingPhotoObjectDraft: string;
  pendingPhotoError: string | null;
  selectedInterestIds: string[];
  selectedEnglishLevel: EnglishLevel;
  userDataStatus: QuestionsStatus;
  userDataError: string | null;
  recordingSaveStatus: AuthStatus;
  recordingSaveError: string | null;
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
  selectedOllamaModel: string;
  availableOllamaModels: string[];
  ollamaModelsStatus: QuestionsStatus;
  ollamaModelsError: string | null;
  ollamaModelSaveStatus: AuthStatus;
  ollamaModelSaveError: string | null;
  englishLevelSaveStatus: AuthStatus;
  englishLevelSaveError: string | null;
};

const today = new Date();
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const MIN_PASSWORD_LENGTH = 8;
export const MAX_SELECTED_INTERESTS = 10;
const FREE_WEEKLY_LIMIT_SECONDS = 10 * 60;
const SESSION_LIMIT_SECONDS = 10 * 60;
const DEFAULT_OLLAMA_MODEL = "gemma3:12b";
const PHOTO_PRACTICE_MAX_OBJECT_LENGTH = 120;
export const PHOTO_PRACTICE_MAX_BYTES = 4 * 1024 * 1024;
const PHOTO_DATA_URL_PATTERN = /^data:image\/(png|jpeg|jpg|webp|gif);base64,([A-Za-z0-9+/=]+)$/i;
const PRACTICE_TYPE_SET = new Set<PracticeType>(["free_talk", "topic", "photo_description"]);

const INTEREST_LOOKUP = new Map(INTEREST_OPTIONS.map((item) => [item.id, item]));

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
  error?: string;
};

type FetchTopicGuidanceArgs = {
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
  error?: string;
};

type AuthUserPayload = {
  email?: unknown;
  isSubscriber?: unknown;
  englishLevel?: unknown;
};

type AuthResponse = {
  user?: AuthUserPayload;
  error?: string;
};

type UserDataResponse = {
  interestIds?: unknown;
  recordings?: unknown;
  quota?: unknown;
  subscription?: unknown;
  englishLevel?: unknown;
  error?: string;
};

type SaveInterestsResponse = {
  interestIds?: unknown;
  error?: string;
};

type SaveRecordingResponse = {
  recording?: unknown;
  quota?: unknown;
  error?: string;
};

type SubscriptionResponse = {
  subscription?: unknown;
  quota?: unknown;
  error?: string;
};

type OllamaModelResponse = {
  selectedModel?: unknown;
  availableModels?: unknown;
  warning?: unknown;
  error?: string;
};

type EnglishLevelResponse = {
  level?: unknown;
  error?: string;
};

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

type OllamaModelState = {
  selectedModel: string;
  availableModels: string[];
  warning: string | null;
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

const DEFAULT_OLLAMA_MODEL_STATE: OllamaModelState = {
  selectedModel: DEFAULT_OLLAMA_MODEL,
  availableModels: [DEFAULT_OLLAMA_MODEL],
  warning: null
};

const parseAuthUser = (payload: AuthResponse | null): { email: string; isSubscriber: boolean; englishLevel: EnglishLevel } | null => {
  const email = payload?.user?.email;
  if (typeof email !== "string") {
    return null;
  }

  const normalized = email.trim().toLowerCase();
  if (!EMAIL_PATTERN.test(normalized)) {
    return null;
  }

  return {
    email: normalized,
    isSubscriber: Boolean(payload?.user?.isSubscriber),
    englishLevel: normalizeEnglishLevel(payload?.user?.englishLevel, DEFAULT_ENGLISH_LEVEL)
  };
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

const normalizeModelName = (value: unknown): string | null => {
  if (typeof value !== "string") {
    return null;
  }

  const cleaned = value.trim();
  if (!cleaned || cleaned.length > 120) {
    return null;
  }

  return cleaned;
};

const parseOllamaModelState = (payload: OllamaModelResponse | null): OllamaModelState | null => {
  const selectedModel = normalizeModelName(payload?.selectedModel);
  if (!selectedModel) {
    return null;
  }

  const rawList = Array.isArray(payload?.availableModels) ? payload?.availableModels : [];
  const list = rawList
    .map((item) => normalizeModelName(item))
    .filter((item): item is string => item !== null);
  const availableModels = Array.from(new Set([selectedModel, ...list]));
  const warningRaw = payload?.warning;
  const warning = typeof warningRaw === "string" && warningRaw.trim() ? warningRaw.trim() : null;

  return {
    selectedModel,
    availableModels: availableModels.length > 0 ? availableModels : [selectedModel],
    warning
  };
};

const normalizeInterestIds = (value: unknown): string[] => {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const item of value) {
    if (typeof item !== "string") {
      continue;
    }

    const cleaned = item.trim();
    if (!cleaned || !INTEREST_LOOKUP.has(cleaned)) {
      continue;
    }

    if (seen.has(cleaned)) {
      continue;
    }

    seen.add(cleaned);
    normalized.push(cleaned);

    if (normalized.length >= MAX_SELECTED_INTERESTS) {
      break;
    }
  }

  return normalized;
};

const parseSuggestion = (value: unknown): Suggestion | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const candidate = value as Record<string, unknown>;
  const wrong = typeof candidate.wrong === "string" ? candidate.wrong.trim() : "";
  const right = typeof candidate.right === "string" ? candidate.right.trim() : "";
  const explanation = typeof candidate.explanation === "string" ? candidate.explanation.trim() : "";

  if (!wrong || !right || !explanation) {
    return null;
  }

  return { wrong, right, explanation };
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

const parseRecording = (value: unknown): Recording | null => {
  if (typeof value !== "object" || value === null) {
    return null;
  }

  const candidate = value as Record<string, unknown>;
  const id = typeof candidate.id === "string" ? candidate.id.trim() : "";
  const topic = typeof candidate.topic === "string" ? candidate.topic.trim() : "";
  const transcript = typeof candidate.transcript === "string" ? candidate.transcript : "";
  const timestampRaw = typeof candidate.timestamp === "string" ? candidate.timestamp : "";
  const timestamp = new Date(timestampRaw);
  const duration = Number.parseInt(String(candidate.duration ?? 0), 10);
  const suggestionsRaw = Array.isArray(candidate.suggestions) ? candidate.suggestions : [];
  const suggestions = suggestionsRaw
    .map((item) => parseSuggestion(item))
    .filter((item): item is Suggestion => item !== null)
    .slice(0, 20);
  const photoDataUrl = normalizePhotoDataUrl(candidate.photoDataUrl);
  const photoObject = normalizePhotoObject(candidate.photoObject);
  const practiceType = parsePracticeType(candidate.practiceType, topic, Boolean(photoDataUrl));

  if (!id || !topic || Number.isNaN(timestamp.getTime()) || !Number.isFinite(duration) || duration < 0) {
    return null;
  }

  return {
    id,
    topic,
    duration: Math.max(0, duration),
    timestamp: timestamp.toISOString(),
    transcript,
    suggestions,
    practiceType,
    photoDataUrl,
    photoObject
  };
};

const buildInterestsKey = (interestIds: string[]): string => {
  return [...interestIds].sort().join("|");
};

const resolveInterestLabels = (interestIds: string[]): string[] => {
  const labels = interestIds
    .map((id) => INTEREST_LOOKUP.get(id)?.label)
    .filter((value): value is string => typeof value === "string");

  return [...new Set(labels)];
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

      const response = await fetch(`/api/daily-questions?${params.toString()}`, {
        cache: "no-store"
      });
      const payload = (await response.json().catch(() => null)) as DailyQuestionsResponse | null;

      if (!response.ok) {
        return rejectWithValue(payload?.error ?? "Failed to load daily questions from Ollama.");
      }

      const questions = Array.isArray(payload?.questions)
        ? payload.questions
            .filter((item): item is string => typeof item === "string")
            .map((item) => item.trim())
            .filter((item) => item.length > 0)
        : [];

      if (questions.length !== 3) {
        return rejectWithValue("Ollama must return exactly 3 questions.");
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
        app.topics.length === 3
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
    { rejectWithValue }
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

      const response = await fetch(`/api/topic-guidance?${params.toString()}`, {
        cache: "no-store"
      });
      const payload = (await response.json().catch(() => null)) as TopicGuidanceResponse | null;

      if (!response.ok) {
        return rejectWithValue(payload?.error ?? "Failed to generate questions and useful words.");
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

      if (questions.length === 0 || words.length === 0) {
        return rejectWithValue("Ollama returned empty guidance for this topic.");
      }

      return {
        topic,
        questions: questions.slice(0, 4),
        words: words.slice(0, 10)
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
        app.topicGuidanceStatus === "loading" &&
        app.topicGuidanceTopic === normalizedTopic &&
        app.topicGuidanceInterestsKey === interestKey &&
        app.topicGuidanceEnglishLevel === englishLevel
      ) {
        return false;
      }
      if (
        app.topicGuidanceTopic === normalizedTopic &&
        app.topicGuidanceInterestsKey === interestKey &&
        app.topicGuidanceEnglishLevel === englishLevel &&
        app.topicGuidanceStatus === "ready"
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
    const response = await fetch("/api/auth/session", {
      cache: "no-store"
    });
    const payload = (await response.json().catch(() => null)) as AuthResponse | null;

    if (response.status === 401) {
      return { email: null, isSubscriber: false, englishLevel: DEFAULT_ENGLISH_LEVEL };
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to restore session.");
    }

    const user = parseAuthUser(payload);
    if (!user) {
      return rejectWithValue("Invalid session payload.");
    }

    return user;
  } catch {
    return rejectWithValue("Cannot connect to authentication service.");
  }
});

export const signIn = createAsyncThunk<
  { email: string; isSubscriber: boolean; englishLevel: EnglishLevel },
  void,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/signIn",
  async (_, { getState, rejectWithValue }) => {
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
      const response = await fetch("/api/auth/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ email, password })
      });
      const payload = (await response.json().catch(() => null)) as AuthResponse | null;
      const user = parseAuthUser(payload);

      if (!response.ok || !user) {
        return rejectWithValue(payload?.error ?? "Failed to sign in.");
      }

      return user;
    } catch {
      return rejectWithValue("Cannot connect to authentication service.");
    }
  }
);

export const signUp = createAsyncThunk<
  { email: string; isSubscriber: boolean; englishLevel: EnglishLevel },
  void,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/signUp",
  async (_, { getState, rejectWithValue }) => {
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
      const response = await fetch("/api/auth/register", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ email, password })
      });
      const payload = (await response.json().catch(() => null)) as AuthResponse | null;
      const user = parseAuthUser(payload);

      if (!response.ok || !user) {
        return rejectWithValue(payload?.error ?? "Failed to create account.");
      }

      return user;
    } catch {
      return rejectWithValue("Cannot connect to authentication service.");
    }
  }
);

export const logout = createAsyncThunk("app/logout", async () => {
  try {
    await fetch("/api/auth/logout", {
      method: "POST"
    });
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
    const response = await fetch("/api/user/data", {
      cache: "no-store"
    });
    const payload = (await response.json().catch(() => null)) as UserDataResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to load user data.");
    }

    const interestIds = normalizeInterestIds(payload?.interestIds);
    const recordingsRaw = Array.isArray(payload?.recordings) ? payload.recordings : [];
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
      const response = await fetch("/api/user/interests", {
        method: "PUT",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ interestIds: selectedInterestIds })
      });

      const payload = (await response.json().catch(() => null)) as SaveInterestsResponse | null;

      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }

      if (!response.ok) {
        return rejectWithValue(payload?.error ?? "Failed to save interests.");
      }

      return normalizeInterestIds(payload?.interestIds);
    } catch {
      return rejectWithValue("Cannot connect to user data service.");
    }
  }
);

export const saveRecording = createAsyncThunk<
  { recording: Recording; quota: RecordingQuota | null },
  void,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/saveRecording",
  async (_, { getState, rejectWithValue }) => {
    const {
      isAuthenticated,
      speakState,
      selectedTopic,
      recordingPracticeType,
      pendingPhotoDataUrl,
      pendingPhotoObjectDraft,
      recordingDuration,
      isSubscriber,
      weeklyRemainingSeconds,
      maxSessionSeconds
    } = getState().app;

    if (speakState !== "recorded") {
      return rejectWithValue("Recording is not ready to save.");
    }

    if (!isAuthenticated) {
      return rejectWithValue("Unauthorized");
    }

    const normalizedDuration = Math.max(0, Math.floor(recordingDuration));
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

    const normalizedPhotoObject = pendingPhotoObjectDraft
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, PHOTO_PRACTICE_MAX_OBJECT_LENGTH);
    const photoObject = normalizedPhotoObject || null;
    const photoDataUrl = recordingPracticeType === "photo_description" ? pendingPhotoDataUrl : null;

    if (recordingPracticeType === "photo_description" && !photoDataUrl) {
      return rejectWithValue("Upload a photo before saving this practice.");
    }

    const topic =
      recordingPracticeType === "photo_description"
        ? photoObject
          ? `Photo description: ${photoObject}`
          : "Photo description"
        : selectedTopic ?? "Free talk";

    const recordingDraft = {
      topic,
      duration: normalizedDuration,
      timestamp: new Date().toISOString(),
      practiceType: recordingPracticeType,
      photoDataUrl,
      photoObject
    };

    try {
      const response = await fetch("/api/user/recordings", {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ recording: recordingDraft })
      });
      const payload = (await response.json().catch(() => null)) as SaveRecordingResponse | null;

      if (response.status === 401) {
        return rejectWithValue("Unauthorized");
      }

      if (!response.ok) {
        return rejectWithValue(payload?.error ?? "Failed to save recording.");
      }

      const recording = parseRecording(payload?.recording);
      if (!recording) {
        return rejectWithValue("Invalid recording payload from server.");
      }
      const quota = parseRecordingQuota(payload?.quota);

      return { recording, quota };
    } catch {
      return rejectWithValue("Cannot connect to user data service.");
    }
  }
);

export const subscribeMonthly = createAsyncThunk<
  { subscription: SubscriptionState; quota: RecordingQuota | null },
  void,
  { state: { app: AppState }; rejectValue: string }
>("app/subscribeMonthly", async (_, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  try {
    const response = await fetch("/api/user/subscription", {
      method: "POST"
    });
    const payload = (await response.json().catch(() => null)) as SubscriptionResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to activate subscription.");
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
    const response = await fetch("/api/user/subscription", {
      method: "DELETE"
    });
    const payload = (await response.json().catch(() => null)) as SubscriptionResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to cancel subscription.");
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

export const fetchOllamaModelSettings = createAsyncThunk<
  OllamaModelState,
  void,
  { state: { app: AppState }; rejectValue: string }
>("app/fetchOllamaModelSettings", async (_, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  try {
    const response = await fetch("/api/user/ollama-model", {
      cache: "no-store"
    });
    const payload = (await response.json().catch(() => null)) as OllamaModelResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to load Ollama model settings.");
    }

    const modelState = parseOllamaModelState(payload);
    if (!modelState) {
      return rejectWithValue("Invalid Ollama model payload.");
    }

    return modelState;
  } catch {
    return rejectWithValue("Cannot connect to Ollama model service.");
  }
});

export const saveOllamaModel = createAsyncThunk<
  OllamaModelState,
  string,
  { state: { app: AppState }; rejectValue: string }
>("app/saveOllamaModel", async (model, { getState, rejectWithValue }) => {
  if (!getState().app.isAuthenticated) {
    return rejectWithValue("Unauthorized");
  }

  const normalizedModel = normalizeModelName(model);
  if (!normalizedModel) {
    return rejectWithValue("Model name is invalid.");
  }

  try {
    const response = await fetch("/api/user/ollama-model", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ model: normalizedModel })
    });
    const payload = (await response.json().catch(() => null)) as OllamaModelResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to save Ollama model settings.");
    }

    const modelState = parseOllamaModelState(payload);
    if (!modelState) {
      return rejectWithValue("Invalid Ollama model payload.");
    }

    return modelState;
  } catch {
    return rejectWithValue("Cannot connect to Ollama model service.");
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
    const response = await fetch("/api/user/english-level", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ level: normalizedLevel })
    });
    const payload = (await response.json().catch(() => null)) as EnglishLevelResponse | null;

    if (response.status === 401) {
      return rejectWithValue("Unauthorized");
    }

    if (!response.ok) {
      return rejectWithValue(payload?.error ?? "Failed to save English level.");
    }

    return normalizeEnglishLevel(payload?.level, normalizedLevel);
  } catch {
    return rejectWithValue("Cannot connect to English level service.");
  }
});

const initialState: AppState = {
  currentScreen: "speak",
  activeTab: "speak",
  speakState: "idle",
  selectedTopic: null,
  showQuestions: false,
  showWords: false,
  recordingDuration: 0,
  recordings: [],
  selectedDate: null,
  currentRecordingId: null,
  isPlaying: false,
  playbackPosition: 0,
  topics: [],
  showAddTopicInput: false,
  customTopicDraft: "",
  calendarVisible: false,
  calendarMonth: today.getMonth(),
  calendarYear: today.getFullYear(),
  shareModalOpen: false,
  shareAction: "copy",
  copyMessage: null,
  isAuthenticated: false,
  userEmail: null,
  authEmailDraft: "",
  authPasswordDraft: "",
  authError: null,
  authStatus: "idle",
  authInitialized: false,
  pendingSaveAfterAuth: false,
  screenBeforeAuth: "speak",
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
  recordingPracticeType: "topic",
  pendingPhotoDataUrl: null,
  pendingPhotoObjectDraft: "",
  pendingPhotoError: null,
  selectedInterestIds: [],
  selectedEnglishLevel: DEFAULT_ENGLISH_LEVEL,
  userDataStatus: "idle",
  userDataError: null,
  recordingSaveStatus: "idle",
  recordingSaveError: null,
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
  selectedOllamaModel: DEFAULT_OLLAMA_MODEL_STATE.selectedModel,
  availableOllamaModels: DEFAULT_OLLAMA_MODEL_STATE.availableModels,
  ollamaModelsStatus: "idle",
  ollamaModelsError: null,
  ollamaModelSaveStatus: "idle",
  ollamaModelSaveError: null,
  englishLevelSaveStatus: "idle",
  englishLevelSaveError: null
};

const resetPlayback = (state: AppState): void => {
  state.isPlaying = false;
  state.playbackPosition = 0;
};

const clearTopicGuidanceState = (state: AppState): void => {
  state.topicGuidanceQuestions = [];
  state.topicGuidanceWords = [];
  state.topicGuidanceStatus = "idle";
  state.topicGuidanceError = null;
  state.topicGuidanceTopic = null;
  state.topicGuidanceInterestsKey = "";
  state.topicGuidanceEnglishLevel = state.selectedEnglishLevel;
};

const openAuthFlow = (state: AppState, pendingSaveAfterAuth: boolean): void => {
  state.screenBeforeAuth = state.activeTab;
  state.currentScreen = "auth";
  state.activeTab = "speak";
  state.authPasswordDraft = "";
  state.authError = null;
  state.authStatus = "idle";
  state.pendingSaveAfterAuth = pendingSaveAfterAuth;
  state.shareModalOpen = false;
  state.copyMessage = null;
  resetPlayback(state);
};

const applySavedRecording = (state: AppState, recording: Recording): void => {
  state.recordings = [recording, ...state.recordings.filter((item) => item.id !== recording.id)];
  state.currentRecordingId = recording.id;
  state.selectedDate = toDateKey(new Date(recording.timestamp));
  state.speakState = "idle";
  state.selectedTopic = null;
  state.showQuestions = false;
  state.showWords = false;
  state.recordingDuration = 0;
  state.showAddTopicInput = false;
  state.customTopicDraft = "";
  state.activeTab = "history";
  state.currentScreen = "details";
  const recordingDate = new Date(recording.timestamp);
  state.calendarMonth = recordingDate.getMonth();
  state.calendarYear = recordingDate.getFullYear();
  state.copyMessage = null;
  state.pendingSaveAfterAuth = false;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.recordingPracticeType = "topic";
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

const applyOllamaModelState = (state: AppState, modelState: OllamaModelState | null): void => {
  if (!modelState) {
    state.selectedOllamaModel = DEFAULT_OLLAMA_MODEL_STATE.selectedModel;
    state.availableOllamaModels = DEFAULT_OLLAMA_MODEL_STATE.availableModels;
    return;
  }

  state.selectedOllamaModel = modelState.selectedModel;
  state.availableOllamaModels = modelState.availableModels;
  state.ollamaModelsError = modelState.warning;
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
  state.ollamaModelsStatus = "idle";
  state.ollamaModelsError = null;
  state.ollamaModelSaveStatus = "idle";
  state.ollamaModelSaveError = null;
  state.englishLevelSaveStatus = "idle";
  state.englishLevelSaveError = null;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.pendingPhotoError = null;
  state.selectedInterestIds = [];
  state.questionsEnglishLevel = englishLevel;
  state.recordings = [];
  state.currentRecordingId = null;
  state.selectedDate = null;
  state.currentScreen = state.screenBeforeAuth;
  state.activeTab = state.screenBeforeAuth;
  applySubscriptionState(state, {
    isSubscriber,
    subscriptionExpiresAt: null,
    subscriptionCancelled: false
  });
  applyRecordingQuotaState(state, buildDefaultRecordingQuota(isSubscriber));
  applyOllamaModelState(state, DEFAULT_OLLAMA_MODEL_STATE);
};

const clearAuthenticatedState = (state: AppState): void => {
  state.isAuthenticated = false;
  state.userEmail = null;
  state.activeTab = "speak";
  state.currentScreen = "speak";
  state.shareModalOpen = false;
  state.copyMessage = null;
  state.authPasswordDraft = "";
  state.authError = null;
  state.authStatus = "idle";
  state.authInitialized = true;
  state.pendingSaveAfterAuth = false;
  state.screenBeforeAuth = "speak";
  state.selectedInterestIds = [];
  state.questionsInterestsKey = "";
  state.questionsDate = null;
  state.questionsEnglishLevel = DEFAULT_ENGLISH_LEVEL;
  state.topics = [];
  state.questionsStatus = "idle";
  state.questionsError = null;
  state.recordingPracticeType = "topic";
  state.pendingPhotoDataUrl = null;
  state.pendingPhotoObjectDraft = "";
  state.pendingPhotoError = null;
  state.selectedEnglishLevel = DEFAULT_ENGLISH_LEVEL;
  state.recordings = [];
  state.currentRecordingId = null;
  state.selectedDate = null;
  state.userDataStatus = "idle";
  state.userDataError = null;
  state.recordingSaveStatus = "idle";
  state.recordingSaveError = null;
  state.interestsSaveStatus = "idle";
  state.interestsSaveError = null;
  state.subscriptionActionStatus = "idle";
  state.subscriptionActionError = null;
  state.ollamaModelsStatus = "idle";
  state.ollamaModelsError = null;
  state.ollamaModelSaveStatus = "idle";
  state.ollamaModelSaveError = null;
  state.englishLevelSaveStatus = "idle";
  state.englishLevelSaveError = null;
  state.isSubscriber = false;
  state.subscriptionExpiresAt = DEFAULT_SUBSCRIPTION_STATE.subscriptionExpiresAt;
  state.subscriptionCancelled = DEFAULT_SUBSCRIPTION_STATE.subscriptionCancelled;
  applyRecordingQuotaState(state, DEFAULT_RECORDING_QUOTA);
  applyOllamaModelState(state, DEFAULT_OLLAMA_MODEL_STATE);
  clearTopicGuidanceState(state);
  resetPlayback(state);
};

const appSlice = createSlice({
  name: "app",
  initialState,
  reducers: {
    navigateToTab: (state, action: PayloadAction<TabName>) => {
      if (action.payload !== "speak" && !state.isAuthenticated) {
        return;
      }
      if (state.currentScreen === "auth") {
        state.pendingSaveAfterAuth = false;
        state.authPasswordDraft = "";
        state.authError = null;
        state.authStatus = "idle";
      }
      state.activeTab = action.payload;
      state.currentScreen = action.payload;
      state.shareModalOpen = false;
      if (action.payload === "speak") {
        state.copyMessage = null;
      }
      resetPlayback(state);
    },
    clearQuestionsError: (state) => {
      state.questionsError = null;
    },
    clearTopicGuidanceError: (state) => {
      state.topicGuidanceError = null;
    },
    openProfile: (state) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.currentScreen = "profile";
      state.shareModalOpen = false;
      state.copyMessage = null;
      resetPlayback(state);
    },
    openInterests: (state) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.currentScreen = "interests";
      state.shareModalOpen = false;
      state.copyMessage = null;
      resetPlayback(state);
    },
    backToProfile: (state) => {
      if (!state.isAuthenticated) {
        state.currentScreen = "speak";
        state.activeTab = "speak";
        state.shareModalOpen = false;
        state.copyMessage = null;
        resetPlayback(state);
        return;
      }
      state.currentScreen = "profile";
      state.shareModalOpen = false;
      state.copyMessage = null;
      resetPlayback(state);
    },
    toggleInterest: (state, action: PayloadAction<string>) => {
      const interestId = action.payload;
      if (!INTEREST_LOOKUP.has(interestId)) {
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
    },
    setPhotoUploadError: (state, action: PayloadAction<string | null>) => {
      state.pendingPhotoError = action.payload;
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
      state.copyMessage = null;
      state.recordingSaveError = null;
      state.pendingPhotoError = null;
      clearTopicGuidanceState(state);
    },
    startFreeTalk: (state) => {
      state.selectedTopic = null;
      state.showQuestions = false;
      state.showWords = false;
      state.speakState = "recording";
      state.recordingDuration = 0;
      state.copyMessage = null;
      state.recordingSaveError = null;
      state.recordingPracticeType = "free_talk";
      state.pendingPhotoError = null;
      clearTopicGuidanceState(state);
    },
    selectTopic: (state, action: PayloadAction<string>) => {
      state.selectedTopic = action.payload;
      state.speakState = "readyToRecord";
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.copyMessage = null;
      state.recordingSaveError = null;
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
      state.copyMessage = null;
      state.recordingSaveError = null;
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
      state.pendingPhotoError = null;
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
      state.pendingPhotoError = null;
      if (state.topicGuidanceTopic !== normalized) {
        clearTopicGuidanceState(state);
      }
    },
    toggleCalendar: (state) => {
      state.calendarVisible = !state.calendarVisible;
    },
    setSelectedDate: (state, action: PayloadAction<string>) => {
      state.selectedDate = action.payload;
    },
    clearSelectedDate: (state) => {
      state.selectedDate = null;
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
    openDetails: (state, action: PayloadAction<string>) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.currentRecordingId = action.payload;
      state.activeTab = "history";
      state.currentScreen = "details";
      state.copyMessage = null;
      resetPlayback(state);
    },
    backToHistory: (state) => {
      if (!state.isAuthenticated) {
        state.activeTab = "speak";
        state.currentScreen = "speak";
        state.shareModalOpen = false;
        state.copyMessage = null;
        resetPlayback(state);
        return;
      }
      state.activeTab = "history";
      state.currentScreen = "history";
      state.shareModalOpen = false;
      state.copyMessage = null;
      resetPlayback(state);
    },
    togglePlayback: (state) => {
      state.isPlaying = !state.isPlaying;
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
    openShareModal: (state) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.shareModalOpen = true;
      state.copyMessage = null;
    },
    closeShareModal: (state) => {
      state.shareModalOpen = false;
    },
    setShareAction: (state, action: PayloadAction<ShareAction>) => {
      state.shareAction = action.payload;
    },
    openSharePreview: (state) => {
      if (!state.isAuthenticated) {
        return;
      }
      state.shareModalOpen = false;
      state.currentScreen = "share";
      state.activeTab = "history";
      state.copyMessage = null;
      resetPlayback(state);
    },
    setCopyMessage: (state, action: PayloadAction<string | null>) => {
      state.copyMessage = action.payload;
    },
    openAuth: (state) => {
      if (state.isAuthenticated) {
        return;
      }
      openAuthFlow(state, false);
    },
    cancelAuth: (state) => {
      state.currentScreen = state.screenBeforeAuth;
      state.activeTab = state.screenBeforeAuth;
      state.authPasswordDraft = "";
      state.authError = null;
      state.authStatus = "idle";
      state.pendingSaveAfterAuth = false;
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
          applyOllamaModelState(state, DEFAULT_OLLAMA_MODEL_STATE);
          state.ollamaModelsStatus = "idle";
          state.ollamaModelsError = null;
          state.ollamaModelSaveStatus = "idle";
          state.ollamaModelSaveError = null;
          state.englishLevelSaveStatus = "idle";
          state.englishLevelSaveError = null;
          state.userDataStatus = "idle";
          state.userDataError = null;
          return;
        }

        state.isAuthenticated = false;
        state.userEmail = null;
        state.isSubscriber = false;
        state.selectedEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        state.questionsEnglishLevel = DEFAULT_ENGLISH_LEVEL;
        applySubscriptionState(state, DEFAULT_SUBSCRIPTION_STATE);
        applyRecordingQuotaState(state, DEFAULT_RECORDING_QUOTA);
        applyOllamaModelState(state, DEFAULT_OLLAMA_MODEL_STATE);
        state.ollamaModelsStatus = "idle";
        state.ollamaModelsError = null;
        state.ollamaModelSaveStatus = "idle";
        state.ollamaModelSaveError = null;
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.userDataStatus = "idle";
        state.userDataError = null;
        state.selectedInterestIds = [];
        state.recordings = [];
        clearTopicGuidanceState(state);
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
        applyOllamaModelState(state, DEFAULT_OLLAMA_MODEL_STATE);
        state.ollamaModelsStatus = "idle";
        state.ollamaModelsError = null;
        state.ollamaModelSaveStatus = "idle";
        state.ollamaModelSaveError = null;
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.userDataStatus = "idle";
        state.userDataError = null;
        state.selectedInterestIds = [];
        state.recordings = [];
        clearTopicGuidanceState(state);
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
        state.recordings = action.payload.recordings;
        applySubscriptionState(state, action.payload.subscription);
        applyRecordingQuotaState(state, action.payload.quota);
      })
      .addCase(fetchUserData.rejected, (state, action) => {
        if (action.payload === "Unauthorized") {
          clearAuthenticatedState(state);
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
          clearAuthenticatedState(state);
          return;
        }
        if (action.payload && action.payload !== "Unauthorized") {
          state.interestsSaveError = action.payload;
        }
      })
      .addCase(saveRecording.pending, (state) => {
        state.recordingSaveStatus = "loading";
        state.recordingSaveError = null;
      })
      .addCase(saveRecording.fulfilled, (state, action) => {
        applySavedRecording(state, action.payload.recording);
        applyRecordingQuotaState(state, action.payload.quota);
      })
      .addCase(saveRecording.rejected, (state, action) => {
        state.recordingSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          clearAuthenticatedState(state);
          openAuthFlow(state, true);
          return;
        }
        if (action.payload && action.payload !== "Unauthorized") {
          state.recordingSaveError = action.payload;
        }
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
          clearAuthenticatedState(state);
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
          clearAuthenticatedState(state);
          return;
        }
        state.subscriptionActionError = action.payload ?? "Failed to cancel subscription.";
      })
      .addCase(fetchOllamaModelSettings.pending, (state) => {
        state.ollamaModelsStatus = "loading";
        state.ollamaModelsError = null;
        state.ollamaModelSaveError = null;
      })
      .addCase(fetchOllamaModelSettings.fulfilled, (state, action) => {
        state.ollamaModelsStatus = "ready";
        state.ollamaModelsError = action.payload.warning;
        applyOllamaModelState(state, action.payload);
      })
      .addCase(fetchOllamaModelSettings.rejected, (state, action) => {
        state.ollamaModelsStatus = "failed";
        if (action.payload === "Unauthorized") {
          clearAuthenticatedState(state);
          return;
        }
        state.ollamaModelsError = action.payload ?? "Failed to load Ollama model settings.";
      })
      .addCase(saveOllamaModel.pending, (state) => {
        state.ollamaModelSaveStatus = "loading";
        state.ollamaModelSaveError = null;
      })
      .addCase(saveOllamaModel.fulfilled, (state, action) => {
        state.ollamaModelSaveStatus = "idle";
        state.ollamaModelSaveError = null;
        state.ollamaModelsStatus = "ready";
        state.ollamaModelsError = action.payload.warning;
        applyOllamaModelState(state, action.payload);
      })
      .addCase(saveOllamaModel.rejected, (state, action) => {
        state.ollamaModelSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          clearAuthenticatedState(state);
          return;
        }
        state.ollamaModelSaveError = action.payload ?? "Failed to save Ollama model settings.";
      })
      .addCase(saveEnglishLevel.pending, (state) => {
        state.englishLevelSaveStatus = "loading";
        state.englishLevelSaveError = null;
      })
      .addCase(saveEnglishLevel.fulfilled, (state, action) => {
        state.englishLevelSaveStatus = "idle";
        state.englishLevelSaveError = null;
        state.selectedEnglishLevel = action.payload;
      })
      .addCase(saveEnglishLevel.rejected, (state, action) => {
        state.englishLevelSaveStatus = "idle";
        if (action.payload === "Unauthorized") {
          clearAuthenticatedState(state);
          return;
        }
        state.englishLevelSaveError = action.payload ?? "Failed to save English level.";
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
        if (state.topicGuidanceTopic !== topic || state.topicGuidanceInterestsKey !== interestKey) {
          state.topicGuidanceQuestions = [];
          state.topicGuidanceWords = [];
        }
        state.topicGuidanceTopic = topic;
        state.topicGuidanceInterestsKey = interestKey;
        state.topicGuidanceEnglishLevel = englishLevel;
        state.topicGuidanceStatus = "loading";
        state.topicGuidanceError = null;
      })
      .addCase(fetchTopicGuidance.fulfilled, (state, action) => {
        state.topicGuidanceTopic = action.payload.topic;
        state.topicGuidanceInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
        state.topicGuidanceEnglishLevel = action.meta.arg.englishLevel ?? DEFAULT_ENGLISH_LEVEL;
        state.topicGuidanceQuestions = action.payload.questions;
        state.topicGuidanceWords = action.payload.words;
        state.topicGuidanceStatus = "ready";
        state.topicGuidanceError = null;
      })
      .addCase(fetchTopicGuidance.rejected, (state, action) => {
        state.topicGuidanceStatus =
          state.topicGuidanceQuestions.length > 0 || state.topicGuidanceWords.length > 0 ? "ready" : "failed";
        state.topicGuidanceError = action.payload ?? "Failed to generate guidance.";
      });
  }
});

export const {
  navigateToTab,
  clearQuestionsError,
  clearTopicGuidanceError,
  setPhotoUploadError,
  setPhotoForPractice,
  clearPhotoForPractice,
  setPhotoObjectDraft,
  startPhotoDescription,
  openProfile,
  openInterests,
  backToProfile,
  toggleInterest,
  startFreeTalk,
  selectTopic,
  toggleQuestions,
  toggleWords,
  startRecording,
  tickRecording,
  stopRecording,
  reRecord,
  openAuthForSave,
  toggleAddTopicInput,
  setCustomTopicDraft,
  useCustomTopic,
  toggleCalendar,
  setSelectedDate,
  clearSelectedDate,
  previousMonth,
  nextMonth,
  openDetails,
  backToHistory,
  togglePlayback,
  tickPlayback,
  setPlaybackPosition,
  resetPlaybackState,
  openShareModal,
  closeShareModal,
  setShareAction,
  openSharePreview,
  setCopyMessage,
  openAuth,
  cancelAuth,
  setAuthEmailDraft,
  setAuthPasswordDraft
} = appSlice.actions;

export default appSlice.reducer;
