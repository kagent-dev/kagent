package a2a

import (
	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// ConversionContext holds the contextual identifiers needed for event conversion.
type ConversionContext struct {
	TaskID    string
	ContextID string
	AppName   string
	UserID    string
	SessionID string
}

// ConvertEventsFunc converts runner events (e.g. *adksession.Event, RunnerErrorEvent) to A2A events.
type ConvertEventsFunc func(event interface{}, cc ConversionContext) []a2atype.Event

// IsPartialFunc reports whether a runner event is a partial/streaming event.
type IsPartialFunc func(event interface{}) bool

// ConvertA2ARequestToRunArgs converts an A2A request to internal agent run arguments.
// The *a2atype.Message is passed through as-is; conversion to genai.Content
// happens in the runner via converter.A2AMessageToGenAIContent.
func ConvertA2ARequestToRunArgs(req *a2atype.MessageSendParams, userID, sessionID string) map[string]interface{} {
	args := map[string]interface{}{
		ArgKeyUserID:    userID,
		ArgKeySessionID: sessionID,
		ArgKeyRunConfig: map[string]interface{}{
			RunConfigKeyStreamingMode: "NONE", // Default, overridden by executor config
		},
	}
	if req != nil && req.Message != nil {
		args[ArgKeyMessage] = req.Message
	}
	return args
}
