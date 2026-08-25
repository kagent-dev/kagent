CREATE TABLE IF NOT EXISTS agent_instance_checkpoint (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    -- Provenance only: checkpoints outlive their source and may initialize other Actors.
    source_instance_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    head_task_id TEXT NOT NULL,
    history_sequence BIGINT NOT NULL,
    snapshot_atespace TEXT NOT NULL,
    snapshot_name TEXT NOT NULL,
    snapshot_uid TEXT NOT NULL,
    snapshot_content_scope TEXT NOT NULL,
    tag_uid TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    failure TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (snapshot_content_scope IN ('FULL', 'DATA')),
    CHECK (state IN ('CREATING', 'READY', 'FAILED', 'DELETING')),
    UNIQUE (user_id, namespace, request_id)
);

CREATE INDEX IF NOT EXISTS agent_instance_checkpoint_list_idx
    ON agent_instance_checkpoint (namespace, source_instance_id, id);

CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_checkpoint_one_creating_idx
    ON agent_instance_checkpoint (source_instance_id)
    WHERE state = 'CREATING';
