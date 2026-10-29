package env

import "time"

var (
	TaskStoreOperationBudget = RegisterDurationVar("KAGENT_TASKSTORE_OPERATION_BUDGET", 90*time.Second,
		"Longest a Go runtime retries one TaskStore call, waits included, while the controller is unreachable. An earlier caller deadline still applies. Set it in a Harness's spec.env; malformed and non-positive values keep the default.",
		ComponentAgentRuntime)
	TaskStoreAttemptTimeout = RegisterDurationVar("KAGENT_TASKSTORE_ATTEMPT_TIMEOUT", 30*time.Second,
		"Longest one TaskStore attempt may take, within what remains of KAGENT_TASKSTORE_OPERATION_BUDGET. The default matches the controller client's 30s call timeout, which also caps any longer value. A lower value notices a hung connection sooner, but fails a healthy call that takes longer. Set it in a Harness's spec.env; malformed and non-positive values keep the default.",
		ComponentAgentRuntime)
)
