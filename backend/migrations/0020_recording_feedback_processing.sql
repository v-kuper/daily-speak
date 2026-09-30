ALTER TABLE recordings
  ADD COLUMN strengths_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (strengths_status IN ('unknown', 'pending', 'processing', 'ready', 'failed')),
  ADD COLUMN strengths_job_id TEXT,
  ADD COLUMN analysis_checkpoints JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE recordings SET strengths_status = 'ready' WHERE jsonb_array_length(strengths) > 0;
ALTER TABLE recordings ALTER COLUMN strengths_status SET DEFAULT 'pending';

ALTER TABLE processing_jobs DROP CONSTRAINT processing_jobs_kind_check;
ALTER TABLE processing_jobs ADD CONSTRAINT processing_jobs_kind_check CHECK (
  kind IN ('recording.process', 'recording.strengths', 'shadowing.synthesize', 'media.delete', 'guest.preview', 'interview.process')
);
