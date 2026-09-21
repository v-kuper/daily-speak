"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { buildInterviewQuestions, moveInterviewQuestion } from "../lib/interviewGuidance";

type InterviewQuestionCardProps = {
  topic: string;
  followUps: string[];
};

export default function InterviewQuestionCard({ topic, followUps }: InterviewQuestionCardProps) {
  const questions = useMemo(() => buildInterviewQuestions(topic, followUps), [followUps, topic]);
  const [currentIndex, setCurrentIndex] = useState(0);
  const hintId = useId();

  useEffect(() => {
    setCurrentIndex(0);
  }, [topic]);

  useEffect(() => {
    if (currentIndex >= questions.length) {
      setCurrentIndex(Math.max(0, questions.length - 1));
    }
  }, [currentIndex, questions.length]);

  if (questions.length === 0) {
    return null;
  }

  const isFirst = currentIndex === 0;
  const isComplete = currentIndex === questions.length - 1;
  const progress = ((currentIndex + 1) / questions.length) * 100;
  const move = (direction: -1 | 1) => {
    setCurrentIndex((index) => moveInterviewQuestion(index, direction, questions.length));
  };

  return (
    <div className="interview-question-panel">
      <button
        className="interview-question-card"
        type="button"
        onClick={() => move(1)}
        aria-describedby={hintId}
      >
        <span aria-live="polite">{questions[currentIndex]}</span>
      </button>

      <div id={hintId} className="interview-question-hint">
        {isComplete
          ? "Interview complete — continue speaking or stop the recording."
          : "Tap the question when you are ready for the next one."}
      </div>

      <div className="interview-question-navigation">
        <button
          className="interview-question-arrow"
          type="button"
          onClick={() => move(-1)}
          disabled={isFirst}
          aria-label="Previous interview question"
        >
          ←
        </button>
        <span className="interview-question-count" aria-live="polite">
          {isComplete ? "Complete · " : ""}
          {currentIndex + 1}/{questions.length}
        </span>
        <button
          className="interview-question-arrow"
          type="button"
          onClick={() => move(1)}
          disabled={isComplete}
          aria-label="Next interview question"
        >
          →
        </button>
      </div>

      <div
        className="interview-question-progress"
        role="progressbar"
        aria-label="Interview progress"
        aria-valuemin={1}
        aria-valuemax={questions.length}
        aria-valuenow={currentIndex + 1}
      >
        <div className="interview-question-progress-fill" style={{ width: `${progress}%` }} />
      </div>
    </div>
  );
}
