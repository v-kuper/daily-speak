ALTER TABLE recordings
ADD COLUMN strengths JSONB NOT NULL DEFAULT '[]'::jsonb;
