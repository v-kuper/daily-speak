"use client";

import { useAppDispatch, useAppSelector } from "../store/hooks";
import { logout, navigateToTab, openAuth } from "../store/slices/appSlice";
import AuthScreen from "./AuthScreen";
import DetailsScreen from "./DetailsScreen";
import HistoryScreen from "./HistoryScreen";
import ShareScreen from "./ShareScreen";
import SpeakScreen from "./SpeakScreen";

export default function AppShell() {
  const dispatch = useAppDispatch();
  const { currentScreen, activeTab, isAuthenticated, userEmail } = useAppSelector((state) => state.app);

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
          </ul>

          {isAuthenticated ? (
            <div className="session-info">
              <span>{userEmail}</span>
              <button className="btn btn-secondary btn-small" onClick={() => dispatch(logout())}>
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
        {currentScreen === "details" && <DetailsScreen />}
        {currentScreen === "share" && <ShareScreen />}
        {currentScreen === "auth" && <AuthScreen />}
      </main>
    </div>
  );
}
