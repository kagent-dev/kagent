package env

import "time"

var (
	TaskStoreOperationBudget = RegisterDurationVar("KAGENT_TASKSTORE_OPERATION_BUDGET", 90*time.Second,
		"Longest a Go runtime retries one TaskStore call, waits included, while the controller is unreachable. An earlier caller deadline still applies. Set it in a Harness's spec.env; malformed and non-positive values keep the default.",
		ComponentAgentRuntime)
	TaskStoreAttemptTimeout = RegisterDurationVar("KAGENT_TASKSTORE_ATTEMPT_TIMEOUT", 5*time.Second,
		"Longest one TaskStore attempt may take. What remains of KAGENT_TASKSTORE_OPERATION_BUDGET and the controller client's 30s call timeout also cap it, and a call slower than this fails even when the controller is healthy. Set it in a Harness's spec.env; malformed and non-positive values keep the default.",
		ComponentAgentRuntime)
)
