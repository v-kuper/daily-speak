ALTER TABLE interview_sessions
  ADD COLUMN opening_useful_words JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE interview_candidates
  ADD COLUMN useful_words JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE interview_turns
  ADD COLUMN useful_words JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE interview_sessions
SET opening_useful_words = (
  SELECT COALESCE(jsonb_agg(word.value ORDER BY word.position), '[]'::jsonb)
  FROM jsonb_array_elements(useful_words) WITH ORDINALITY AS word(value, position)
  WHERE word.position <= 10
)
WHERE useful_words <> '[]'::jsonb;

UPDATE interview_turns AS turn
SET useful_words = session.opening_useful_words
FROM interview_sessions AS session
WHERE turn.session_id = session.id
  AND session.opening_useful_words <> '[]'::jsonb;

WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (
           PARTITION BY session_id
           ORDER BY CASE WHEN source = 'adaptive' THEN 0 ELSE 1 END, created_at, id
         ) AS position
  FROM interview_candidates
  WHERE consumed_by_turn_seq IS NULL
)
DELETE FROM interview_candidates AS candidate
USING ranked
WHERE candidate.id = ranked.id
  AND ranked.position > 1;

UPDATE interview_candidates AS candidate
SET useful_words = session.opening_useful_words
FROM interview_sessions AS session
WHERE candidate.session_id = session.id
  AND candidate.consumed_by_turn_seq IS NULL
  AND session.opening_useful_words <> '[]'::jsonb;

CREATE UNIQUE INDEX interview_candidates_one_ready_idx
  ON interview_candidates(session_id)
  WHERE consumed_by_turn_seq IS NULL;
