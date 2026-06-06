"use client";

import { useEffect } from "react";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  fetchUserData,
  logout,
  navigateToTab,
  openAuth,
  openProfile,
  restoreSession,
  saveRecording
} from "../store/slices/appSlice";
import AuthScreen from "./AuthScreen";
import DetailsScreen from "./DetailsScreen";
import FeedScreen from "./FeedScreen";
import FeedThreadScreen from "./FeedThreadScreen";
import HistoryScreen from "./HistoryScreen";
import InterestsScreen from "./InterestsScreen";
import ProfileScreen from "./ProfileScreen";
import ShareScreen from "./ShareScreen";
import SpeakScreen from "./SpeakScreen";

export default function AppShell() {
  const dispatch = useAppDispatch();
  const {
    currentScreen,
    isAuthenticated,
    userEmail,
    authInitialized,
    authStatus,
    userDataStatus,
    pendingSaveAfterAuth,
    speakState,
    recordingSaveStatus
  } = useAppSelector((state) => state.app);

  useEffect(() => {
    if (authInitialized || authStatus === "loading") {
      return;
    }

    void dispatch(restoreSession());
  }, [authInitialized, authStatus, dispatch]);

  useEffect(() => {
    if (!isAuthenticated || pendingSaveAfterAuth || userDataStatus !== "idle") {
      return;
    }

    void dispatch(fetchUserData());
  }, [dispatch, isAuthenticated, pendingSaveAfterAuth, userDataStatus]);

  useEffect(() => {
    if (!isAuthenticated || !pendingSaveAfterAuth || speakState !== "recorded" || recordingSaveStatus === "loading") {
      return;
    }

    void dispatch(saveRecording());
  }, [dispatch, isAuthenticated, pendingSaveAfterAuth, speakState, recordingSaveStatus]);

  const canShowHistory = isAuthenticated;
  const canShowFeed = isAuthenticated;
  const headerSubtitle = isAuthenticated ? "Practice studio" : "Sign in to save progress";
  const profileInitial = userEmail?.slice(0, 1).toUpperCase() ?? "P";
  const highlightedTab =
    currentScreen === "feed" || currentScreen === "feedThread"
      ? "feed"
      : currentScreen === "history" || currentScreen === "details" || currentScreen === "share"
        ? "history"
        : currentScreen === "speak"
          ? "speak"
          : null;

  return (
    <div className="app-viewport">
      <div className="phone-shell" role="application" aria-label="Daily Speaking Practice">
        <div className="phone-status-bar" aria-hidden="true">
          <span>9:41</span>
          <span className="phone-camera" />
          <span>LTE</span>
        </div>

        <header className="app-header">
          <button
            type="button"
            className="icon-btn brand-mark"
            onClick={() => dispatch(navigateToTab("speak"))}
            aria-label="Go to speaking practice"
          >
            DS
          </button>

          <div className="app-title-block">
            <h1>Daily Speaking</h1>
            <p>{headerSubtitle}</p>
          </div>

          {isAuthenticated ? (
            <button type="button" className="icon-btn profile-trigger" onClick={() => dispatch(openProfile())}>
              {profileInitial}
            </button>
          ) : (
            <button className="btn btn-secondary btn-small" onClick={() => dispatch(openAuth())}>
              Sign in
            </button>
          )}
        </header>

        {isAuthenticated && (
          <div className="session-strip">
            <button type="button" className="session-email-btn" onClick={() => dispatch(openProfile())}>
              {userEmail}
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-small"
              onClick={() => void dispatch(logout())}
              disabled={authStatus === "loading"}
            >
              Log out
            </button>
          </div>
        )}

        <main className="main-content">
          {currentScreen === "speak" && <SpeakScreen />}
          {currentScreen === "history" && <HistoryScreen />}
          {currentScreen === "feed" && <FeedScreen />}
          {currentScreen === "feedThread" && <FeedThreadScreen />}
          {currentScreen === "details" && <DetailsScreen />}
          {currentScreen === "share" && <ShareScreen />}
          {currentScreen === "auth" && <AuthScreen />}
          {currentScreen === "profile" && <ProfileScreen />}
          {currentScreen === "interests" && <InterestsScreen />}
        </main>

        <nav className="bottom-tabs" aria-label="Main navigation">
          <button className={highlightedTab === "speak" ? "active" : ""} onClick={() => dispatch(navigateToTab("speak"))}>
            <span className="tab-icon">Rec</span>
            <span>Speak</span>
          </button>
          <button
            className={highlightedTab === "history" ? "active" : ""}
            onClick={() => dispatch(navigateToTab("history"))}
            disabled={!canShowHistory}
          >
            <span className="tab-icon">Log</span>
            <span>History</span>
          </button>
          <button
            className={highlightedTab === "feed" ? "active" : ""}
            onClick={() => dispatch(navigateToTab("feed"))}
            disabled={!canShowFeed}
          >
            <span className="tab-icon">Live</span>
            <span>Feed</span>
          </button>
        </nav>
      </div>
    </div>
  );
}
