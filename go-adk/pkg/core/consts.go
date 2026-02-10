package core

import "time"

// Well-known metadata key suffixes (used with GetKAgentMetadataKey).
const (
	MetadataKeyUserID    = "user_id"
	MetadataKeySessionID = "session_id"
)

// Channel and buffer sizes
const (
	// EventChannelBufferSize is the buffer size for event channels.
	// Sized to handle bursts of events without blocking the producer.
	EventChannelBufferSize = 10

	// JSONPreviewMaxLength is the maximum length for JSON previews in logs.
	JSONPreviewMaxLength = 500

	// SchemaJSONMaxLength is the maximum length for schema JSON in logs.
	SchemaJSONMaxLength = 2000

	// ResponseBodyMaxLength is the maximum length for response body in logs.
	ResponseBodyMaxLength = 2000
)

// Timeout constants
const (
	// EventPersistTimeout is the timeout for persisting events to the backend.
	EventPersistTimeout = 30 * time.Second

	// MCPInitTimeout is the default timeout for MCP toolset initialization.
	MCPInitTimeout = 2 * time.Minute

	// MCPInitTimeoutMax is the maximum timeout for MCP initialization.
	MCPInitTimeoutMax = 5 * time.Minute

	// MinTimeout is the minimum timeout for any operation.
	MinTimeout = 1 * time.Second
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
	ArgKeyAppName        = "app_name"
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
