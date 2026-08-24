CREATE TABLE IF NOT EXISTS agent_instance_checkpoint (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    head_task_id TEXT NOT NULL,
    history_sequence BIGINT NOT NULL,
    snapshot_atespace TEXT NOT NULL,
    snapshot_name TEXT NOT NULL,
    snapshot_uid TEXT NOT NULL,
    tag_name TEXT NOT NULL,
    tag_uid TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    failure TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (state IN ('CREATING', 'READY', 'FAILED')),
    UNIQUE (user_id, namespace, request_id),
    UNIQUE (snapshot_atespace, tag_name)
);

CREATE INDEX IF NOT EXISTS agent_instance_checkpoint_list_idx
    ON agent_instance_checkpoint (namespace, instance_id, id);
