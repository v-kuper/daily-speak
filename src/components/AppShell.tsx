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
    activeTab,
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

  return (
    <div className="app-container">
      <header className="header">
        <h1>Daily Speaking</h1>
        <div className="header-actions">
          <ul className="nav-tabs" aria-label="Main navigation">
            <li>
              <button
                className={activeTab === "speak" ? "active" : ""}
                onClick={() => dispatch(navigateToTab("speak"))}
              >
                Speak
              </button>
            </li>
            {isAuthenticated && (
              <li>
                <button
                  className={activeTab === "history" ? "active" : ""}
                  onClick={() => dispatch(navigateToTab("history"))}
                >
                  History
                </button>
              </li>
            )}
            {isAuthenticated && (
              <li>
                <button className={activeTab === "feed" ? "active" : ""} onClick={() => dispatch(navigateToTab("feed"))}>
                  Feed
                </button>
              </li>
            )}
          </ul>

          {isAuthenticated ? (
            <div className="session-info" onClick={() => dispatch(openProfile())} role="presentation">
              <button type="button" className="session-email-btn" onClick={() => dispatch(openProfile())}>
                {userEmail}
              </button>
              <button
                type="button"
                className="btn btn-secondary btn-small"
                onClick={(event) => {
                  event.stopPropagation();
                  void dispatch(logout());
                }}
                disabled={authStatus === "loading"}
              >
                Log out
              </button>
            </div>
          ) : (
            <button className="btn btn-secondary btn-small" onClick={() => dispatch(openAuth())}>
              Sign in / Register
            </button>
          )}
        </div>
      </header>

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
    </div>
  );
}
