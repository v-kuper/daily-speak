"use client";
import { useEffect, useRef, useState } from "react";
import { requestArtifactAudio } from "../lib/artifactAudio";
import PracticeActionIcon from "./PracticeActionIcon";
import { trackQuestionAudio } from "../lib/activityTracking";

export default function ArtifactAudioButton({ path, label = "Прослушать", ariaLabel }: { path: string; label?: string; ariaLabel?: string }) {
 const [state, setState] = useState<"idle" | "loading" | "playing" | "error">("idle");
 const [error, setError] = useState("");
 const controller = useRef<AbortController | null>(null);
 const audio = useRef<HTMLAudioElement | null>(null);
 const busy = useRef(false);
 const stopTracking = useRef<(() => void) | null>(null);
 useEffect(() => () => { controller.current?.abort(); audio.current?.pause(); stopTracking.current?.(); stopTracking.current = null; audio.current = null; busy.current = false; }, [path]);
 const listen = async () => {
  if (busy.current) return;
  busy.current = true; const request = new AbortController(); controller.current = request;
  setState("loading"); setError("");
  try {
   const ticket = await requestArtifactAudio(path, { signal: request.signal, retryFailed: true });
   if (request.signal.aborted) return;
   const player = new Audio(ticket.url); audio.current = player;
   stopTracking.current?.();
   stopTracking.current = path.includes("/questions/") ? trackQuestionAudio(player) : null;
   player.onended = () => { if (!request.signal.aborted) { setState("idle"); busy.current = false; } };
   player.onerror = () => { if (!request.signal.aborted) { setState("error"); setError("Не удалось воспроизвести аудио. Нажмите ещё раз."); busy.current = false; } };
   await player.play(); if (!request.signal.aborted) setState("playing");
  } catch (failure) {
   if (!request.signal.aborted) { setState("error"); setError(failure instanceof Error ? failure.message : "Аудио недоступно."); busy.current = false; }
  }
 };
 const stop = () => { controller.current?.abort(); audio.current?.pause(); stopTracking.current?.(); stopTracking.current = null; busy.current = false; setState("idle"); };
 return <div className="focus-audio">
  <button type="button" className="practice-action-button" disabled={state === "loading"} aria-busy={state === "loading"}
   aria-label={state === "playing" ? "Остановить воспроизведение" : state === "error" ? "Повторить воспроизведение" : ariaLabel ?? (label === "Прослушать" ? "Прослушать правильный вариант" : label)}
   onClick={() => { if (state === "playing") stop(); else void listen(); }}>
   {state === "loading" ? <span className="focus-audio-spinner" aria-hidden="true" /> : <PracticeActionIcon action={state === "playing" ? "stop" : state === "error" ? "retry" : "play"} />}
   <span>{state === "loading" ? "Готовим…" : state === "playing" ? "Остановить" : state === "error" ? "Ещё раз" : label}</span>
  </button>
  <span role="status" className="practice-action-status">{state === "loading" ? "Готовим аудио" : state === "playing" ? "Воспроизведение" : ""}</span>
  {error && <p role="alert" className="auth-error">{error}</p>}
 </div>;
}
