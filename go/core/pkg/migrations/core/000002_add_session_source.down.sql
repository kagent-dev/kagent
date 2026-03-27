DROP INDEX IF EXISTS idx_session_source;
ALTER TABLE session DROP COLUMN IF EXISTS source;
