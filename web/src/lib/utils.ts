import type { Recording } from "./data";

const datePart = (value: number): string => String(value).padStart(2, "0");

export const formatTime = (seconds: number): string => {
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}:${secs.toString().padStart(2, "0")}`;
};

export const toDateKey = (date: Date): string => {
  return `${date.getFullYear()}-${datePart(date.getMonth() + 1)}-${datePart(date.getDate())}`;
};

export const recordingDateKey = (recording: Recording): string => {
  return toDateKey(new Date(recording.timestamp));
};

export const formatTimeOfDay = (isoDate: string): string => {
  return new Date(isoDate).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit"
  });
};

export const buildShareLink = (recordingId: string): string => {
  if (typeof window === "undefined") {
    return `?share=${recordingId}`;
  }
  const url = new URL(window.location.href);
  url.searchParams.set("share", recordingId);
  return url.toString();
};
