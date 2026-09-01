ALTER TABLE runtime_revision
    RENAME COLUMN actor_template_namespace TO actor_template_atespace;

ALTER TABLE runtime_revision
    DROP COLUMN phase,
    DROP COLUMN golden_snapshot;
