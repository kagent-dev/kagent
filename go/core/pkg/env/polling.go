package env

import "time"

var (
	SessionQuiescencePollInterval     = RegisterDurationVar("KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL", time.Second, "Interval between idle session quiescence scans. Must be positive; longer intervals delay pausing and suspending settled sessions.", ComponentController)
	SandboxExpirationPollInterval     = RegisterDurationVar("KAGENT_SANDBOX_EXPIRATION_POLL_INTERVAL", time.Second, "Interval between expired sandbox cleanup batches. Must be positive; longer intervals delay deletion after TTL expiry.", ComponentController)
	ScheduledRunPollInterval          = RegisterDurationVar("KAGENT_SCHEDULED_RUN_POLL_INTERVAL", time.Second, "Interval between reserving due scheduled runs. Must be positive; occurrences more than 30 seconds late are skipped.", ComponentController)
	ScheduledRunExecutionPollInterval = RegisterDurationVar("KAGENT_SCHEDULED_RUN_EXECUTION_POLL_INTERVAL", time.Second, "Interval between scheduled execution reconciliation attempts. Must be positive; longer intervals delay dispatch, status updates, deadline enforcement, and cleanup.", ComponentController)
	RuntimeRevisionGCInterval         = RegisterDurationVar("KAGENT_RUNTIME_REVISION_GC_INTERVAL", time.Minute, "Interval between unreferenced runtime revision cleanup sweeps. Must be positive.", ComponentController)
)
