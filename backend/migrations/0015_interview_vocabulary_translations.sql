ALTER TABLE interview_sessions
  ADD COLUMN useful_vocabulary JSONB NOT NULL DEFAULT '[]'::jsonb;
