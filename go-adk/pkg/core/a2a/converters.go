package a2a

import (
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// ConversionContext holds the contextual identifiers needed for event conversion.
type ConversionContext struct {
	TaskID    string
	ContextID string
	AppName   string
	UserID    string
	SessionID string
}

// EventConverter converts runner events to A2A events and reports event properties.
// Implementations typically wrap ADK-specific logic (e.g. *adksession.Event, RunnerErrorEvent).
type EventConverter interface {
	ConvertEventToA2AEvents(event interface{}, cc ConversionContext) []protocol.Event
	IsPartialEvent(event interface{}) bool
}

// ConvertA2ARequestToRunArgs converts an A2A request to internal agent run arguments.
// The *protocol.Message is passed through as-is; conversion to genai.Content
// happens in the runner via converter.A2AMessageToGenAIContent.
func ConvertA2ARequestToRunArgs(req *protocol.SendMessageParams, userID, sessionID string) map[string]interface{} {
	args := map[string]interface{}{
		ArgKeyUserID:    userID,
		ArgKeySessionID: sessionID,
		ArgKeyRunConfig: map[string]interface{}{
			RunConfigKeyStreamingMode: "NONE", // Default, overridden by executor config
		},
	}
	if req != nil {
		args[ArgKeyMessage] = &req.Message
	}
	return args
}
