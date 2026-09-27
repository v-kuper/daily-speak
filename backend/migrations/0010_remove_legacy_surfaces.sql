-- The sandbox web client and native clients now use only /api/v1 and private
-- media assets. Remove the retired Feed experiment and public-URL storage
-- columns instead of carrying compatibility state into new features.
DROP TABLE IF EXISTS feed_reply_reactions;
DROP TABLE IF EXISTS feed_post_reactions;
DROP TABLE IF EXISTS feed_replies;
DROP TABLE IF EXISTS feed_posts;
DROP TABLE IF EXISTS pending_file_deletions;

ALTER TABLE recordings
  DROP COLUMN IF EXISTS audio_data_url,
  DROP COLUMN IF EXISTS photo_data_url,
  DROP COLUMN IF EXISTS shadowing_audio_url;

DELETE FROM media_assets
WHERE purpose = 'feed_reply_audio'
   OR legacy_public_url IS NOT NULL;

DROP INDEX IF EXISTS media_assets_owner_legacy_url_uidx;

ALTER TABLE media_assets
  DROP CONSTRAINT IF EXISTS media_assets_legacy_url_check,
  DROP COLUMN IF EXISTS legacy_public_url;

ALTER TABLE media_assets
  DROP CONSTRAINT IF EXISTS media_assets_purpose_check;

ALTER TABLE media_assets
  ADD CONSTRAINT media_assets_purpose_check CHECK (
    purpose IN (
      'recording_audio',
      'recording_photo',
      'shadowing_audio',
      'guest_preview_audio'
    )
  );
