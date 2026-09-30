ALTER TABLE recordings
  ADD COLUMN analysis_pipeline TEXT NOT NULL DEFAULT 'legacy-v2',
  ADD COLUMN focused_feedback JSONB;
-- Existing jobs retain their pipeline. All subsequently created recordings,
-- including promoted previews, enter the focused pipeline.
ALTER TABLE recordings ALTER COLUMN analysis_pipeline SET DEFAULT 'focused-v1';

ALTER TABLE media_assets DROP CONSTRAINT media_assets_purpose_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_purpose_check CHECK (
  purpose IN ('recording_audio', 'recording_photo', 'shadowing_audio',
    'guest_preview_audio', 'interview_turn_audio', 'interview_attempt_audio',
    'interview_question_audio', 'interview_question_manifest', 'feedback_audio')
);

ALTER TABLE processing_jobs DROP CONSTRAINT processing_jobs_kind_check;
ALTER TABLE processing_jobs ADD CONSTRAINT processing_jobs_kind_check CHECK (
  kind IN ('recording.process', 'recording.strengths', 'recording.feedback_audio',
    'shadowing.synthesize', 'media.delete', 'guest.preview', 'interview.process')
);

CREATE TABLE interview_question_artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
  question_index INTEGER NOT NULL CHECK (question_index > 0),
  question TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'failed')),
  audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
  manifest_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
  job_id TEXT,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(session_id, question_index),
  UNIQUE(session_id, question)
);
ALTER TABLE interview_sessions ADD COLUMN opening_question_artifact_id TEXT
  REFERENCES interview_question_artifacts(id) ON DELETE SET NULL;
ALTER TABLE interview_candidates ADD COLUMN question_artifact_id TEXT
  REFERENCES interview_question_artifacts(id) ON DELETE SET NULL;
ALTER TABLE interview_turns ADD COLUMN question_artifact_id TEXT
  REFERENCES interview_question_artifacts(id) ON DELETE SET NULL;

CREATE TABLE interview_answer_attempts (
  id TEXT PRIMARY KEY,
  recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES interview_sessions(id) ON DELETE CASCADE,
  turn_seq INTEGER NOT NULL CHECK (turn_seq > 0),
  owner_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  create_key TEXT NOT NULL,
  audio_asset_id TEXT NOT NULL UNIQUE REFERENCES media_assets(id),
  transcript TEXT NOT NULL DEFAULT '',
  focused_feedback JSONB,
  duration_seconds INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'failed')),
  job_id TEXT NOT NULL,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(recording_id, create_key)
);
CREATE INDEX interview_answer_attempts_turn_idx
  ON interview_answer_attempts(recording_id, turn_seq, created_at DESC, id DESC);

CREATE TABLE recording_feedback_audio (
  id TEXT PRIMARY KEY,
  recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
  attempt_id TEXT REFERENCES interview_answer_attempts(id) ON DELETE CASCADE,
  feedback_id TEXT NOT NULL,
  practice_text TEXT NOT NULL,
  owner_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing', 'ready', 'failed')),
  audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
  job_id TEXT NOT NULL,
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX recording_feedback_audio_focus_idx
  ON recording_feedback_audio(recording_id, COALESCE(attempt_id, ''), feedback_id);

CREATE TABLE recording_feedback_reanalysis_requests (
  recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
  request_key TEXT NOT NULL,
  job_id TEXT NOT NULL,
  PRIMARY KEY(recording_id, request_key)
);
