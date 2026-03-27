-- Add session.source column, introduced after the initial GORM-managed schema.
-- ALTER TABLE ... ADD COLUMN IF NOT EXISTS is idempotent: a no-op on fresh installs
-- where migration 000001 created the table without this column, and adds the column
-- on existing GORM-managed deployments upgrading to golang-migrate.
ALTER TABLE session ADD COLUMN IF NOT EXISTS source TEXT;
CREATE INDEX IF NOT EXISTS idx_session_source ON session(source);
