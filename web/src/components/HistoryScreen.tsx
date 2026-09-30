"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { recordingPath } from "../lib/routes";
import { historyDateFromSearch, startHistoryRecordingPolling } from "../lib/routeFlows";

import { useEffect, useId, useMemo, useRef } from "react";
import { recordingCountLabel, recordingFeedbackSummary, recordingHistoryStatus, recordingPracticeLabel, recordingTitle } from "../lib/recordingPresentation";
import { formatTime, recordingDateKey, toDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector, useAppStore } from "../store/hooks";
import RecordingLoadError from "./RecordingLoadError";
import ProtectedMediaImage from "./ProtectedMediaImage";
import {
  nextMonth,
  previousMonth,
  setCalendarDate,
  toggleCalendar
} from "../store/slices/appSlice";

const DAY_LABELS = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

const dateKeyFromParts = (year: number, month: number, day: number): string => {
  const mm = String(month + 1).padStart(2, "0");
  const dd = String(day).padStart(2, "0");
  return `${year}-${mm}-${dd}`;
};

function HistoryIcon({ name }: { name: "calendar" | "clock" | "topic" | "free_talk" | "photo_description" | "arrow" }) {
  return <svg className="history-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {name === "calendar" ? <><rect x="3" y="5" width="18" height="16" rx="3" /><path d="M7 3v4m10-4v4M3 11h18m-12 4h2m3 0h2" /></>
      : name === "clock" ? <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>
      : name === "arrow" ? <path d="m9 5 7 7-7 7" />
      : name === "photo_description" ? <><rect x="3" y="3" width="18" height="18" rx="3" /><circle cx="8" cy="8" r="1.5" /><path d="m21 16-5-5-7 10" /></>
      : name === "free_talk" ? <><rect x="9" y="3" width="6" height="12" rx="3" /><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3m-4 0h8" /></>
      : <><path d="M21 11a8 8 0 0 1-8 8H7l-4 3V11a9 9 0 0 1 18 0Z" /><path d="M8 10h8m-8 4h5" /></>}
  </svg>;
}

export default function HistoryScreen() {
  const dispatch = useAppDispatch();
  const store = useAppStore();
  const router = useRouter();
  const searchParams = useSearchParams();
  const selectedDate = historyDateFromSearch(searchParams);
  const { recordings, recordingSaveError, recordingFetchErrors, calendarVisible, calendarMonth, calendarYear } = useAppSelector(
    (state) => state.app
  );
  const calendarId = useId();
  const calendarWrapper = useRef<HTMLDivElement>(null);
  const calendarTrigger = useRef<HTMLButtonElement>(null);
  useEffect(() => startHistoryRecordingPolling(store), [store]);
  useEffect(() => {
    if (selectedDate) dispatch(setCalendarDate(selectedDate));
  }, [dispatch, selectedDate]);
  useEffect(() => {
    if (!calendarVisible) return;
    const closeOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !calendarWrapper.current?.contains(event.target)) dispatch(toggleCalendar());
    };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, [calendarVisible, dispatch]);

  const dateCounts = useMemo(() => {
    const counts = new Map<string, number>();
    recordings.forEach(recording => {
      const key = recordingDateKey(recording);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    });
    return counts;
  }, [recordings]);

  const firstDay = new Date(calendarYear, calendarMonth, 1);
  const lastDay = new Date(calendarYear, calendarMonth + 1, 0);
  const daysInMonth = lastDay.getDate();
  const startingDayOfWeek = (firstDay.getDay() + 6) % 7;
  const todayKey = toDateKey(new Date());

  const sortedRecordings = [...recordings].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  );
  const visibleRecordings = selectedDate
    ? sortedRecordings.filter(recording => recordingDateKey(recording) === selectedDate)
    : sortedRecordings.slice(0, 10);
  const selectedDateLabel = selectedDate ? new Date(`${selectedDate}T12:00:00`).toLocaleDateString("ru", { day: "numeric", month: "long", year: "numeric" }) : null;
  const chooseDate = (date: string) => {
    router.replace(`/history?date=${date}`);
    if (calendarVisible) dispatch(toggleCalendar());
    calendarTrigger.current?.focus();
  };

  return (
    <section className="history-screen">
      <div className="history-heading"><h2>История</h2><span>{recordingCountLabel(visibleRecordings.length)}</span></div>
      {recordingSaveError && <div className="auth-error" role="alert">{recordingSaveError}</div>}
      {Object.keys(recordingFetchErrors).map((recordingId) => <RecordingLoadError key={recordingId} recordingId={recordingId} />)}

      <div className="calendar-wrapper history-date-filter" ref={calendarWrapper} onKeyDown={event => {
        if (event.key === "Escape" && calendarVisible) {
          event.preventDefault(); dispatch(toggleCalendar()); calendarTrigger.current?.focus();
        }
      }}>
        <div className="history-filter-label">{selectedDateLabel || "Последние записи"}</div>
        <div className="history-filter-actions">
          <button ref={calendarTrigger} type="button" className="history-date-button" aria-expanded={calendarVisible} aria-controls={calendarId} onClick={() => dispatch(toggleCalendar())}>
            <HistoryIcon name="calendar" />По дате
          </button>
          {selectedDate && <button type="button" className="history-filter-reset" onClick={() => {
            router.replace("/history");
            if (calendarVisible) dispatch(toggleCalendar());
          }}>Все записи</button>}
        </div>

        {calendarVisible && <div id={calendarId} className="calendar visible history-calendar" role="region" aria-label="Выбор даты записи">
          <div className="calendar-header">
            <button type="button" aria-label="Предыдущий месяц" onClick={() => dispatch(previousMonth())}>‹</button>
            <h3 aria-live="polite">{firstDay.toLocaleString("ru", { month: "long", year: "numeric" })}</h3>
            <button type="button" aria-label="Следующий месяц" onClick={() => dispatch(nextMonth())}>›</button>
          </div>

          <div className="calendar-grid">
            {DAY_LABELS.map((label) => (
              <div key={label} className="calendar-week-label">
                {label}
              </div>
            ))}

            {Array.from({ length: startingDayOfWeek }).map((_, index) => (
              <div key={`empty-${index}`} className="calendar-day other-month" />
            ))}

            {Array.from({ length: daysInMonth }).map((_, index) => {
              const day = index + 1;
              const dateString = dateKeyFromParts(calendarYear, calendarMonth, day);
              const count = dateCounts.get(dateString) ?? 0;
              const hasRecordings = count > 0;
              const isSelected = selectedDate === dateString;

              return (
                <button
                  key={dateString}
                  type="button"
                  className={`calendar-day ${hasRecordings ? "has-recordings" : ""} ${
                    isSelected ? "selected" : ""
                  } ${dateString === todayKey ? "is-today" : ""}`}
                  aria-label={`${new Date(calendarYear, calendarMonth, day).toLocaleDateString("ru", { day: "numeric", month: "long", year: "numeric" })}, ${recordingCountLabel(count)}`}
                  aria-pressed={isSelected}
                  aria-current={dateString === todayKey ? "date" : undefined}
                  onClick={() => chooseDate(dateString)}
                >
                  {day}
                </button>
              );
            })}
          </div>
          <div className="history-calendar-footer"><span><i aria-hidden="true" />Есть записи</span><button type="button" onClick={() => chooseDate(todayKey)}>Сегодня</button></div>
        </div>}
      </div>

      {visibleRecordings.length === 0 ? (
        <div className="empty-state">{selectedDate ? "В этот день записей пока нет. Выберите другую дату или вернитесь ко всем записям." : "Здесь появятся ваши записи после первой практики."}</div>
      ) : (
        visibleRecordings.map((recording) => {
          const feedbackSummary = recordingFeedbackSummary(recording);
          return (
          <Link key={recording.id} className="recording-card" href={recordingPath(recording.id)}>
            <div className="recording-card-header">
              <span className={`recording-kind-icon recording-kind-${recording.practiceType}`}><HistoryIcon name={recording.practiceType} /></span>
              <div className="recording-main">
                <div className="recording-card-topline">
                  <span className="recording-practice-tag">{recordingPracticeLabel(recording)}</span>
                  <span className={`recording-history-status recording-history-status-${recording.status}`} aria-live={recording.status === "processing" ? "polite" : undefined}>
                    {recording.status === "processing" && <span className="recording-processing-dot" aria-hidden="true" />}
                    {recordingHistoryStatus(recording)}
                  </span>
                </div>
                <h3 className="recording-card-title">{recordingTitle(recording)}</h3>
                <div className="recording-card-meta">
                  <time dateTime={recording.timestamp}>{new Date(recording.timestamp).toLocaleString("ru", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</time>
                  <span aria-label={`Длительность ${formatTime(recording.duration)}`}><HistoryIcon name="clock" />{formatTime(recording.duration)}</span>
                  {feedbackSummary && <span>{feedbackSummary}</span>}
                </div>
              </div>
              <div className="recording-side">
                {(recording.media?.photo || recording.localPhotoDataUrl) && (
                  <ProtectedMediaImage
                    downloadPath={recording.media?.photo?.downloadPath ?? null}
                    localURL={recording.localPhotoDataUrl}
                    alt="Photo from recording"
                    className="recording-thumb"
                  />
                )}
                <span className="recording-open-arrow"><HistoryIcon name="arrow" /></span>
              </div>
            </div>
          </Link>
        ); })
      )}
    </section>
  );
}
