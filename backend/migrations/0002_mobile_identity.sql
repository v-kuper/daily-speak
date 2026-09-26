CREATE TABLE principals (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('guest', 'user')),
  user_id TEXT UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ,
  merged_into_principal_id TEXT REFERENCES principals(id) ON DELETE CASCADE,
  merged_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT principals_shape_check CHECK (
    (kind = 'user' AND user_id IS NOT NULL AND expires_at IS NULL)
    OR
    (kind = 'guest' AND user_id IS NULL AND expires_at IS NOT NULL)
  ),
  CONSTRAINT principals_merge_check CHECK (
    (merged_into_principal_id IS NULL AND merged_at IS NULL)
    OR
    (kind = 'guest' AND merged_into_principal_id IS NOT NULL AND merged_at IS NOT NULL)
  )
);

INSERT INTO principals (id, kind, user_id)
SELECT id, 'user', id
FROM users
ON CONFLICT (id) DO NOTHING;

CREATE UNIQUE INDEX principals_active_user_idx
  ON principals (user_id)
  WHERE kind = 'user';
CREATE INDEX principals_guest_expiry_idx
  ON principals (expires_at)
  WHERE kind = 'guest' AND merged_into_principal_id IS NULL;

CREATE OR REPLACE FUNCTION create_user_principal()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
  INSERT INTO principals (id, kind, user_id)
  VALUES (NEW.id, 'user', NEW.id)
  ON CONFLICT (id) DO NOTHING;
  RETURN NEW;
END
$$;

CREATE TRIGGER users_create_principal
AFTER INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION create_user_principal();

CREATE TABLE device_sessions (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL UNIQUE,
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  device_name TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  revocation_reason TEXT,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX device_sessions_principal_created_idx
  ON device_sessions (principal_id, created_at DESC);
CREATE INDEX device_sessions_active_expiry_idx
  ON device_sessions (expires_at)
  WHERE revoked_at IS NULL;

CREATE TABLE refresh_tokens (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES device_sessions(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  replaced_by_token_id TEXT REFERENCES refresh_tokens(id) ON DELETE SET NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX refresh_tokens_session_created_idx
  ON refresh_tokens (session_id, created_at DESC);
CREATE INDEX refresh_tokens_active_expiry_idx
  ON refresh_tokens (expires_at)
  WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE principal_merges (
  guest_principal_id TEXT PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
  user_principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  merged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (guest_principal_id <> user_principal_id)
);

CREATE INDEX principal_merges_user_idx
  ON principal_merges (user_principal_id, merged_at DESC);
