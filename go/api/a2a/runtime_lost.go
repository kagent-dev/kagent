package a2a

const (
	// RuntimeLostMessagePrefix opens the failure message of a Session whose
	// runtime is gone for good: its Actor crashed or no longer exists. The
	// text after the prefix names the cause. A client that reads it knows the
	// conversation cannot continue, as opposed to a turn that may be retried.
	RuntimeLostMessagePrefix = "runtime lost: "

	// FailureReasonRuntimeLost is the Session failure reason recorded with a
	// message that opens with RuntimeLostMessagePrefix.
	FailureReasonRuntimeLost = "RuntimeLost"
)
