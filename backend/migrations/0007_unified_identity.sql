-- The v1 identity contract is now the only authentication system. Existing
-- browser sessions cannot be converted into rotating device sessions safely,
-- so they are invalidated during the migration.
DROP TABLE IF EXISTS user_sessions;
