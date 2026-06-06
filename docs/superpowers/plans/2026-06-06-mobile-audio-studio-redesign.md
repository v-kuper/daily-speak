# Mobile Audio Studio Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the current Daily Speaking Practice app into a fully interactive always-mobile audio-studio interface.

**Architecture:** Keep the existing Next.js App Router, React components, Redux state, and API contracts. Add a mobile shell in `AppShell`, restyle existing screens through CSS tokens and small class additions, and add one reusable waveform component for recording/playback surfaces.

**Tech Stack:** Next.js 15, React 19, TypeScript, Redux Toolkit, plain CSS in `app/globals.css`, browser MediaRecorder/audio APIs already in the app.

---

## File Structure

- Create `src/components/AudioWaveform.tsx`: presentational waveform component reused in recording and playback surfaces.
- Modify `src/components/AppShell.tsx`: replace desktop header/nav layout with mobile app chrome, compact header, and bottom tabs while keeping existing dispatch actions.
- Modify `src/components/SpeakScreen.tsx`: add audio-studio classes and waveform usage for idle, ready, recording, and recorded states.
- Modify `src/components/DetailsScreen.tsx`: add waveform/playback studio markup around the existing audio playback logic.
- Modify `src/components/HistoryScreen.tsx`, `src/components/FeedScreen.tsx`, `src/components/ProfileScreen.tsx`, `src/components/AuthScreen.tsx`, `src/components/InterestsScreen.tsx`: add screen/card classes only where needed for consistent mobile styling.
- Modify `app/globals.css`: define design tokens, always-mobile shell, glass cards, bottom navigation, waveform, player, forms, lists, feed, profile, auth, and responsive rules.

## Task 1: Baseline Verification

**Files:**
- Read-only: `package.json`
- Read-only: `src/components/AppShell.tsx`
- Read-only: `app/globals.css`

- [ ] **Step 1: Confirm current working tree before code edits**

Run: `git status --short`

Expected: only pre-existing untracked files may appear, such as:

```text
?? .idea/swift-toolchain.xml
?? docs/EA_AGENT_DESIGN_BRIEF.md
?? docs/FUNCTIONAL_REQUIREMENTS_DESIGN.md
```

- [ ] **Step 2: Run TypeScript baseline**

Run: `npm run typecheck`

Expected: PASS or document any pre-existing TypeScript failures before changing code.

- [ ] **Step 3: Run lint baseline**

Run: `npm run lint`

Expected: PASS or document any pre-existing lint failures before changing code.

## Task 2: Add Reusable Audio Waveform

**Files:**
- Create: `src/components/AudioWaveform.tsx`

- [ ] **Step 1: Create presentational waveform component**

Create `src/components/AudioWaveform.tsx` with:

```tsx
type AudioWaveformProps = {
  variant?: "compact" | "hero";
  active?: boolean;
};

const WAVEFORM_BARS = [18, 34, 54, 42, 28, 64, 48, 26, 58, 36, 22, 44, 30, 20];

export default function AudioWaveform({ variant = "compact", active = false }: AudioWaveformProps) {
  return (
    <div className={`audio-waveform audio-waveform-${variant} ${active ? "active" : ""}`} aria-hidden="true">
      {WAVEFORM_BARS.map((height, index) => (
        <span key={`${height}-${index}`} style={{ "--bar-height": `${height}%` } as React.CSSProperties} />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Run TypeScript for the new component**

Run: `npm run typecheck`

Expected: PASS. If `React.CSSProperties` requires an import, add:

```tsx
import type { CSSProperties } from "react";
```

and change the style cast to:

```tsx
style={{ "--bar-height": `${height}%` } as CSSProperties}
```

## Task 3: Convert AppShell To Always-Mobile Chrome

**Files:**
- Modify: `src/components/AppShell.tsx`
- Modify: `app/globals.css`

- [ ] **Step 1: Update AppShell markup**

Replace the current root/header/main structure in `AppShell` with this structure while keeping the existing imports, hooks, effects, and screen rendering:

```tsx
  const canShowHistory = isAuthenticated;
  const canShowFeed = isAuthenticated;
  const headerSubtitle = isAuthenticated ? "Practice studio" : "Sign in to save progress";

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
            className="icon-btn"
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
              {userEmail?.slice(0, 1).toUpperCase() ?? "P"}
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
          <button
            className={activeTab === "speak" ? "active" : ""}
            onClick={() => dispatch(navigateToTab("speak"))}
          >
            <span className="tab-icon">Rec</span>
            <span>Speak</span>
          </button>
          <button
            className={activeTab === "history" ? "active" : ""}
            onClick={() => dispatch(navigateToTab("history"))}
            disabled={!canShowHistory}
          >
            <span className="tab-icon">Log</span>
            <span>History</span>
          </button>
          <button
            className={activeTab === "feed" ? "active" : ""}
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
```

- [ ] **Step 2: Add shell CSS tokens and layout**

In `app/globals.css`, replace the current `:root`, `body`, `.app-container`, `.header`, `.header-actions`, `.nav-tabs`, `.session-info`, `.main-content`, and base button definitions with a mobile-first tokenized shell. Preserve later class names that screens still use.

Use these token values:

```css
:root {
  color-scheme: light;
  --bg-page: #d8d1c8;
  --bg-app: #eee8df;
  --panel: rgba(255, 255, 255, 0.54);
  --panel-strong: rgba(255, 255, 255, 0.76);
  --border-soft: rgba(255, 255, 255, 0.62);
  --border-muted: rgba(87, 78, 67, 0.14);
  --text-main: #2d2925;
  --text-muted: #7d756d;
  --accent: #f05f2f;
  --accent-strong: #d84f22;
  --ink: #27221e;
  --danger: #9a2f2f;
  --success: #315f3c;
  --shadow-soft: 0 24px 70px rgba(55, 48, 40, 0.24);
  --shadow-panel: 0 16px 42px rgba(80, 70, 58, 0.12);
}
```

- [ ] **Step 3: Verify shell compiles**

Run: `npm run typecheck`

Expected: PASS.

## Task 4: Restyle Speak Screen As Audio Studio

**Files:**
- Modify: `src/components/SpeakScreen.tsx`
- Modify: `app/globals.css`

- [ ] **Step 1: Import waveform component**

At the top of `src/components/SpeakScreen.tsx`, add:

```tsx
import AudioWaveform from "./AudioWaveform";
```

- [ ] **Step 2: Add waveform to idle hero**

Inside the idle state's first `.speak-card.speak-hero-card`, after the heading and before quota/error notices, add:

```tsx
          <div className="studio-focus-panel">
            <div className="studio-kicker">Ready when you are</div>
            <AudioWaveform variant="hero" />
            <div className="studio-timer-preview">00:00</div>
          </div>
```

- [ ] **Step 3: Add waveform to recording state**

Inside the recording state's `.speak-card.speak-center-card`, place this before the timer:

```tsx
          <div className="studio-focus-panel live">
            <div className="studio-kicker">Live recording</div>
            <AudioWaveform variant="hero" active />
          </div>
```

- [ ] **Step 4: Add waveform to recorded state**

Inside the recorded state's `.speak-card.speak-center-card`, after the optional photo preview and before quota notices, add:

```tsx
        <div className="studio-focus-panel">
          <div className="studio-kicker">Captured audio</div>
          <AudioWaveform variant="hero" />
        </div>
```

- [ ] **Step 5: Restyle speak CSS**

Update `app/globals.css` so `.speak-card`, `.studio-focus-panel`, `.audio-waveform`, `.timer`, `.recording-indicator`, `.topics-grid`, `.topic-btn`, `.question-item`, `.word-item`, `.study-word-chip`, `.study-text-card`, `.photo-practice-preview`, `.collapsible-header`, and `.empty-state` follow the new glass audio-studio system.

- [ ] **Step 6: Verify Speak screen compiles**

Run: `npm run typecheck`

Expected: PASS.

## Task 5: Restyle Details Playback Studio

**Files:**
- Modify: `src/components/DetailsScreen.tsx`
- Modify: `app/globals.css`

- [ ] **Step 1: Import waveform component**

At the top of `src/components/DetailsScreen.tsx`, add:

```tsx
import AudioWaveform from "./AudioWaveform";
```

- [ ] **Step 2: Add studio playback class and waveform**

Replace the current player block in `DetailsScreen`:

```tsx
      <div className="player">
        <div className="player-controls">
          <button className="play-btn" onClick={onTogglePlayback} disabled={!hasAudio}>
            {isPlaying ? "⏸" : "▶"}
          </button>
          <div className={`progress-bar ${hasAudio ? "" : "disabled-progress"}`} onClick={onSeek}>
            <div className="progress-bar-fill" style={{ width: `${playbackPercent}%` }} />
          </div>
          <div className="time-display">
            {formatTime(playbackPosition)} / {formatTime(recordingDuration)}
          </div>
        </div>
      </div>
```

with:

```tsx
      <div className="player studio-player">
        <AudioWaveform variant="compact" active={isPlaying} />
        <div className="player-controls">
          <button className="play-btn" onClick={onTogglePlayback} disabled={!hasAudio} aria-label={isPlaying ? "Pause" : "Play"}>
            {isPlaying ? "Pause" : "Play"}
          </button>
          <div className={`progress-bar ${hasAudio ? "" : "disabled-progress"}`} onClick={onSeek}>
            <div className="progress-bar-fill" style={{ width: `${playbackPercent}%` }} />
          </div>
          <div className="time-display">
            {formatTime(playbackPosition)} / {formatTime(recordingDuration)}
          </div>
        </div>
      </div>
```

- [ ] **Step 3: Restyle details CSS**

Update `app/globals.css` so `.details-metadata`, `.details-photo-card`, `.player`, `.studio-player`, `.player-controls`, `.play-btn`, `.progress-bar`, `.progress-bar-fill`, `.transcript-text`, and `.suggestion-item` match the mobile glass system.

- [ ] **Step 4: Verify Details compiles**

Run: `npm run typecheck`

Expected: PASS.

## Task 6: Align Secondary Screens To Mobile Glass System

**Files:**
- Modify: `src/components/HistoryScreen.tsx`
- Modify: `src/components/FeedScreen.tsx`
- Modify: `src/components/ProfileScreen.tsx`
- Modify: `src/components/AuthScreen.tsx`
- Modify: `src/components/InterestsScreen.tsx`
- Modify: `app/globals.css`

- [ ] **Step 1: Add screen wrapper classes**

Update top-level sections:

```tsx
<section className="screen-section history-screen">
```

```tsx
<section className="screen-section feed-screen">
```

```tsx
<section className="screen-section profile-screen">
```

```tsx
<section className="screen-section auth-screen">
```

```tsx
<section className="screen-section interests-screen">
```

- [ ] **Step 2: Restyle secondary screen classes**

Update `app/globals.css` so `.screen-section`, `.recording-card`, `.feed-card`, `.feed-reply-card`, `.feed-reply-composer`, `.profile-card`, `.profile-menu-item`, `.auth-form`, `.calendar`, `.modal-content`, `.notice`, `.auth-error`, `.interest-chip`, `.feed-reaction-btn`, and form inputs use the same tokens.

- [ ] **Step 3: Verify secondary screens compile**

Run: `npm run typecheck`

Expected: PASS.

## Task 7: Browser Verification And Polish

**Files:**
- Modify if needed: files from earlier tasks only.

- [ ] **Step 1: Start local dev server**

Run: `npm run dev`

Expected: Next.js starts and reports a local URL, usually `http://localhost:3000`.

- [ ] **Step 2: Open the app in browser**

Use the in-app browser at `http://localhost:3000`.

Expected: the first screen is the actual Daily Speaking app in a centered mobile shell, not a landing page.

- [ ] **Step 3: Verify desktop viewport**

Set browser viewport around `1280x900`.

Expected:

- Mobile shell remains centered.
- Header, main content, and bottom tabs do not overlap.
- Speak screen text fits in cards and buttons.
- Waveform is visible and not blank.

- [ ] **Step 4: Verify mobile viewport**

Set browser viewport around `390x844`.

Expected:

- App fills width without horizontal scrolling.
- Bottom tabs remain usable.
- Content has bottom padding so final card content is not hidden behind tabs.

- [ ] **Step 5: Check interactions**

Use browser UI to verify:

- Speak tab opens.
- History and Feed tabs are disabled or unavailable while signed out.
- Sign in screen opens from header.
- Start speaking requests microphone or shows a browser permission/error path without breaking layout.
- Upload photo control remains visible.
- Daily question cards remain clickable when data is available.

## Task 8: Final Verification

**Files:**
- All modified files.

- [ ] **Step 1: Run TypeScript**

Run: `npm run typecheck`

Expected: PASS.

- [ ] **Step 2: Run lint**

Run: `npm run lint`

Expected: PASS.

- [ ] **Step 3: Review git diff**

Run: `git diff --stat`

Expected: modified app files match the planned frontend scope.

- [ ] **Step 4: Commit implementation**

Run:

```bash
git add app/globals.css src/components/AppShell.tsx src/components/AudioWaveform.tsx src/components/SpeakScreen.tsx src/components/DetailsScreen.tsx src/components/HistoryScreen.tsx src/components/FeedScreen.tsx src/components/ProfileScreen.tsx src/components/AuthScreen.tsx src/components/InterestsScreen.tsx docs/superpowers/plans/2026-06-06-mobile-audio-studio-redesign.md
git commit -m "feat: redesign app as mobile audio studio"
```

Expected: one implementation commit containing only planned files.

## Self-Review

- Spec coverage: App shell, Speak, Details/playback, secondary screens, data-flow preservation, error visibility, accessibility, and verification are covered by Tasks 2-8.
- Placeholder scan: no `TBD`, `TODO`, or intentionally incomplete task remains.
- Type consistency: `AudioWaveform` props are defined before use; `variant` values are `"compact"` and `"hero"` consistently; existing Redux actions and screen names are preserved.
