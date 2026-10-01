"use client";

import { useEffect, useRef } from "react";
import { apiFetch } from "./apiClient";
import { browserIdentity, restoreBrowserIdentity } from "./identity";
import { useAppSelector } from "../store/hooks";
import { ActivityClock, type ActivityInterval, type ActivityKind } from "./activity";

const storagePrefix = "daily-speaking-activity-v1:";
let clock: ActivityClock | null = null;
let owner: string | null = null;
let inFlight: Promise<void> | null = null;
const sources = new Map<string, { kind: ActivityKind; principal: string }>();
// Separate per-event keys avoid lost updates between tabs sharing an account.
const memory = new Map<string, ActivityInterval>();
const pending = (principal: string): ActivityInterval[] => {
  const prefix = `${storagePrefix}${principal}:`;
  const items = new Map<string, ActivityInterval>();
  for (const [key, item] of memory) if (key.startsWith(prefix)) items.set(item.id, item);
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (!key?.startsWith(prefix)) continue;
      try {
        const item = JSON.parse(localStorage.getItem(key) ?? "null") as ActivityInterval;
        if (item && typeof item.id === "string" && (item.kind === "speaking" || item.kind === "review") &&
          Number.isFinite(Date.parse(item.startedAt)) && Number.isFinite(Date.parse(item.endedAt))) items.set(item.id, item);
        else localStorage.removeItem(key);
      } catch { localStorage.removeItem(key); }
    }
  } catch { /* Storage can be unavailable in a private browsing session. */ }
  return [...items.values()].sort((a, b) => a.startedAt.localeCompare(b.startedAt)).slice(0, 50);
};

const enqueue = (principal: string, kind: ActivityKind, from: number, to: number) => {
  const item: ActivityInterval = { id: crypto.randomUUID(), kind, startedAt: new Date(from).toISOString(), endedAt: new Date(to).toISOString() };
  const key = `${storagePrefix}${principal}:${item.id}`;
  memory.set(key, item);
  try { localStorage.setItem(key, JSON.stringify(item)); } catch { /* Keep the in-memory retry copy. */ }
};

export const flushActivity = (): Promise<void> => {
  if (inFlight) return inFlight;
  const identity = browserIdentity();
  if (identity?.kind !== "user") return Promise.resolve();
  const principal = identity.principalId;
  const operation = (async () => {
    // Drain bounded requests. An offline failure leaves every key for retry.
    while (browserIdentity()?.principalId === principal) {
      const intervals = pending(principal);
      if (!intervals.length) return;
      let identity = browserIdentity();
      if (identity?.principalId !== principal) return;
      if (Date.parse(identity.accessTokenExpiresAt) <= Date.now() + 30_000) identity = await restoreBrowserIdentity();
      if (identity.principalId !== principal) return;
      const send = (token: string) => apiFetch("/api/v1/profile/activity", {
        method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
        body: JSON.stringify({ intervals }), keepalive: true,
      });
      let response = await send(identity.accessToken);
      if (response.status === 401 && browserIdentity()?.principalId === principal) {
        identity = await restoreBrowserIdentity();
        if (identity.principalId !== principal) return;
        response = await send(identity.accessToken);
      }
      if (!response.ok) throw new Error("Время практики ожидает синхронизации.");
      for (const item of intervals) {
        const key = `${storagePrefix}${principal}:${item.id}`;
        memory.delete(key);
        try { localStorage.removeItem(key); } catch { /* Idempotency makes a repeated send harmless. */ }
      }
    }
  })();
  inFlight = operation.finally(() => { inFlight = null; });
  return inFlight;
};

export const startActivity = (kind: ActivityKind): (() => void) => {
  const id = crypto.randomUUID();
  const identity = browserIdentity();
  if (identity?.kind !== "user") return () => undefined;
  sources.set(id, { kind, principal: identity.principalId });
  if (owner === identity.principalId) clock?.start(id, kind, Date.now());
  return () => {
    if (owner === sources.get(id)?.principal) clock?.stop(id, Date.now());
    sources.delete(id);
    void flushActivity().catch(() => undefined);
  };
};

export const interactWithReview = () => clock?.interact(Date.now());

// Playback state, rather than a button/loading state, owns question-listening
// time. Buffering, errors and early stops close the active interval immediately.
export const trackQuestionAudio = (player: HTMLAudioElement): (() => void) => {
  let stop: (() => void) | null = null;
  const playing = () => { if (!stop) stop = startActivity("speaking"); };
  const paused = () => { stop?.(); stop = null; };
  player.addEventListener("playing", playing);
  const endings = ["pause", "ended", "waiting", "error", "emptied"];
  for (const event of endings) player.addEventListener(event, paused);
  return () => {
    paused(); player.removeEventListener("playing", playing);
    for (const event of endings) player.removeEventListener(event, paused);
  };
};

export const useActivityTracking = () => {
  const { isAuthenticated, userEmail } = useAppSelector(state => state.app);
  useEffect(() => {
    const identity = browserIdentity();
    if (!isAuthenticated || identity?.kind !== "user") return;
    owner = identity.principalId;
    const principal = owner;
    const tracker = new ActivityClock(Date.now(), (kind, from, to) => enqueue(principal, kind, from, to));
    tracker.setVisible(!document.hidden, Date.now()); clock = tracker;
    for (const [id, source] of sources) if (source.principal === principal) tracker.start(id, source.kind, Date.now());
    const sync = () => { tracker.settle(Date.now()); void flushActivity().catch(() => undefined); };
    const visibility = () => { tracker.setVisible(!document.hidden, Date.now()); void flushActivity().catch(() => undefined); };
    const leave = () => { tracker.setVisible(false, Date.now()); void flushActivity().catch(() => undefined); };
    const returnToPage = () => tracker.setVisible(!document.hidden, Date.now());
    const timer = window.setInterval(sync, 15_000);
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", leave); window.addEventListener("pageshow", returnToPage);
    window.addEventListener("online", sync);
    void flushActivity().catch(() => undefined);
    return () => {
      tracker.setVisible(false, Date.now());
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pagehide", leave); window.removeEventListener("pageshow", returnToPage);
      window.removeEventListener("online", sync);
      if (clock === tracker) { clock = null; owner = null; }
      void flushActivity().catch(() => undefined);
    };
  }, [isAuthenticated, userEmail]);
};

export const useSpeakingActivity = (active: boolean) => {
  const isAuthenticated = useAppSelector(state => state.app.isAuthenticated);
  useEffect(() => {
    if (active && isAuthenticated) return startActivity("speaking");
  }, [active, isAuthenticated]);
};

export const useReviewActivity = (active: boolean) => {
  const ref = useRef<HTMLElement>(null);
  const isAuthenticated = useAppSelector(state => state.app.isAuthenticated);
  useEffect(() => {
    const element = ref.current;
    if (!active || !isAuthenticated || !element) return;
    const stop = startActivity("review");
    let lastPointer = 0;
    const interact = (event: Event) => {
      if (!event.isTrusted) return;
      if (event.type === "pointermove" && Date.now() - lastPointer < 1000) return;
      lastPointer = Date.now(); interactWithReview();
    };
    const events = ["pointerdown", "pointermove", "keydown", "scroll", "input"];
    for (const event of events) element.addEventListener(event, interact, { capture: true, passive: true });
    return () => {
      stop();
      for (const event of events) element.removeEventListener(event, interact, true);
    };
  }, [active, isAuthenticated]);
  return ref;
};
