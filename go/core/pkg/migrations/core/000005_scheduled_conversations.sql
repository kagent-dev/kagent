-- +goose Up
-- A generation identifies one uninterrupted period with continuity enabled.
ALTER TABLE scheduled_run ADD COLUMN conversation_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE scheduled_run_execution
    ADD COLUMN conversation_generation bigint,
    ADD COLUMN reservation_order bigint GENERATED ALWAYS AS IDENTITY;
CREATE INDEX scheduled_conversation_queue ON scheduled_run_execution
    (scheduled_run_id, conversation_generation, reservation_order)
    WHERE conversation_generation IS NOT NULL AND completed_at IS NULL;
CREATE INDEX scheduled_conversation_session ON scheduled_run_execution
    (scheduled_run_id, conversation_generation, reservation_order DESC)
    WHERE conversation_generation IS NOT NULL AND session_id IS NOT NULL;

-- +goose Down
ALTER TABLE scheduled_run_execution DROP COLUMN conversation_generation, DROP COLUMN reservation_order;
ALTER TABLE scheduled_run DROP COLUMN conversation_generation;
