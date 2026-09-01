ALTER TABLE runtime_revision
    RENAME COLUMN actor_template_atespace TO actor_template_namespace;

ALTER TABLE runtime_revision
    ADD COLUMN phase TEXT NOT NULL DEFAULT 'Pending',
    ADD COLUMN golden_snapshot TEXT NOT NULL DEFAULT '';

ALTER TABLE runtime_revision
    ALTER COLUMN phase DROP DEFAULT;
