ALTER TABLE interview_turns
  ADD COLUMN skipped BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN skip_key TEXT;

CREATE UNIQUE INDEX interview_turns_skip_key_idx
  ON interview_turns(session_id, skip_key)
  WHERE skip_key IS NOT NULL;
