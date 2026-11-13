-- +goose Up

-- When a claimed suspension ends with an unknown outcome, the claim is kept so
-- that no second worker issues runtime work over the top of it. Recording when
-- the claim was taken lets a later pass tell an in-flight suspension from one
-- that has been stranded, and ask Substrate what actually happened.
--
-- A claim with no time recorded is never taken over: it could belong to a
-- replica that has not been upgraded and is still inside its call. The claims
-- that exist at upgrade are dated here instead, so the ones that are genuinely
-- stranded become eligible once the age has passed rather than immediately.
ALTER TABLE session_task_event ADD COLUMN quiescence_claimed_at TIMESTAMPTZ;
UPDATE session_task_event SET quiescence_claimed_at = clock_timestamp()
WHERE quiescence_pending AND quiescence_executor_id IS NOT NULL;

-- +goose Down

ALTER TABLE session_task_event DROP COLUMN quiescence_claimed_at;
