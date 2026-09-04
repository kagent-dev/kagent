-- Baseline schema for clean kagent installations.
--
-- Notes on column definitions vs. what you might expect:
--   - created_at/updated_at are nullable: GORM sets these in Go code, not via a
--     DB default or NOT NULL constraint.
--   - access_count is BIGINT: GORM maps Go `int` to bigint.

CREATE TABLE IF NOT EXISTS tool (
    id          TEXT        NOT NULL,
    server_name TEXT        NOT NULL,
    group_kind  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ,
    description TEXT,
    PRIMARY KEY (id, server_name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_tool_deleted_at ON tool(deleted_at);

CREATE TABLE IF NOT EXISTS toolserver (
    name           TEXT        NOT NULL,
    group_kind     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ,
    deleted_at     TIMESTAMPTZ,
    description    TEXT,
    last_connected TIMESTAMPTZ,
    PRIMARY KEY (name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_toolserver_deleted_at ON toolserver(deleted_at);
