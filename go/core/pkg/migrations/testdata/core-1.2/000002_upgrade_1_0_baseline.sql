-- +goose Up

-- Kagent 1.0 recorded version 1 with an earlier 000001 baseline: no runtime
-- revision credentials, no agent instance operation and executor, no pinned
-- checkpoint, no DELETED state. This brings that baseline to the current
-- one and leaves a database created by the current 000001 unchanged. A
-- schema that matches neither baseline fails the migration.
--
-- agent_instance_checkpoint.source_name stays: the 1.0 controller reads and
-- writes it, the current controller never names it, so a migrated database
-- runs either release.

-- +goose StatementBegin
DO $$
DECLARE
    current_columns integer;
BEGIN
    SELECT count(*) INTO current_columns
    FROM pg_attribute
    WHERE NOT attisdropped AND (
        (attrelid = 'runtime_revision'::regclass AND attname = 'credentials') OR
        (attrelid = 'agent_instance'::regclass
            AND attname IN ('pinned_checkpoint_id', 'operation_id', 'executor_id')));
    IF current_columns = 4 THEN
        RETURN;
    END IF;
    IF current_columns <> 0 THEN
        RAISE EXCEPTION 'core schema version 1 matches neither the Kagent 1.0 nor the current baseline: % of 4 current columns exist',
            current_columns;
    END IF;

    ALTER TABLE runtime_revision
        ADD COLUMN credentials JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(credentials) = 'array');

    ALTER TABLE agent_instance
        DROP CONSTRAINT agent_instance_source_checkpoint_id_fkey,
        DROP CONSTRAINT agent_instance_state_check,
        ADD COLUMN pinned_checkpoint_id UUID GENERATED ALWAYS AS (
            CASE WHEN state <> 'AGENT_INSTANCE_STATE_DELETED' THEN source_checkpoint_id END
        ) STORED REFERENCES agent_instance_checkpoint(id) ON DELETE RESTRICT,
        ADD COLUMN operation_id UUID,
        ADD COLUMN executor_id UUID,
        ADD CONSTRAINT agent_instance_check CHECK (executor_id IS NULL OR (operation_id IS NOT NULL
            AND operation <> 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
            AND state <> 'AGENT_INSTANCE_STATE_DELETED')),
        ADD CONSTRAINT agent_instance_check1 CHECK (state <> 'AGENT_INSTANCE_STATE_DELETED' OR
            (prepared_revision IS NULL AND operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED')),
        ADD CONSTRAINT agent_instance_state_check
            CHECK (state IN ('AGENT_INSTANCE_STATE_CREATING', 'AGENT_INSTANCE_STATE_READY',
                'AGENT_INSTANCE_STATE_SUSPENDED', 'AGENT_INSTANCE_STATE_FAILED', 'AGENT_INSTANCE_STATE_DELETED'));

    DROP INDEX agent_instance_user_id_id_idx;
    CREATE INDEX agent_instance_user_id_id_idx
        ON agent_instance (user_id, id) WHERE state <> 'AGENT_INSTANCE_STATE_DELETED';
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Nothing to undo: both baselines meet here, the result still runs the 1.0
-- controller, and 000001's Down drops the tables.
