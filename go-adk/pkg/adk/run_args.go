package adk

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

// RunConfig keys (value of args[ArgKeyRunConfig] is map[string]interface{}).
const (
	RunConfigKeyStreamingMode = "streaming_mode"
)

// Session/API request body keys (e.g. session create payload).
const (
	SessionRequestKeyAgentRef = "agent_ref"
)
