package core

// Well-known metadata key suffixes (used with GetKAgentMetadataKey).
const (
	MetadataKeyUserID   = "user_id"
	MetadataKeySessionID = "session_id"
)

// HTTP header names and values.
const (
	HeaderContentType = "Content-Type"
	HeaderXUserID    = "X-User-ID"
	ContentTypeJSON  = "application/json"
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
