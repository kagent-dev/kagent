-- +goose Up
ALTER TABLE agent_revision
    ADD COLUMN bootstrap_bundle BYTEA CHECK (octet_length(bootstrap_bundle) <= 263168),
    ADD COLUMN preparing_until TIMESTAMPTZ,
    ADD CONSTRAINT bootstrap_publication CHECK ((bootstrap_bundle IS NULL) = (preparing_until IS NULL));

-- +goose Down
ALTER TABLE agent_revision
    DROP CONSTRAINT bootstrap_publication,
    DROP COLUMN preparing_until,
    DROP COLUMN bootstrap_bundle;
