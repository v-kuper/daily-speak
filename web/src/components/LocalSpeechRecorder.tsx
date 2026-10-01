"use client";
import { useEffect, useRef, useState } from "react";
import PracticeActionIcon from "./PracticeActionIcon";
import { useSpeakingActivity } from "../lib/activityTracking";

export default function LocalSpeechRecorder({ maxSeconds = 30, onSave, saveLabel = "Сохранить новую попытку", repeatLabel = "Повторить правильный вариант голосом", initialDraft, onDraftChange, onBusyChange }: {
 maxSeconds?: number; onSave?: (blob: Blob) => Promise<void>; saveLabel?: string; repeatLabel?: string;
 initialDraft?: Blob; onDraftChange?: (blob: Blob | null) => void; onBusyChange?: (busy: boolean) => void;
}) {
 const [state, setState] = useState<"idle" | "starting" | "recording" | "ready" | "saving">(initialDraft ? "ready" : "idle");
 useSpeakingActivity(state === "recording");
 const [error, setError] = useState(""); const [url, setURL] = useState<string | null>(null);
 const [seconds, setSeconds] = useState(0);
 const recorder = useRef<MediaRecorder | null>(null); const stream = useRef<MediaStream | null>(null);
 const timer = useRef<ReturnType<typeof setInterval> | null>(null);
 const deadline = useRef<ReturnType<typeof setTimeout> | null>(null);
 const blob = useRef<Blob | null>(initialDraft ?? null);
 const mounted = useRef(true); const urlRef = useRef<string | null>(null);
 const release = () => {
  if (timer.current) clearInterval(timer.current);
  if (deadline.current) clearTimeout(deadline.current);
  timer.current = null; deadline.current = null;
  stream.current?.getTracks().forEach(track => track.stop()); stream.current = null;
 };
 useEffect(() => {
  mounted.current = true;
  if (blob.current) {
   urlRef.current = URL.createObjectURL(blob.current);
   setURL(urlRef.current);
  }
  const stopWhenHidden = () => {
   if (document.hidden && recorder.current?.state === "recording") recorder.current.stop();
  };
  document.addEventListener("visibilitychange", stopWhenHidden);
  return () => {
   mounted.current = false;
   document.removeEventListener("visibilitychange", stopWhenHidden);
   if (recorder.current?.state === "recording") recorder.current.stop();
   release(); if (urlRef.current) URL.revokeObjectURL(urlRef.current);
  };
 }, []);
 useEffect(() => {
  onBusyChange?.(state === "starting" || state === "recording" || state === "saving");
 }, [state, onBusyChange]);
 const start = async () => {
  if (state === "starting" || state === "recording" || state === "saving") return;
  setState("starting"); setError("");
  try {
   const input = await navigator.mediaDevices.getUserMedia({ audio: true });
   if (!mounted.current || document.hidden) {
    input.getTracks().forEach(track => track.stop());
    if (mounted.current) setState("idle");
    return;
   }
   stream.current = input;
   const capture = new MediaRecorder(input); recorder.current = capture; const chunks: BlobPart[] = []; let failed = false;
   capture.ondataavailable = event => { if (event.data.size) chunks.push(event.data); };
   capture.onstop = () => {
    release(); if (!mounted.current) return;
    if (failed || !chunks.length) { setState("idle"); setError("Не удалось записать звук. Попробуйте ещё раз."); return; }
    blob.current = new Blob(chunks, { type: capture.mimeType || "audio/webm" });
    onDraftChange?.(blob.current);
    if (urlRef.current) URL.revokeObjectURL(urlRef.current);
    urlRef.current = URL.createObjectURL(blob.current); setURL(urlRef.current); setState("ready");
   };
   capture.onerror = () => { failed = true; release(); if (mounted.current) { setError("Не удалось записать звук. Попробуйте ещё раз."); setState("idle"); } };
   capture.start(); setSeconds(0); setState("recording");
   const startedAt = performance.now();
   const stopAtLimit = () => { if (capture.state === "recording") capture.stop(); };
   deadline.current = setTimeout(stopAtLimit, maxSeconds * 1000);
   timer.current = setInterval(() => {
    const elapsed = (performance.now() - startedAt) / 1000;
    setSeconds(Math.min(maxSeconds, Math.floor(elapsed)));
    if (elapsed >= maxSeconds) stopAtLimit();
   }, 250);
  } catch { release(); if (mounted.current) { setState("idle"); setError("Разрешите доступ к микрофону и попробуйте ещё раз."); } }
 };
 const save = async () => {
  if (!blob.current || !onSave || state !== "ready") return;
  setState("saving"); setError("");
  try { await onSave(blob.current); if (mounted.current) {
   onDraftChange?.(null);
   setState("idle"); setURL(null); blob.current = null;
   if (urlRef.current) URL.revokeObjectURL(urlRef.current);
   urlRef.current = null;
  } }
  catch (failure) { if (mounted.current) { setState("ready"); setError(failure instanceof Error ? failure.message : "Не удалось сохранить попытку."); } }
 };
 return <div className={`local-repeat${onSave ? " local-answer-recorder" : ""}`}>
  {url && onSave && <audio aria-label="Ваша новая запись" controls src={url} />}
  <div className="local-repeat-controls">
  <button type="button" className={`practice-action-button${state === "recording" ? " practice-action-recording" : ""}`} disabled={state === "starting" || state === "saving"}
   aria-label={state === "recording" ? "Завершить запись" : onSave ? "Записать новый ответ" : repeatLabel}
   aria-pressed={state === "recording"}
   onClick={() => { if (state === "recording") recorder.current?.stop(); else void start(); }}>
   {state === "starting" ? <span className="focus-audio-spinner" aria-hidden="true" /> : <PracticeActionIcon action={state === "recording" ? "stop" : state === "ready" ? "retry" : "microphone"} />}
   <span>{state === "recording" ? "Стоп" : state === "starting" ? "Подключаем…" : state === "ready" ? "Ещё раз" : onSave ? "Записать ответ" : "Повторить"}</span>
   {state === "recording" && <span className="practice-action-time">{seconds} с</span>}
  </button>
  {onSave && state === "ready" && <button className="btn btn-primary local-repeat-save" onClick={() => { void save(); }}>{saveLabel}</button>}
  </div>
  {url && !onSave && <audio aria-label="Ваша новая запись" controls src={url} />}
  {state === "saving" && <p role="status">Сохраняем попытку…</p>}
  {!onSave && state === "idle" && <span className="practice-action-hint">Запись до {maxSeconds} с</span>}
  {error && <p role="alert" className="auth-error">{error}</p>}
 </div>;
}
