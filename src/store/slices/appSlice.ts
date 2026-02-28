import { createAsyncThunk, createSlice, type PayloadAction } from "@reduxjs/toolkit";
import { generateSuggestions, generateTranscript, type Recording } from "../../lib/data";
import { toDateKey } from "../../lib/utils";

export type ScreenName = "speak" | "history" | "details" | "share" | "auth" | "profile" | "interests";
export type TabName = "speak" | "history";
export type SpeakMode = "idle" | "readyToRecord" | "recording" | "recorded";
export type ShareAction = "copy" | "preview";
export type AuthStep = "email" | "code";
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
  authStep: AuthStep;
  authEmailDraft: string;
  authCodeDraft: string;
  authPendingEmail: string;
  authError: string | null;
  pendingSaveAfterAuth: boolean;
  screenBeforeAuth: TabName;
  questionsStatus: QuestionsStatus;
  questionsError: string | null;
  questionsDate: string | null;
  questionsInterestsKey: string;
  topicGuidanceQuestions: string[];
  topicGuidanceWords: string[];
  topicGuidanceStatus: QuestionsStatus;
  topicGuidanceError: string | null;
  topicGuidanceTopic: string | null;
  topicGuidanceInterestsKey: string;
  selectedInterestIds: string[];
};

const today = new Date();
const MOCK_AUTH_CODE = "123456";
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
export const MAX_SELECTED_INTERESTS = 10;

const INTEREST_LOOKUP = new Map(INTEREST_OPTIONS.map((item) => [item.id, item]));

type FetchDailyQuestionsArgs = {
  dateKey: string;
  force?: boolean;
  refreshToken?: string;
  interestIds?: string[];
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
  async ({ dateKey, refreshToken, interestIds = [] }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ date: dateKey });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }
      const interestLabels = resolveInterestLabels(interestIds);
      interestLabels.forEach((label) => params.append("interest", label));

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
    condition: ({ dateKey, force, interestIds = [] }, { getState }) => {
      if (force) {
        return true;
      }
      const { app } = getState();
      if (app.questionsStatus === "loading") {
        return false;
      }
      const interestKey = buildInterestsKey(interestIds);
      if (app.questionsDate === dateKey && app.questionsInterestsKey === interestKey && app.topics.length === 3) {
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
  async ({ topic, refreshToken, interestIds = [] }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ topic });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }
      const interestLabels = resolveInterestLabels(interestIds);
      interestLabels.forEach((label) => params.append("interest", label));

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
    condition: ({ topic, force, interestIds = [] }, { getState }) => {
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
        app.topicGuidanceInterestsKey === interestKey
      ) {
        return false;
      }
      if (
        app.topicGuidanceTopic === normalizedTopic &&
        app.topicGuidanceInterestsKey === interestKey &&
        app.topicGuidanceStatus === "ready"
      ) {
        return false;
      }
      return true;
    }
  }
);

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
  authStep: "email",
  authEmailDraft: "",
  authCodeDraft: "",
  authPendingEmail: "",
  authError: null,
  pendingSaveAfterAuth: false,
  screenBeforeAuth: "speak",
  questionsStatus: "idle",
  questionsError: null,
  questionsDate: null,
  questionsInterestsKey: "",
  topicGuidanceQuestions: [],
  topicGuidanceWords: [],
  topicGuidanceStatus: "idle",
  topicGuidanceError: null,
  topicGuidanceTopic: null,
  topicGuidanceInterestsKey: "",
  selectedInterestIds: []
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
};

const openAuthFlow = (state: AppState, pendingSaveAfterAuth: boolean): void => {
  state.screenBeforeAuth = state.activeTab;
  state.currentScreen = "auth";
  state.activeTab = "speak";
  state.authStep = "email";
  state.authCodeDraft = "";
  state.authPendingEmail = "";
  state.authError = null;
  state.pendingSaveAfterAuth = pendingSaveAfterAuth;
  state.shareModalOpen = false;
  state.copyMessage = null;
  resetPlayback(state);
};

const saveRecordingForAuthenticatedUser = (state: AppState): void => {
  const topic = state.selectedTopic ?? "Free talk";
  const now = new Date();
  const recording: Recording = {
    id: `${Date.now()}-${Math.floor(Math.random() * 1000)}`,
    topic,
    duration: state.recordingDuration,
    timestamp: now.toISOString(),
    transcript: generateTranscript(topic),
    suggestions: generateSuggestions()
  };

  state.recordings.unshift(recording);
  state.currentRecordingId = recording.id;
  state.selectedDate = toDateKey(now);
  state.speakState = "idle";
  state.selectedTopic = null;
  state.showQuestions = false;
  state.showWords = false;
  state.recordingDuration = 0;
  state.showAddTopicInput = false;
  state.customTopicDraft = "";
  state.activeTab = "history";
  state.currentScreen = "details";
  state.calendarMonth = now.getMonth();
  state.calendarYear = now.getFullYear();
  state.copyMessage = null;
  state.pendingSaveAfterAuth = false;
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
        state.authStep = "email";
        state.authCodeDraft = "";
        state.authPendingEmail = "";
        state.authError = null;
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
      clearTopicGuidanceState(state);
    },
    startFreeTalk: (state) => {
      state.selectedTopic = null;
      state.showQuestions = false;
      state.showWords = false;
      state.speakState = "recording";
      state.recordingDuration = 0;
      state.copyMessage = null;
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
    },
    tickRecording: (state) => {
      if (state.speakState === "recording") {
        state.recordingDuration += 1;
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
    },
    saveRecording: (state) => {
      if (state.speakState !== "recorded") {
        return;
      }

      if (!state.isAuthenticated) {
        openAuthFlow(state, true);
        return;
      }

      saveRecordingForAuthenticatedUser(state);
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
      state.authStep = "email";
      state.authCodeDraft = "";
      state.authPendingEmail = "";
      state.authError = null;
      state.pendingSaveAfterAuth = false;
    },
    setAuthEmailDraft: (state, action: PayloadAction<string>) => {
      state.authEmailDraft = action.payload;
      state.authError = null;
    },
    submitAuthEmail: (state) => {
      const email = state.authEmailDraft.trim().toLowerCase();
      if (!EMAIL_PATTERN.test(email)) {
        state.authError = "Enter a valid email address.";
        return;
      }
      state.authPendingEmail = email;
      state.authStep = "code";
      state.authCodeDraft = "";
      state.authError = null;
    },
    backToEmailStep: (state) => {
      state.authStep = "email";
      state.authCodeDraft = "";
      state.authError = null;
    },
    setAuthCodeDraft: (state, action: PayloadAction<string>) => {
      state.authCodeDraft = action.payload;
      state.authError = null;
    },
    verifyAuthCode: (state) => {
      if (state.authCodeDraft.trim() !== MOCK_AUTH_CODE) {
        state.authError = `Invalid code. Use ${MOCK_AUTH_CODE} for demo login.`;
        return;
      }

      state.isAuthenticated = true;
      state.userEmail = state.authPendingEmail || state.authEmailDraft.trim().toLowerCase();
      state.authStep = "email";
      state.authCodeDraft = "";
      state.authPendingEmail = "";
      state.authError = null;

      if (state.pendingSaveAfterAuth && state.speakState === "recorded") {
        saveRecordingForAuthenticatedUser(state);
        return;
      }

      state.pendingSaveAfterAuth = false;
      state.currentScreen = state.screenBeforeAuth;
      state.activeTab = state.screenBeforeAuth;
    },
    logout: (state) => {
      state.isAuthenticated = false;
      state.userEmail = null;
      state.activeTab = "speak";
      state.currentScreen = "speak";
      state.shareModalOpen = false;
      state.copyMessage = null;
      state.authStep = "email";
      state.authCodeDraft = "";
      state.authPendingEmail = "";
      state.authError = null;
      state.pendingSaveAfterAuth = false;
      state.screenBeforeAuth = "speak";
      state.selectedInterestIds = [];
      state.questionsInterestsKey = "";
      state.questionsDate = null;
      state.topics = [];
      state.questionsStatus = "idle";
      state.questionsError = null;
      clearTopicGuidanceState(state);
      resetPlayback(state);
    }
  },
  extraReducers: (builder) => {
    builder
      .addCase(fetchDailyQuestions.pending, (state) => {
        state.questionsStatus = "loading";
        state.questionsError = null;
      })
      .addCase(fetchDailyQuestions.fulfilled, (state, action) => {
        state.topics = action.payload.questions;
        state.questionsDate = action.payload.dateKey;
        state.questionsInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
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
        if (state.topicGuidanceTopic !== topic || state.topicGuidanceInterestsKey !== interestKey) {
          state.topicGuidanceQuestions = [];
          state.topicGuidanceWords = [];
        }
        state.topicGuidanceTopic = topic;
        state.topicGuidanceInterestsKey = interestKey;
        state.topicGuidanceStatus = "loading";
        state.topicGuidanceError = null;
      })
      .addCase(fetchTopicGuidance.fulfilled, (state, action) => {
        state.topicGuidanceTopic = action.payload.topic;
        state.topicGuidanceInterestsKey = buildInterestsKey(action.meta.arg.interestIds ?? []);
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
  saveRecording,
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
  submitAuthEmail,
  backToEmailStep,
  setAuthCodeDraft,
  verifyAuthCode,
  logout
} = appSlice.actions;

export default appSlice.reducer;
