import { createSlice, type PayloadAction } from "@reduxjs/toolkit";
import {
  generateSuggestions,
  generateTranscript,
  pickRandomTopics,
  type Recording
} from "../../lib/data";
import { toDateKey } from "../../lib/utils";

export type ScreenName = "speak" | "history" | "details" | "share";
export type TabName = "speak" | "history";
export type SpeakMode = "idle" | "readyToRecord" | "recording" | "recorded";
export type ShareAction = "copy" | "preview";

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
};

const today = new Date();

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
  topics: pickRandomTopics(),
  showAddTopicInput: false,
  customTopicDraft: "",
  calendarVisible: false,
  calendarMonth: today.getMonth(),
  calendarYear: today.getFullYear(),
  shareModalOpen: false,
  shareAction: "copy",
  copyMessage: null
};

const resetPlayback = (state: AppState): void => {
  state.isPlaying = false;
  state.playbackPosition = 0;
};

const appSlice = createSlice({
  name: "app",
  initialState,
  reducers: {
    navigateToTab: (state, action: PayloadAction<TabName>) => {
      state.activeTab = action.payload;
      state.currentScreen = action.payload;
      state.shareModalOpen = false;
      if (action.payload === "speak") {
        state.copyMessage = null;
      }
      resetPlayback(state);
    },
    refreshTopics: (state) => {
      state.topics = pickRandomTopics();
    },
    startFreeTalk: (state) => {
      state.selectedTopic = null;
      state.showQuestions = false;
      state.showWords = false;
      state.speakState = "recording";
      state.recordingDuration = 0;
      state.copyMessage = null;
    },
    selectTopic: (state, action: PayloadAction<string>) => {
      state.selectedTopic = action.payload;
      state.speakState = "readyToRecord";
      state.showQuestions = false;
      state.showWords = false;
      state.showAddTopicInput = false;
      state.customTopicDraft = "";
      state.copyMessage = null;
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
      resetPlayback(state);
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
      state.currentRecordingId = action.payload;
      state.activeTab = "history";
      state.currentScreen = "details";
      state.copyMessage = null;
      resetPlayback(state);
    },
    backToHistory: (state) => {
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
      state.shareModalOpen = false;
      state.currentScreen = "share";
      state.activeTab = "history";
      state.copyMessage = null;
      resetPlayback(state);
    },
    setCopyMessage: (state, action: PayloadAction<string | null>) => {
      state.copyMessage = action.payload;
    }
  }
});

export const {
  navigateToTab,
  refreshTopics,
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
  setCopyMessage
} = appSlice.actions;

export default appSlice.reducer;
