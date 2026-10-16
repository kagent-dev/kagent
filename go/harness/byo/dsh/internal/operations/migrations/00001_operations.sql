-- +goose Up
CREATE TABLE dsh_approval_operations (
    scope TEXT NOT NULL,
    id UUID NOT NULL,
    request_key TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    binding TEXT NOT NULL,
    tool TEXT NOT NULL,
    arguments JSONB NOT NULL CHECK (jsonb_typeof(arguments) = 'object'),
    state TEXT NOT NULL CHECK (state IN ('awaiting_approval', 'approved', 'rejected', 'expired', 'outcome_unknown', 'succeeded', 'failed')),
    result JSONB,
    decided_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    notification TEXT NOT NULL DEFAULT 'pending' CHECK (notification IN ('pending', 'delivery_unknown', 'delivered')),
    PRIMARY KEY (scope, id),
    UNIQUE (scope, request_key),
    CHECK ((state IN ('succeeded', 'failed')) = (result IS NOT NULL))
);
CREATE INDEX dsh_approval_work ON dsh_approval_operations (scope, state, created_at);

-- +goose Down
DROP TABLE dsh_approval_operations;
