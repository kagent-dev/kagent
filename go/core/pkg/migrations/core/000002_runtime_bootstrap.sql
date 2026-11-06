-- +goose Up
ALTER TABLE agent_revision
    ADD COLUMN bootstrap_bundle BYTEA CHECK (octet_length(bootstrap_bundle) <= 263168);

-- +goose Down
ALTER TABLE agent_revision DROP COLUMN bootstrap_bundle;
