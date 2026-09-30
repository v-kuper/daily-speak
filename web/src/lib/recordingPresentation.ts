import type { Recording } from "./data";

const countLabel = (count: number, forms: [string, string, string]): string => {
  const lastTwo = count % 100;
  const last = count % 10;
  const word = lastTwo >= 11 && lastTwo <= 14 ? forms[2]
    : last === 1 ? forms[0] : last >= 2 && last <= 4 ? forms[1] : forms[2];
  return `${count} ${word}`;
};

export const recordingCountLabel = (count: number): string => countLabel(count, ["запись", "записи", "записей"]);

export const recordingTitle = (recording: Recording): string =>
  recording.interviewTurns?.[0]?.question.trim()
  || (recording.topic === "Free talk" ? "Свободная практика" : recording.topic);

export const recordingPracticeLabel = (recording: Recording): string => {
  switch (recording.practiceType) {
    case "free_talk": return "Свободная практика";
    case "photo_description": return "Описание фото";
    default: return "Интервью";
  }
};

export const recordingFeedbackSummary = (recording: Recording): string | null => {
  if (recording.status !== "ready") return null;
  if (recording.focusedFeedback) {
    const errors = recording.focusedFeedback.answers.reduce((count, answer) =>
      count + answer.items.filter(item => item.kind === "blocker").length, 0);
    return errors ? countLabel(errors, ["ошибка", "ошибки", "ошибок"]) : "Без ошибок";
  }
  return null;
};

export const recordingHistoryStatus = (recording: Recording): string => {
  if (recording.status === "failed") return "Разбор не завершён";
  if (recording.status === "ready") return "Разбор готов";
  switch (recording.processingStage) {
    case "transcribing": return "Распознаём речь";
    case "suggestions": return "Разбираем ответ";
    case "rewriting": return "Готовим shadowing";
    default: return "Обрабатываем запись";
  }
};
