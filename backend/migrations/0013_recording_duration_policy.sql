-- Existing adaptive sessions keep their persisted duration policy across
-- idempotent create retries. Bring active sessions in line with the current
-- identity policy: guests receive three minutes and accounts ten minutes.
UPDATE interview_sessions
SET max_duration_seconds = CASE WHEN user_id IS NULL THEN 180 ELSE 600 END,
    updated_at = NOW()
WHERE status IN ('preparing', 'ready', 'recording', 'finalizing')
  AND max_duration_seconds <> CASE WHEN user_id IS NULL THEN 180 ELSE 600 END;
