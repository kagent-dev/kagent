-- +goose Up
CREATE TABLE runtime_lifecycle_job (
    runtime_id UUID NOT NULL REFERENCES runtime_instance(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    generation UUID NOT NULL,
    phase TEXT NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    executor_id UUID,
    PRIMARY KEY (runtime_id, kind),
    CONSTRAINT runtime_lifecycle_job_phase_check CHECK (
        kind = 'RECOVER_ABANDONED_DISPATCH'
        AND phase IN ('RECOVERING', 'SUSPENDING')
    )
);

CREATE INDEX runtime_lifecycle_job_due_idx
    ON runtime_lifecycle_job (kind, available_at, runtime_id);

CREATE INDEX session_dispatch_pending_idx ON session (id)
    WHERE dispatch_id IS NOT NULL;

-- +goose Down
DROP INDEX session_dispatch_pending_idx;
DROP TABLE runtime_lifecycle_job;
