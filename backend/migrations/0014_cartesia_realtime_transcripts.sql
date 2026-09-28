DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM processing_jobs job
    WHERE job.state IN ('queued', 'running', 'retry_wait')
      AND (
        (
          job.kind = 'recording.process'
          AND EXISTS (
            SELECT 1 FROM interview_sessions session
            WHERE session.recording_id = job.resource_id
          )
        )
        OR (
          job.kind = 'guest.preview'
          AND EXISTS (
            SELECT 1 FROM interview_sessions session
            WHERE session.guest_preview_id = job.resource_id
          )
        )
      )
  ) THEN
    RAISE EXCEPTION
      'drain active legacy interview recording and guest preview jobs before migration 0014';
  END IF;
END
$$;

ALTER TABLE interview_turns
  ADD COLUMN transcript_idempotency_key TEXT,
  ADD COLUMN transcript_origin TEXT,
  ADD COLUMN corrected_answer_text TEXT;

ALTER TABLE interview_turns
  ADD CONSTRAINT interview_turns_transcript_origin_check CHECK (
    transcript_origin IS NULL OR transcript_origin IN ('turn_realtime', 'turn_batch', 'full_audio_alignment')
  ),
  ADD CONSTRAINT interview_turns_corrected_answer_check CHECK (
    corrected_answer_text IS NULL
    OR (length(btrim(corrected_answer_text)) BETWEEN 1 AND 4000)
  );

CREATE UNIQUE INDEX interview_turns_transcript_key_idx
  ON interview_turns(session_id, transcript_idempotency_key)
  WHERE transcript_idempotency_key IS NOT NULL;

UPDATE interview_turns
SET transcript_origin = CASE
  WHEN final_transcript IS NOT NULL THEN 'full_audio_alignment'
  WHEN provisional_transcript IS NOT NULL AND transcript_status = 'ready' THEN 'turn_batch'
  ELSE NULL
END
WHERE transcript_origin IS NULL
  AND (final_transcript IS NOT NULL
       OR (provisional_transcript IS NOT NULL AND transcript_status = 'ready'));
