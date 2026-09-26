CREATE TABLE media_assets (
  id TEXT PRIMARY KEY,
  owner_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  purpose TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  storage_driver TEXT NOT NULL,
  bucket TEXT,
  object_key TEXT NOT NULL,
  content_type TEXT NOT NULL,
  expected_size_bytes BIGINT,
  verified_size_bytes BIGINT,
  expected_checksum_sha256 TEXT,
  verified_checksum_sha256 TEXT,
  etag TEXT,
  legacy_public_url TEXT,
  retention_until TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  attached_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT media_assets_purpose_check CHECK (
    purpose IN ('recording_audio', 'recording_photo', 'shadowing_audio', 'feed_reply_audio')
  ),
  CONSTRAINT media_assets_state_check CHECK (
    state IN ('pending', 'uploading', 'uploaded', 'verifying', 'ready', 'failed', 'deleting', 'deleted')
  ),
  CONSTRAINT media_assets_storage_driver_check CHECK (
    storage_driver IN ('local', 's3')
  ),
  CONSTRAINT media_assets_object_key_check CHECK (
    length(btrim(object_key)) > 0
    AND object_key !~ '(^|/)\.\.(/|$)'
    AND left(object_key, 1) <> '/'
    AND position(chr(92) IN object_key) = 0
  ),
  CONSTRAINT media_assets_content_type_check CHECK (length(btrim(content_type)) > 0),
  CONSTRAINT media_assets_expected_size_check CHECK (
    expected_size_bytes IS NULL OR expected_size_bytes > 0
  ),
  CONSTRAINT media_assets_verified_size_check CHECK (
    verified_size_bytes IS NULL OR verified_size_bytes > 0
  ),
  CONSTRAINT media_assets_expected_checksum_check CHECK (
    expected_checksum_sha256 IS NULL OR expected_checksum_sha256 ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT media_assets_verified_checksum_check CHECK (
    verified_checksum_sha256 IS NULL OR verified_checksum_sha256 ~ '^[0-9a-f]{64}$'
  ),
  CONSTRAINT media_assets_verified_values_check CHECK (
    (expected_size_bytes IS NULL OR verified_size_bytes IS NULL OR expected_size_bytes = verified_size_bytes)
    AND
    (expected_checksum_sha256 IS NULL OR verified_checksum_sha256 IS NULL OR expected_checksum_sha256 = verified_checksum_sha256)
    AND
    (verified_at IS NULL OR (verified_size_bytes IS NOT NULL AND verified_checksum_sha256 IS NOT NULL))
  ),
  CONSTRAINT media_assets_legacy_url_check CHECK (
    legacy_public_url IS NULL OR legacy_public_url LIKE '/uploads/%'
  ),
  CONSTRAINT media_assets_storage_shape_check CHECK (
    (storage_driver = 'local' AND bucket IS NULL)
    OR
    (storage_driver = 's3' AND length(btrim(bucket)) > 0)
  ),
  CONSTRAINT media_assets_deleted_shape_check CHECK (
    (state = 'deleted' AND deleted_at IS NOT NULL)
    OR
    (state <> 'deleted')
  )
);

CREATE UNIQUE INDEX media_assets_storage_object_uidx
  ON media_assets (storage_driver, COALESCE(bucket, ''), object_key);

CREATE UNIQUE INDEX media_assets_owner_legacy_url_uidx
  ON media_assets (owner_principal_id, legacy_public_url)
  WHERE legacy_public_url IS NOT NULL;

CREATE INDEX media_assets_owner_created_idx
  ON media_assets (owner_principal_id, created_at DESC);

CREATE INDEX media_assets_state_retention_idx
  ON media_assets (state, retention_until)
  WHERE state IN ('pending', 'uploading', 'uploaded', 'verifying', 'failed', 'deleting');

CREATE TABLE media_uploads (
  id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
  provider_upload_id TEXT,
  state TEXT NOT NULL DEFAULT 'pending',
  part_size_bytes BIGINT NOT NULL,
  part_count INTEGER NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_by_session_id TEXT REFERENCES device_sessions(id) ON DELETE SET NULL,
  idempotency_key TEXT NOT NULL,
  completed_at TIMESTAMPTZ,
  aborted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT media_uploads_state_check CHECK (
    state IN ('pending', 'uploading', 'completing', 'aborting', 'completed', 'aborted', 'expired', 'failed')
  ),
  CONSTRAINT media_uploads_part_size_check CHECK (part_size_bytes > 0),
  CONSTRAINT media_uploads_part_count_check CHECK (part_count BETWEEN 1 AND 10000),
  CONSTRAINT media_uploads_idempotency_key_check CHECK (length(btrim(idempotency_key)) > 0),
  CONSTRAINT media_uploads_expiry_check CHECK (expires_at > created_at),
  CONSTRAINT media_uploads_terminal_shape_check CHECK (
    (state = 'completed' AND completed_at IS NOT NULL AND aborted_at IS NULL)
    OR
    (state = 'aborted' AND aborted_at IS NOT NULL AND completed_at IS NULL)
    OR
    (state NOT IN ('completed', 'aborted') AND completed_at IS NULL AND aborted_at IS NULL)
  )
);

CREATE UNIQUE INDEX media_uploads_active_asset_uidx
  ON media_uploads (asset_id)
  WHERE state IN ('pending', 'uploading', 'completing', 'aborting');

CREATE UNIQUE INDEX media_uploads_session_idempotency_uidx
  ON media_uploads (created_by_session_id, idempotency_key)
  WHERE created_by_session_id IS NOT NULL;

CREATE INDEX media_uploads_expiry_idx
  ON media_uploads (expires_at)
  WHERE state IN ('pending', 'uploading', 'completing', 'aborting', 'failed');

CREATE INDEX media_uploads_session_created_idx
  ON media_uploads (created_by_session_id, created_at DESC)
  WHERE created_by_session_id IS NOT NULL;

CREATE TABLE media_upload_parts (
  upload_id TEXT NOT NULL REFERENCES media_uploads(id) ON DELETE CASCADE,
  part_number INTEGER NOT NULL,
  size_bytes BIGINT NOT NULL,
  etag TEXT NOT NULL,
  checksum_sha256 TEXT,
  verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (upload_id, part_number),
  CONSTRAINT media_upload_parts_number_check CHECK (part_number BETWEEN 1 AND 10000),
  CONSTRAINT media_upload_parts_size_check CHECK (size_bytes > 0),
  CONSTRAINT media_upload_parts_etag_check CHECK (length(btrim(etag)) > 0),
  CONSTRAINT media_upload_parts_checksum_check CHECK (
    checksum_sha256 IS NULL OR checksum_sha256 ~ '^[0-9a-f]{64}$'
  )
);

ALTER TABLE recordings
ADD COLUMN audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE recordings
ADD COLUMN photo_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE recordings
ADD COLUMN shadowing_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE feed_posts
ADD COLUMN audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE feed_posts
ADD COLUMN photo_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE feed_replies
ADD COLUMN audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE recording_upload_sessions
ADD COLUMN audio_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

ALTER TABLE recording_upload_sessions
ADD COLUMN photo_asset_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL;

CREATE INDEX recordings_audio_asset_id_idx
  ON recordings (audio_asset_id)
  WHERE audio_asset_id IS NOT NULL;

CREATE INDEX recordings_photo_asset_id_idx
  ON recordings (photo_asset_id)
  WHERE photo_asset_id IS NOT NULL;

CREATE INDEX recordings_shadowing_asset_id_idx
  ON recordings (shadowing_asset_id)
  WHERE shadowing_asset_id IS NOT NULL;

CREATE INDEX feed_posts_audio_asset_id_idx
  ON feed_posts (audio_asset_id)
  WHERE audio_asset_id IS NOT NULL;

CREATE INDEX feed_posts_photo_asset_id_idx
  ON feed_posts (photo_asset_id)
  WHERE photo_asset_id IS NOT NULL;

CREATE INDEX feed_replies_audio_asset_id_idx
  ON feed_replies (audio_asset_id)
  WHERE audio_asset_id IS NOT NULL;

CREATE INDEX recording_upload_sessions_audio_asset_id_idx
  ON recording_upload_sessions (audio_asset_id)
  WHERE audio_asset_id IS NOT NULL;

CREATE INDEX recording_upload_sessions_photo_asset_id_idx
  ON recording_upload_sessions (photo_asset_id)
  WHERE photo_asset_id IS NOT NULL;

-- md5 is used only to derive stable migration identifiers for existing files;
-- it is not used for authentication or integrity verification. Data URLs are
-- deliberately excluded because only persisted /uploads/* files are objects.
INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(r.user_id || chr(31) || r.audio_data_url),
  r.user_id,
  'recording_audio',
  'ready',
  'local',
  substring(r.audio_data_url FROM 10),
  CASE lower(split_part(r.audio_data_url, '.', -1))
    WHEN 'mp3' THEN 'audio/mpeg'
    WHEN 'm4a' THEN 'audio/mp4'
    WHEN 'mp4' THEN 'audio/mp4'
    WHEN 'wav' THEN 'audio/wav'
    WHEN 'ogg' THEN 'audio/ogg'
    ELSE 'audio/webm'
  END,
  r.audio_data_url,
  r.created_at,
  r.created_at,
  r.created_at
FROM recordings r
WHERE r.audio_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE recordings r
SET audio_asset_id = a.id
FROM media_assets a
WHERE r.audio_asset_id IS NULL
  AND r.audio_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = r.user_id
  AND a.purpose = 'recording_audio'
  AND a.legacy_public_url = r.audio_data_url;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(r.user_id || chr(31) || r.photo_data_url),
  r.user_id,
  'recording_photo',
  'ready',
  'local',
  substring(r.photo_data_url FROM 10),
  CASE lower(split_part(r.photo_data_url, '.', -1))
    WHEN 'png' THEN 'image/png'
    WHEN 'gif' THEN 'image/gif'
    WHEN 'webp' THEN 'image/webp'
    ELSE 'image/jpeg'
  END,
  r.photo_data_url,
  r.created_at,
  r.created_at,
  r.created_at
FROM recordings r
WHERE r.photo_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE recordings r
SET photo_asset_id = a.id
FROM media_assets a
WHERE r.photo_asset_id IS NULL
  AND r.photo_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = r.user_id
  AND a.purpose = 'recording_photo'
  AND a.legacy_public_url = r.photo_data_url;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(r.user_id || chr(31) || r.shadowing_audio_url),
  r.user_id,
  'shadowing_audio',
  'ready',
  'local',
  substring(r.shadowing_audio_url FROM 10),
  'audio/mpeg',
  r.shadowing_audio_url,
  r.shadowing_updated_at,
  r.created_at,
  r.shadowing_updated_at
FROM recordings r
WHERE r.shadowing_audio_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE recordings r
SET shadowing_asset_id = a.id
FROM media_assets a
WHERE r.shadowing_asset_id IS NULL
  AND r.shadowing_audio_url LIKE '/uploads/%'
  AND a.owner_principal_id = r.user_id
  AND a.purpose = 'shadowing_audio'
  AND a.legacy_public_url = r.shadowing_audio_url;

-- Feed posts normally copy the source recording media URL. Reuse those asset
-- rows instead of manufacturing a second owner/object record.
UPDATE feed_posts p
SET audio_asset_id = r.audio_asset_id
FROM recordings r
WHERE p.audio_asset_id IS NULL
  AND p.source_recording_id = r.id
  AND p.audio_data_url LIKE '/uploads/%'
  AND p.audio_data_url = r.audio_data_url
  AND r.audio_asset_id IS NOT NULL;

UPDATE feed_posts p
SET photo_asset_id = r.photo_asset_id
FROM recordings r
WHERE p.photo_asset_id IS NULL
  AND p.source_recording_id = r.id
  AND p.photo_data_url LIKE '/uploads/%'
  AND p.photo_data_url = r.photo_data_url
  AND r.photo_asset_id IS NOT NULL;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(p.user_id || chr(31) || p.audio_data_url),
  p.user_id,
  'recording_audio',
  'ready',
  'local',
  substring(p.audio_data_url FROM 10),
  CASE lower(split_part(p.audio_data_url, '.', -1))
    WHEN 'mp3' THEN 'audio/mpeg'
    WHEN 'm4a' THEN 'audio/mp4'
    WHEN 'mp4' THEN 'audio/mp4'
    WHEN 'wav' THEN 'audio/wav'
    WHEN 'ogg' THEN 'audio/ogg'
    ELSE 'audio/webm'
  END,
  p.audio_data_url,
  p.created_at,
  p.created_at,
  p.created_at
FROM feed_posts p
WHERE p.audio_asset_id IS NULL
  AND p.audio_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE feed_posts p
SET audio_asset_id = a.id
FROM media_assets a
WHERE p.audio_asset_id IS NULL
  AND p.audio_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = p.user_id
  AND a.purpose = 'recording_audio'
  AND a.legacy_public_url = p.audio_data_url;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(p.user_id || chr(31) || p.photo_data_url),
  p.user_id,
  'recording_photo',
  'ready',
  'local',
  substring(p.photo_data_url FROM 10),
  CASE lower(split_part(p.photo_data_url, '.', -1))
    WHEN 'png' THEN 'image/png'
    WHEN 'gif' THEN 'image/gif'
    WHEN 'webp' THEN 'image/webp'
    ELSE 'image/jpeg'
  END,
  p.photo_data_url,
  p.created_at,
  p.created_at,
  p.created_at
FROM feed_posts p
WHERE p.photo_asset_id IS NULL
  AND p.photo_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE feed_posts p
SET photo_asset_id = a.id
FROM media_assets a
WHERE p.photo_asset_id IS NULL
  AND p.photo_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = p.user_id
  AND a.purpose = 'recording_photo'
  AND a.legacy_public_url = p.photo_data_url;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(r.user_id || chr(31) || r.audio_data_url),
  r.user_id,
  'feed_reply_audio',
  'ready',
  'local',
  substring(r.audio_data_url FROM 10),
  CASE lower(split_part(r.audio_data_url, '.', -1))
    WHEN 'mp3' THEN 'audio/mpeg'
    WHEN 'm4a' THEN 'audio/mp4'
    WHEN 'mp4' THEN 'audio/mp4'
    WHEN 'wav' THEN 'audio/wav'
    WHEN 'ogg' THEN 'audio/ogg'
    ELSE 'audio/webm'
  END,
  r.audio_data_url,
  r.created_at,
  r.created_at,
  r.created_at
FROM feed_replies r
WHERE r.audio_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE feed_replies r
SET audio_asset_id = a.id
FROM media_assets a
WHERE r.audio_asset_id IS NULL
  AND r.audio_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = r.user_id
  AND a.purpose = 'feed_reply_audio'
  AND a.legacy_public_url = r.audio_data_url;

INSERT INTO media_assets
  (id, owner_principal_id, purpose, state, storage_driver, object_key,
   content_type, legacy_public_url, attached_at, created_at, updated_at)
SELECT
  'legacy-media-' || md5(s.user_id || chr(31) || s.photo_data_url),
  s.user_id,
  'recording_photo',
  'ready',
  'local',
  substring(s.photo_data_url FROM 10),
  CASE lower(split_part(s.photo_data_url, '.', -1))
    WHEN 'png' THEN 'image/png'
    WHEN 'gif' THEN 'image/gif'
    WHEN 'webp' THEN 'image/webp'
    ELSE 'image/jpeg'
  END,
  s.photo_data_url,
  s.created_at,
  s.created_at,
  s.created_at
FROM recording_upload_sessions s
WHERE s.photo_data_url LIKE '/uploads/%'
ON CONFLICT DO NOTHING;

UPDATE recording_upload_sessions s
SET photo_asset_id = a.id
FROM media_assets a
WHERE s.photo_asset_id IS NULL
  AND s.photo_data_url LIKE '/uploads/%'
  AND a.owner_principal_id = s.user_id
  AND a.purpose = 'recording_photo'
  AND a.legacy_public_url = s.photo_data_url;
