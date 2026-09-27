-- A recording can be marked shadowing-ready only after its private media asset
-- has been published. Retired public-URL rows lost that asset during cleanup;
-- return them to the normal generation flow instead of exposing a false-ready
-- state to clients.
UPDATE recordings
SET shadowing_status = 'pending',
    shadowing_error = NULL,
    shadowing_updated_at = NOW(),
    shadowing_attempt_id = NULL
WHERE shadowing_status = 'ready'
  AND shadowing_asset_id IS NULL;

ALTER TABLE recordings
  DROP CONSTRAINT IF EXISTS recordings_shadowing_ready_asset_check;

ALTER TABLE recordings
  ADD CONSTRAINT recordings_shadowing_ready_asset_check CHECK (
    shadowing_status <> 'ready' OR shadowing_asset_id IS NOT NULL
  );
