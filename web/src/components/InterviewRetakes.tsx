"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { FocusedFeedback, Recording } from "../lib/data";
import type { SavedInterviewTurn } from "../lib/interviewTimeline";
import { apiFetch, readApiJSON } from "../lib/apiClient";
import { newIdempotencyKey, uploadMedia } from "../lib/mediaUpload";
import { parseFocusedFeedback } from "../lib/focusedFeedback";
import { useProtectedMediaURL } from "../lib/useProtectedMediaURL";
import { interviewPracticeNavigation, interviewPracticeTurns } from "../lib/interviewPractice";
import ArtifactAudioButton from "./ArtifactAudioButton";
import LocalSpeechRecorder from "./LocalSpeechRecorder";
import FocusedFeedbackReview, { FeedbackLegend } from "./FocusedFeedbackReview";

type Attempt = {
  id: string;
  status: string;
  turnSequence: number;
  transcript: string;
  createdAt: string;
  durationSeconds: number;
  error?: string;
  audioAssetId: string;
  feedback?: FocusedFeedback;
};

type PendingUpload = { blob: Blob; key: string; assetId?: string };

const parseAttempt = (value: unknown): Attempt => {
  if (!value || typeof value !== "object") throw new Error("Некорректная попытка.");
  const source = value as Record<string, unknown>;
  if (typeof source.id !== "string"
    || !["processing", "ready", "failed"].includes(String(source.status))
    || !Number.isSafeInteger(source.turnSequence)
    || typeof source.audioAssetId !== "string") {
    throw new Error("Некорректная попытка.");
  }
  return {
    id: source.id,
    status: String(source.status),
    turnSequence: Number(source.turnSequence),
    transcript: typeof source.transcript === "string" ? source.transcript : "",
    createdAt: String(source.createdAt),
    durationSeconds: Number(source.durationSeconds),
    error: typeof source.error === "string" ? source.error : undefined,
    audioAssetId: source.audioAssetId,
    feedback: parseFocusedFeedback(source.focusedFeedback),
  };
};

const attemptRequest = async (path: string, init?: RequestInit): Promise<Record<string, unknown>> => {
  const response = await apiFetch(path, init);
  const body = await readApiJSON<Record<string, unknown>>(response);
  if (!body) throw new Error("Некорректный ответ сервиса.");
  if (!response.ok) {
    throw new Error((body.error as { message?: string })?.message || "Не удалось загрузить попытки.");
  }
  return body;
};

function AttemptAudio({ assetId }: { assetId: string }) {
  const media = useProtectedMediaURL(`/api/v1/media/${encodeURIComponent(assetId)}/download`);
  const audio = useRef<HTMLAudioElement | null>(null);
  useEffect(() => {
    const player = audio.current;
    return () => player?.pause();
  }, [media.url]);
  return <>
    {media.url && <audio ref={audio} controls preload="metadata" aria-label="Запись этой попытки" src={media.url} onCanPlay={media.reportReady} onError={media.reportError} />}
    {media.error && <button type="button" className="practice-action-button" onClick={media.retry}>Загрузить запись снова</button>}
  </>;
}

function AttemptEntry({ attempt, turn, base, initiallyOpen, retry }: {
  attempt: Attempt;
  turn: SavedInterviewTurn;
  base: string;
  initiallyOpen: boolean;
  retry: (attempt: Attempt) => Promise<void>;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  return <details className="retake-attempt" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
    <summary>
      <span>Повторный ответ</span>
      <span className="retake-attempt-meta">
        {new Date(attempt.createdAt).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}
        {attempt.durationSeconds > 0 && ` · ${Math.round(attempt.durationSeconds)} с`}
      </span>
    </summary>
    {open && <div className="retake-attempt-body">
      <AttemptAudio assetId={attempt.audioAssetId} />
      {attempt.status === "processing" && <p role="status">Распознаём и разбираем новый ответ…</p>}
      {attempt.status === "failed" && <div>
        <p className="auth-error">{attempt.error || "Разбор не удался."}</p>
        <button type="button" className="practice-action-button" onClick={() => { void retry(attempt); }}>Повторить разбор</button>
      </div>}
      {attempt.feedback
        ? <FocusedFeedbackReview feedback={attempt.feedback} transcript={attempt.transcript} turns={[{ ...turn, answerText: attempt.transcript }]} audioBase={`${base}/${attempt.id}/feedback`} showLegend={false} showQuestion={false} />
        : attempt.transcript && <p className="transcript-text">{attempt.transcript}</p>}
    </div>}
  </details>;
}

// A mounted thread belongs to one question. Closing it cancels reads and
// playback; an already submitted save can still finish on the server.
function QuestionRetakeThread({ recording, turn, initialDraft, onDraftChange, onBusyChange, uploads }: {
  recording: Recording; turn: SavedInterviewTurn; initialDraft?: Blob;
  onDraftChange: (blob: Blob | null) => void; onBusyChange: (busy: boolean) => void;
  uploads: Map<number, PendingUpload>;
}) {
  const [attempts, setAttempts] = useState<Attempt[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [pollEpoch, setPollEpoch] = useState(0);
  const listController = useRef<AbortController | null>(null);
  const paginated = useRef(false);
  const base = `/api/v1/recordings/${encodeURIComponent(recording.id)}/interview-turns/${turn.sequence}/attempts`;

  const load = useCallback(async (signal?: AbortSignal, before?: string) => {
    signal ??= listController.current?.signal;
    if (signal?.aborted) return;
    setLoading(true);
    try {
      const body = await attemptRequest(`${base}?limit=20${before ? `&before=${encodeURIComponent(before)}` : ""}`, { signal });
      if (signal?.aborted) return;
      const items = Array.isArray(body.items) ? body.items.map(parseAttempt) : [];
      setAttempts(current => before
        ? [...current, ...items.filter(item => !current.some(existing => existing.id === item.id))]
        : [...items, ...current.filter(item => !items.some(next => next.id === item.id))]);
      if (before || !paginated.current) setHasMore(items.length === 20);
      if (before) paginated.current = true;
      setError("");
    } catch (failure) {
      if (!signal?.aborted) setError(failure instanceof Error ? failure.message : "Попытки недоступны.");
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [base]);

  useEffect(() => {
    const controller = new AbortController();
    listController.current = controller;
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  const processingIDs = attempts.filter(attempt => attempt.status === "processing").map(attempt => attempt.id).join(",");
  useEffect(() => {
    if (!processingIDs) return;
    const controller = new AbortController();
    let polls = 0;
    let busy = false;
    const refresh = async () => {
      if (busy || controller.signal.aborted) return;
      if (++polls > 240) {
        clearInterval(timer);
        setError("Разбор занимает больше времени. Обновите состояние попытки.");
        return;
      }
      busy = true;
      try {
        const updates = await Promise.all(processingIDs.split(",").map(async id => {
          const body = await attemptRequest(`${base}/${encodeURIComponent(id)}`, { signal: controller.signal });
          return parseAttempt(body.attempt);
        }));
        if (!controller.signal.aborted) {
          setAttempts(current => current.map(item => updates.find(next => next.id === item.id) ?? item));
        }
      } catch (failure) {
        if (!controller.signal.aborted) setError(failure instanceof Error ? failure.message : "Не удалось обновить разбор.");
      } finally {
        busy = false;
      }
    };
    const timer = setInterval(() => { void refresh(); }, 2500);
    return () => { clearInterval(timer); controller.abort(); };
  }, [processingIDs, base, pollEpoch]);

  const save = async (blob: Blob) => {
    const scope = listController.current?.signal;
    let pending = uploads.get(turn.sequence);
    if (!pending || pending.blob !== blob) {
      pending = { blob, key: newIdempotencyKey("retake") };
      uploads.set(turn.sequence, pending);
    }
    pending.assetId ??= await uploadMedia({ blob, purpose: "interview_attempt_audio", idempotencyKey: `${pending.key}:audio` });
    const body = await attemptRequest(base, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ audioAssetId: pending.assetId, idempotencyKey: pending.key }),
    });
    if (scope?.aborted) return;
    const attempt = parseAttempt(body.attempt);
    setAttempts(current => [attempt, ...current.filter(item => item.id !== attempt.id)]);
    uploads.delete(turn.sequence);
    onDraftChange(null);
  };

  const retry = async (attempt: Attempt) => {
    const signal = listController.current?.signal;
    if (signal?.aborted) return;
    setError("");
    try {
      const body = await attemptRequest(`${base}/${attempt.id}/retry`, { method: "POST", signal });
      if (signal?.aborted) return;
      const next = parseAttempt(body.attempt);
      setAttempts(current => current.map(item => item.id === next.id ? next : item));
    } catch (failure) {
      if (!signal?.aborted) setError(failure instanceof Error ? failure.message : "Повтор не удался.");
    }
  };

  return <div className="retake-thread">
    <section className="retake-compose" aria-label="Новый ответ">
      <ArtifactAudioButton path={`/api/v1/recordings/${recording.id}/interview-turns/${turn.sequence}/question-audio`} label="Прослушать вопрос" />
      <LocalSpeechRecorder maxSeconds={600} onSave={save} saveLabel="Отправить на разбор"
        initialDraft={initialDraft} onDraftChange={onDraftChange} onBusyChange={onBusyChange} />
      <span className="practice-action-hint">Ответ до 10 минут</span>
    </section>

    <section className="retake-history" aria-label="История ответов на этот вопрос">
      <h3>История ответов</h3>
      {loading && <p className="hint" role="status">Загружаем попытки…</p>}
      {error && <div className="retake-load-error">
        <p className="auth-error" role="alert">{error}</p>
        <button type="button" className="practice-action-button" onClick={() => { setPollEpoch(value => value + 1); void load(); }}>Обновить</button>
      </div>}
      {(recording.focusedFeedback || attempts.some(attempt => attempt.feedback)) && <FeedbackLegend />}
      {attempts.map((attempt, index) => <AttemptEntry key={attempt.id} attempt={attempt} turn={turn} base={base} initiallyOpen={index === 0} retry={retry} />)}
      {hasMore && <button type="button" className="practice-action-button" disabled={loading} onClick={() => { void load(undefined, attempts[attempts.length - 1]?.id); }}>Предыдущие попытки</button>}
      <details className="retake-attempt retake-original" open>
        <summary><span>Исходный ответ</span></summary>
        <div className="retake-attempt-body">
          {recording.focusedFeedback
            ? <FocusedFeedbackReview feedback={recording.focusedFeedback} transcript={turn.answerText} turns={[turn]} audioBase={`/api/v1/recordings/${recording.id}/feedback`} showLegend={false} showQuestion={false} />
            : <p className="transcript-text">{turn.answerText}</p>}
        </div>
      </details>
    </section>
  </div>;
}

export default function InterviewRetakes({ recording, onOpen }: { recording: Recording; onOpen?: () => void }) {
  const [sequence, setSequence] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const drafts = useRef(new Map<number, Blob>());
  const uploads = useRef(new Map<number, PendingUpload>());
  const dialog = useRef<HTMLDialogElement | null>(null);
  const titleID = useId();
  const turns = interviewPracticeTurns(recording.interviewTurns);
  const navigation = interviewPracticeNavigation(turns, sequence);
  const turn = navigation?.turn;
  const isOpen = Boolean(turn);
  const rememberDraft = useCallback((blob: Blob | null) => {
    if (sequence === null) return;
    if (blob) drafts.current.set(sequence, blob);
    else drafts.current.delete(sequence);
  }, [sequence]);

  useEffect(() => {
    const modal = dialog.current;
    if (!isOpen || !modal) return;
    const opener = document.activeElement as HTMLElement | null;
    modal.showModal();
    return () => {
      modal.close();
      if (opener?.isConnected) opener.focus({ preventScroll: true });
    };
  }, [isOpen]);

  const moveTo = (target: number | null) => {
    if (busy || target === null) return;
    setSequence(target);
    dialog.current?.scrollTo({ top: 0, behavior: "instant" });
  };

  return <section className="details-material-card details-retake-material retake-launcher">
    <div className="material-heading">
      <div className="material-kicker">Повторная практика</div>
      <h3>Исправить свои ответы</h3>
    </div>
    <p className="retake-launcher-hint">Ответьте ещё раз после разбора и shadowing. Вопросы можно переключать прямо в окне практики.</p>
    <div className="retake-question-list">
      {turns.map((item, index) =>
        <button key={item.sequence} type="button" className="retake-question-open"
          aria-haspopup="dialog" aria-label={`Открыть практику по вопросу ${index + 1}: ${item.question}`}
          onClick={() => { onOpen?.(); setBusy(false); setSequence(item.sequence); }}>
          <span className="retake-question-number">{index + 1}</span>
          <span className="retake-question-text">{item.question}</span>
          <span className="retake-question-arrow" aria-hidden="true">↗</span>
        </button>,
      )}
    </div>
    <dialog ref={dialog} className="focus-dialog retake-dialog" aria-labelledby={titleID}
      onCancel={event => { if (event.target === event.currentTarget) { event.preventDefault(); setSequence(null); } }}
      onClose={event => { if (event.target === event.currentTarget && !dialog.current?.open) setSequence(null); }}
      onClick={event => {
        if (event.target !== event.currentTarget) return;
        const bounds = event.currentTarget.getBoundingClientRect();
        if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) setSequence(null);
      }}>
      {turn && navigation && <>
        <header className="retake-dialog-header">
          <nav className="retake-question-navigation" aria-label="Навигация по вопросам">
            <button type="button" className="practice-action-button retake-navigation-button"
              aria-label="Предыдущий вопрос" disabled={busy || navigation.previous === null} onClick={() => moveTo(navigation.previous)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><path d="m14 6-6 6 6 6" /></svg>
            </button>
            <span className="material-kicker" aria-live="polite" aria-atomic="true">Вопрос {navigation.position} из {navigation.total}</span>
            <button type="button" className="practice-action-button retake-navigation-button"
              aria-label="Следующий вопрос" disabled={busy || navigation.next === null} onClick={() => moveTo(navigation.next)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><path d="m10 6 6 6-6 6" /></svg>
            </button>
          </nav>
          <h2 id={titleID}>{turn.question}</h2>
          {busy && <p className="retake-navigation-hint" role="status">Завершите запись или дождитесь отправки, чтобы перейти к другому вопросу.</p>}
          <button type="button" className="focus-close" autoFocus aria-label="Закрыть практику по вопросу" onClick={() => setSequence(null)}>×</button>
        </header>
        <QuestionRetakeThread key={turn.sequence} recording={recording} turn={turn}
          initialDraft={drafts.current.get(turn.sequence)} onDraftChange={rememberDraft} onBusyChange={setBusy} uploads={uploads.current} />
      </>}
    </dialog>
  </section>;
}
