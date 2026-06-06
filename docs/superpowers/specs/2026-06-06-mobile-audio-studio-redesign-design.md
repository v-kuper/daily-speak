# Mobile Audio Studio Redesign Design

## Summary

Redesign the existing Daily Speaking Practice app as an always-mobile web app. On desktop and mobile viewports, the product should render inside a mobile-width application surface with an audio-studio feel: warm glass panels, soft blurred depth, compact controls, waveform-forward recording and playback, and an orange accent inspired by the supplied reference image.

The app remains a working product, not a static landing page. Existing recording, history, details, feed, profile, authentication, interests, and save/publish flows must remain functional.

## Goals

- Make the application always feel like a mobile app, even on wide desktop screens.
- Apply the selected "Audio Studio" direction from visual option C.
- Preserve current product behavior and Redux/API flow.
- Improve the recording and playback surfaces so speaking practice feels central.
- Keep the redesign scoped to frontend structure and styling unless a small state or component change is needed to preserve existing behavior.

## Non-Goals

- Do not build a marketing landing page.
- Do not add new backend endpoints or change persistence behavior.
- Do not replace the current app with a static mock.
- Do not remove auth, history, feed, profile, comments, reactions, subscription quota, photo practice, or study-word functionality.
- Do not introduce a large UI dependency unless it is clearly needed for icons or audio affordances.

## Visual Direction

The visual language should adapt the reference image rather than copy its drum-grid product model:

- Mobile phone-like app canvas centered on desktop.
- Warm off-white, beige-gray, and soft graphite surfaces.
- Orange as the primary live/audio accent.
- Frosted glass panels for cards, controls, and bottom navigation.
- Rounded but restrained controls, with larger radii reserved for phone shell, floating panels, and circular audio buttons.
- Recording and playback states should visually foreground waveform/progress and elapsed time.

The selected direction is option C: an audio-studio interface. It emphasizes a focused "now speaking" surface, waveform treatment, compact transport controls, and guidance panels.

## App Structure

### App Shell

`AppShell` should become the stable mobile frame:

- Wrap the app in a desktop background and centered mobile canvas.
- Keep a mobile-width content area on all viewport sizes.
- Replace the current desktop top navigation with a compact mobile header and bottom tab navigation.
- Preserve existing navigation actions for Speak, History, and Feed.
- Keep profile/auth access reachable from the shell.

### Speak Screen

The Speak screen remains the primary home screen:

- Idle state becomes an audio-studio dashboard: daily practice, start speaking, daily questions, photo practice, study words, and custom topic.
- Ready-to-record state should show the selected prompt as a focused session card with guidance below.
- Recording state should emphasize live timer, selected topic, waveform-style visual treatment, and a clear stop action.
- Recorded state should preserve re-record and save/sign-in actions while matching the same visual system.

### Details And Playback

Recording details should feel like a playback studio:

- Keep the existing audio playback logic.
- Restyle the player with a larger circular play/pause control, progress bar, elapsed/total time, and glass panel treatment.
- Preserve transcript highlighting, AI suggestions, photo details, publish-to-feed, and comments.

### History, Feed, Profile, Auth, Interests

These screens should inherit the same mobile design system:

- Cards become soft glass list items.
- Empty states, notices, errors, forms, chips, calendar, reactions, and modal surfaces should match the new visual tokens.
- Feed audio controls may keep native audio where needed, but their container should match the redesign.
- Existing button labels and behavior should stay intact unless a label is visibly too long for the mobile shell.

## Components And Styling

The implementation should prefer existing components and classes. Add small presentational helpers only when they reduce duplication:

- Mobile app frame classes in `app/globals.css`.
- Bottom navigation classes in `AppShell`.
- Reusable waveform markup or CSS class where recording and playback surfaces need consistent audio visuals.
- Shared visual tokens through CSS custom properties for background, panel, border, text, muted text, accent, danger, success, and shadow.

Avoid nested cards and avoid creating decorative elements that fight the existing workflows. The first screen must be the usable app.

## Data Flow

The redesign should preserve current Redux state and async thunks:

- `restoreSession`, `fetchUserData`, `saveRecording`, recording state transitions, feed fetch/publish/reactions, and profile/interests state continue to work as currently implemented.
- Navigation still uses `navigateToTab`, `openProfile`, `openAuth`, and existing screen state.
- Playback state continues to use existing `isPlaying` and `playbackPosition` logic.

No data model changes are planned.

## Error Handling

Existing errors should remain visible and readable inside the mobile shell:

- Microphone permission and recording errors.
- Save, auth, questions, topic guidance, study words, feed, reaction, and profile errors.
- Empty and loading states for generated questions, recordings, feed posts, comments, and transcripts.

The redesign should not hide disabled states or quota notices.

## Accessibility And Responsive Requirements

- The app must fit in a mobile-width shell on desktop and still use the full available width on narrow phones.
- Text must not overflow buttons, cards, or chips.
- Buttons and tab targets should remain comfortable for touch.
- Native forms and audio controls must remain keyboard reachable.
- Color contrast should remain readable over frosted surfaces.
- The bottom navigation must not cover important content; content should have enough bottom padding.

## Testing

Verification should include:

- `npm run typecheck`.
- `npm run lint`.
- Local browser review at desktop width and mobile width.
- Smoke interaction checks for tab navigation, start/stop recording path where browser permissions allow, profile/auth navigation, history/details playback UI, and feed screen rendering.

If microphone permission or local services block full recording/API verification, document the blocker and still verify the UI paths that can be exercised locally.

## Implementation Scope

Expected primary files:

- `src/components/AppShell.tsx`
- `src/components/SpeakScreen.tsx`
- `src/components/DetailsScreen.tsx`
- `src/components/HistoryScreen.tsx`
- `src/components/FeedScreen.tsx`
- `src/components/ProfileScreen.tsx`
- `src/components/AuthScreen.tsx`
- `src/components/InterestsScreen.tsx`
- `app/globals.css`

Secondary files may be touched only if an existing screen requires a small markup class addition for the new visual system.
