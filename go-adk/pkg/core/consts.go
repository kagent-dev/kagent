package core

// Well-known metadata key suffixes (used with GetKAgentMetadataKey).
const (
	MetadataKeyUserID    = "user_id"
	MetadataKeySessionID = "session_id"
)

// Well-known keys for runner/executor args map (Run(ctx, args) and ConvertA2ARequestToRunArgs).
const (
	ArgKeyMessage        = "message"
	ArgKeyNewMessage     = "new_message"
	ArgKeyUserID         = "user_id"
	ArgKeySessionID      = "session_id"
	ArgKeySessionService = "session_service"
	ArgKeySession        = "session"
	ArgKeyRunConfig      = "run_config"
)

// Session state keys (e.g. state passed to CreateSession).
const (
	StateKeySessionName = "session_name"
)

// RunConfig keys (value of args[ArgKeyRunConfig] is map[string]interface{}).
const (
	RunConfigKeyStreamingMode = "streaming_mode"
)

// Session/API request body keys (e.g. session create payload).
const (
	SessionRequestKeyAgentRef = "agent_ref"
)

// HTTP header names and values.
const (
	HeaderContentType = "Content-Type"
	HeaderXUserID     = "X-User-ID"
	ContentTypeJSON   = "application/json"
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
