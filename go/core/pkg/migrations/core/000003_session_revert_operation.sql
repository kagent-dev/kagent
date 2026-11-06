-- +goose Up

-- Crash recovery is its own lifecycle operation, started only after Substrate
-- confirms that the recorded Actor crashed.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE', 'RUNTIME_OPERATION_REVERT'));

-- The durable snapshot the Session's current crash recovery admitted. Only
-- admission starts a REVERT and it always writes this column, so it is read
-- only while that operation is current. The URI is empty when the crashed
-- Actor had no durable snapshot.
ALTER TABLE session ADD COLUMN recovery_snapshot_uri TEXT;

-- +goose Down

ALTER TABLE session DROP COLUMN recovery_snapshot_uri;

-- Fails while any recovery is still in flight, rather than rewriting its
-- operation into one the older binary would execute differently.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE'));
