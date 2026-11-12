-- +goose Up

-- Crash recovery is its own lifecycle operation, started only after Substrate
-- confirms that the recorded Actor crashed.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE', 'RUNTIME_OPERATION_REVERT'));

-- +goose Down

-- Fails while any recovery is still in flight, rather than rewriting its
-- operation into one the older binary would execute differently.
ALTER TABLE runtime_instance DROP CONSTRAINT runtime_instance_operation_check;
ALTER TABLE runtime_instance ADD CONSTRAINT runtime_instance_operation_check
    CHECK (operation IN ('RUNTIME_OPERATION_NONE', 'RUNTIME_OPERATION_CREATE', 'RUNTIME_OPERATION_SUSPEND',
        'RUNTIME_OPERATION_RESUME', 'RUNTIME_OPERATION_DELETE'));
