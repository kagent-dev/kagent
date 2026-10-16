-- +goose Up

-- The groups of a session's owner as their last turn carried them, sent on a
-- turn a share admits so the calls the agent makes for the owner are reviewed
-- with the owner's groups. Not part of the Session a reader sees.
ALTER TABLE session ADD COLUMN IF NOT EXISTS owner_groups TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE session DROP COLUMN owner_groups;
