-- +goose Up

-- Crash recovery is its own lifecycle operation, started only after Substrate
-- confirms that the recorded Actor crashed.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE', 'RUNTIME_OPERATION_REVERT'));

-- The durable snapshot a crash recovery admitted, for the REVERT operation
-- named by recovery_operation_id. It is valid only while that operation is the
-- Session's current one, so a later operation needs no cleanup. The URI is
-- empty when the crashed Actor had no durable snapshot.
ALTER TABLE session
    ADD COLUMN recovery_operation_id UUID,
    ADD COLUMN recovery_snapshot_uri TEXT,
    ADD CONSTRAINT session_recovery_snapshot_check
        CHECK ((recovery_operation_id IS NULL) = (recovery_snapshot_uri IS NULL));

-- +goose Down

ALTER TABLE session
    DROP CONSTRAINT session_recovery_snapshot_check,
    DROP COLUMN recovery_snapshot_uri,
    DROP COLUMN recovery_operation_id;

-- Fails while any recovery is still in flight, rather than rewriting its
-- operation into one the older binary would execute differently.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE'));
