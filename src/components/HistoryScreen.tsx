"use client";

import { formatTime, formatTimeOfDay, recordingDateKey } from "../lib/utils";
import { useAppDispatch, useAppSelector } from "../store/hooks";
import {
  clearSelectedDate,
  nextMonth,
  openDetails,
  previousMonth,
  setSelectedDate,
  toggleCalendar
} from "../store/slices/appSlice";

const DAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const dateKeyFromParts = (year: number, month: number, day: number): string => {
  const mm = String(month + 1).padStart(2, "0");
  const dd = String(day).padStart(2, "0");
  return `${year}-${mm}-${dd}`;
};

const formatPracticeLabel = (value: "free_talk" | "topic" | "photo_description"): string => {
  switch (value) {
    case "free_talk":
      return "Free talk";
    case "photo_description":
      return "Photo description";
    default:
      return "Topic";
  }
};

export default function HistoryScreen() {
  const dispatch = useAppDispatch();
  const { recordings, selectedDate, calendarVisible, calendarMonth, calendarYear } = useAppSelector(
    (state) => state.app
  );

  const firstDay = new Date(calendarYear, calendarMonth, 1);
  const lastDay = new Date(calendarYear, calendarMonth + 1, 0);
  const daysInMonth = lastDay.getDate();
  const startingDayOfWeek = firstDay.getDay();

  const selectedDateRecordings = selectedDate
    ? recordings.filter((recording) => recordingDateKey(recording) === selectedDate)
    : recordings.slice(0, 10);

  const visibleRecordings = [...selectedDateRecordings].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  );

  return (
    <section className="screen-section history-screen">
      <h2>History</h2>

      <div className="calendar-wrapper">
        <button className="btn btn-secondary btn-small" onClick={() => dispatch(toggleCalendar())}>
          📅 Select date
        </button>

        {selectedDate && (
          <button className="btn btn-secondary btn-small" onClick={() => dispatch(clearSelectedDate())}>
            Show latest
          </button>
        )}

        <div className={`calendar ${calendarVisible ? "visible" : ""}`}>
          <div className="calendar-header">
            <button onClick={() => dispatch(previousMonth())}>←</button>
            <h3>{firstDay.toLocaleString("default", { month: "long", year: "numeric" })}</h3>
            <button onClick={() => dispatch(nextMonth())}>→</button>
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
              const hasRecordings = recordings.some((recording) => recordingDateKey(recording) === dateString);
              const isSelected = selectedDate === dateString;

              return (
                <button
                  key={dateString}
                  className={`calendar-day ${hasRecordings ? "has-recordings" : ""} ${
                    isSelected ? "selected" : ""
                  }`}
                  onClick={() => dispatch(setSelectedDate(dateString))}
                >
                  {day}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      {visibleRecordings.length === 0 ? (
        <div className="empty-state">No recordings on this day.</div>
      ) : (
        visibleRecordings.map((recording) => (
          <button key={recording.id} className="recording-card" onClick={() => dispatch(openDetails(recording.id))}>
            <div className="recording-card-header">
              <div className="recording-main">
                <div className="recording-time">{formatTimeOfDay(recording.timestamp)}</div>
                <div className="recording-topic">{recording.topic}</div>
                <div className="recording-practice-tag">{formatPracticeLabel(recording.practiceType)}</div>
              </div>
              <div className="recording-side">
                <div className="recording-duration">{formatTime(recording.duration)}</div>
                {recording.photoDataUrl && (
                  <img src={recording.photoDataUrl} alt="Photo from recording" className="recording-thumb" />
                )}
              </div>
            </div>
          </button>
        ))
      )}
    </section>
  );
}
