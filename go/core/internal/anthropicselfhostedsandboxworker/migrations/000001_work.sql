-- +goose Up
CREATE TABLE sandbox_work (
    id text PRIMARY KEY,
    environment text NOT NULL,
    session_id text NOT NULL,
    credential bytea,
    acknowledged boolean NOT NULL DEFAULT false,
    heartbeat text NOT NULL DEFAULT '',
    sandbox_id text NOT NULL DEFAULT '',
    sandbox_generation bigint NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_token uuid,
    lease_until timestamptz,
    finished_at timestamptz,
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK ((finished_at IS NULL) = (credential IS NOT NULL))
);
CREATE INDEX sandbox_work_ready ON sandbox_work (available_at, id) WHERE finished_at IS NULL;

CREATE TABLE sandbox_command (
    work_id text NOT NULL REFERENCES sandbox_work(id) ON DELETE CASCADE,
    id text NOT NULL,
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    name text NOT NULL,
    input jsonb,
    phase text NOT NULL DEFAULT 'queued',
    process_id text,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    result text,
    is_error boolean NOT NULL DEFAULT false,
    discard boolean NOT NULL DEFAULT false,
    PRIMARY KEY (work_id, id),
    CHECK (phase IN ('queued', 'starting', 'running', 'result', 'done')),
    CHECK ((phase = 'running') = (process_id IS NOT NULL)),
    CHECK ((phase = 'result') = (result IS NOT NULL)),
    CHECK ((phase = 'done') = (input IS NULL))
);
CREATE INDEX sandbox_command_ready ON sandbox_command (work_id, sequence) WHERE phase <> 'done';

-- +goose Down
DROP TABLE sandbox_command;
DROP TABLE sandbox_work;
