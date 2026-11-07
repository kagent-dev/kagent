-- +goose Up
ALTER TABLE session ADD COLUMN turn_sender JSONB;

-- +goose Down
ALTER TABLE session DROP COLUMN turn_sender;
