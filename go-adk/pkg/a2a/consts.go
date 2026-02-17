package a2a

import "time"

// Timeout constants
const (
	DefaultExecutionTimeout = 30 * time.Minute
)

// Session state keys
const (
	StateKeySessionName = "session_name"
)

// A2A Data Part Metadata Constants
const (
	A2ADataPartMetadataTypeKey                 = "type"
	A2ADataPartMetadataIsLongRunningKey        = "is_long_running"
	A2ADataPartMetadataTypeFunctionCall        = "function_call"
	A2ADataPartMetadataTypeFunctionResponse    = "function_response"
	A2ADataPartMetadataTypeCodeExecutionResult = "code_execution_result"
	A2ADataPartMetadataTypeExecutableCode      = "executable_code"
)
