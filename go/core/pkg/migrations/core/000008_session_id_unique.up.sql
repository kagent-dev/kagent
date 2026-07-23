-- A session's client-supplied id was only unique per (id, user_id), so two
-- different users could create sessions with the same id. Deleting one then
-- cascaded to the other user's tasks and events, which are keyed by session_id
-- alone. This index makes id unique among live sessions so that can't happen.
CREATE UNIQUE INDEX IF NOT EXISTS session_id_active_unique ON session (id) WHERE deleted_at IS NULL;
