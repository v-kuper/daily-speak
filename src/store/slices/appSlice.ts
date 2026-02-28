import { createAsyncThunk, createSlice, type PayloadAction } from "@reduxjs/toolkit";
import { generateSuggestions, generateTranscript, type Recording } from "../../lib/data";
import { toDateKey } from "../../lib/utils";

export type ScreenName = "speak" | "history" | "details" | "share" | "auth";
export type TabName = "speak" | "history";
export type SpeakMode = "idle" | "readyToRecord" | "recording" | "recorded";
export type ShareAction = "copy" | "preview";
export type AuthStep = "email" | "code";
export type QuestionsStatus = "idle" | "loading" | "ready" | "failed";

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
  topicGuidanceQuestions: string[];
  topicGuidanceWords: string[];
  topicGuidanceStatus: QuestionsStatus;
  topicGuidanceError: string | null;
  topicGuidanceTopic: string | null;
};

const today = new Date();
const MOCK_AUTH_CODE = "123456";
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

type FetchDailyQuestionsArgs = {
  dateKey: string;
  force?: boolean;
  refreshToken?: string;
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

export const fetchDailyQuestions = createAsyncThunk<
  FetchDailyQuestionsResult,
  FetchDailyQuestionsArgs,
  { state: { app: AppState }; rejectValue: string }
>(
  "app/fetchDailyQuestions",
  async ({ dateKey, refreshToken }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ date: dateKey });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }

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
    condition: ({ dateKey, force }, { getState }) => {
      if (force) {
        return true;
      }
      const { app } = getState();
      if (app.questionsStatus === "loading") {
        return false;
      }
      if (app.questionsDate === dateKey && app.topics.length === 3) {
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
  async ({ topic, refreshToken }, { rejectWithValue }) => {
    try {
      const params = new URLSearchParams({ topic });
      if (refreshToken) {
        params.set("refresh", refreshToken);
      }

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
    condition: ({ topic, force }, { getState }) => {
      if (force) {
        return true;
      }
      const normalizedTopic = topic.trim();
      if (!normalizedTopic) {
        return false;
      }
      const { app } = getState();
      if (app.topicGuidanceStatus === "loading" && app.topicGuidanceTopic === normalizedTopic) {
        return false;
      }
      if (app.topicGuidanceTopic === normalizedTopic && app.topicGuidanceStatus === "ready") {
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
  topicGuidanceQuestions: [],
  topicGuidanceWords: [],
  topicGuidanceStatus: "idle",
  topicGuidanceError: null,
  topicGuidanceTopic: null
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
      if (action.payload === "history" && !state.isAuthenticated) {
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
        state.questionsStatus = "ready";
        state.questionsError = null;
      })
      .addCase(fetchDailyQuestions.rejected, (state, action) => {
        state.questionsStatus = state.topics.length > 0 ? "ready" : "failed";
        state.questionsError = action.payload ?? "Failed to generate questions.";
      })
      .addCase(fetchTopicGuidance.pending, (state, action) => {
        const topic = action.meta.arg.topic.trim();
        if (state.topicGuidanceTopic !== topic) {
          state.topicGuidanceQuestions = [];
          state.topicGuidanceWords = [];
        }
        state.topicGuidanceTopic = topic;
        state.topicGuidanceStatus = "loading";
        state.topicGuidanceError = null;
      })
      .addCase(fetchTopicGuidance.fulfilled, (state, action) => {
        state.topicGuidanceTopic = action.payload.topic;
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
