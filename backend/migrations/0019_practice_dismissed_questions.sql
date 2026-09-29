CREATE TABLE practice_dismissed_questions (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  question_key TEXT NOT NULL,
  question TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, question_key)
);

CREATE INDEX practice_dismissed_questions_recent_idx
  ON practice_dismissed_questions (user_id, created_at DESC);

CREATE INDEX interview_sessions_user_history_idx
  ON interview_sessions (user_id, created_at DESC)
  WHERE user_id IS NOT NULL;
