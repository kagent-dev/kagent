-- +goose Up
CREATE TABLE push_signing_state (
    issuer text PRIMARY KEY CHECK (issuer <> ''),
    last_activated_key_id text NOT NULL CHECK (last_activated_key_id <> ''),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)
);

-- +goose Down
DROP TABLE push_signing_state;
