-- +goose Up
CREATE TABLE runtime_bootstrap (
    revision TEXT PRIMARY KEY REFERENCES agent_revision(revision) ON DELETE CASCADE,
    -- 256 KiB combined JSON payload plus 1 KiB for protobuf metadata.
    bundle BYTEA NOT NULL CHECK (octet_length(bundle) <= 263168),
    preparing_until TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE runtime_bootstrap;
