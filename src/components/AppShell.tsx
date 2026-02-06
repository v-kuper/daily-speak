"use client";

import { useAppDispatch, useAppSelector } from "../store/hooks";
import { navigateToTab } from "../store/slices/appSlice";
import DetailsScreen from "./DetailsScreen";
import HistoryScreen from "./HistoryScreen";
import ShareScreen from "./ShareScreen";
import SpeakScreen from "./SpeakScreen";

export default function AppShell() {
  const dispatch = useAppDispatch();
  const { currentScreen, activeTab } = useAppSelector((state) => state.app);

  return (
    <div className="app-container">
      <header className="header">
        <h1>Daily Speaking</h1>
        <ul className="nav-tabs" aria-label="Main navigation">
          <li>
            <button
              className={activeTab === "speak" ? "active" : ""}
              onClick={() => dispatch(navigateToTab("speak"))}
            >
              Speak
            </button>
          </li>
          <li>
            <button
              className={activeTab === "history" ? "active" : ""}
              onClick={() => dispatch(navigateToTab("history"))}
            >
              History
            </button>
          </li>
        </ul>
      </header>

      <main className="main-content">
        {currentScreen === "speak" && <SpeakScreen />}
        {currentScreen === "history" && <HistoryScreen />}
        {currentScreen === "details" && <DetailsScreen />}
        {currentScreen === "share" && <ShareScreen />}
      </main>
    </div>
  );
}
