-- A reader-supplied display name for the conversation an AgentInstance is.
-- Deliberately not unique: unlike a Kubernetes name this is a label for a human,
-- and two conversations with the same agent may reasonably carry the same title.
-- The default keeps the column additive — every existing row reads as unnamed.
ALTER TABLE agent_instance
    ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
