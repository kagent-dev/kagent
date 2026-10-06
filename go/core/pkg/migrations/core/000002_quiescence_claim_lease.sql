-- +goose Up

-- A quiescence claim's lease. The holder renews it while it works; a claim whose
-- lease has run out, or that has none, is claimed again to settle its boundary.
ALTER TABLE session_task_event ADD COLUMN quiescence_claimed_until TIMESTAMPTZ,
    ADD CONSTRAINT session_task_event_quiescence_lease_held
        CHECK (quiescence_claimed_until IS NULL OR quiescence_executor_id IS NOT NULL);

-- +goose Down

ALTER TABLE session_task_event DROP CONSTRAINT session_task_event_quiescence_lease_held,
    DROP COLUMN quiescence_claimed_until;
