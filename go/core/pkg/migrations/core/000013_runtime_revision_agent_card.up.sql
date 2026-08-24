ALTER TABLE runtime_revision
    ADD COLUMN IF NOT EXISTS agent_card JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE runtime_revision
    ALTER COLUMN agent_card DROP DEFAULT;
