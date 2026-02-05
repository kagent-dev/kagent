package core

import (
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// EventConverter converts runner events to A2A events and reports event properties.
// Implementations typically wrap ADK-specific logic (e.g. *adksession.Event, RunnerErrorEvent).
type EventConverter interface {
	ConvertEventToA2AEvents(event interface{}, taskID, contextID, appName, userID, sessionID string) []protocol.Event
	IsPartialEvent(event interface{}) bool
	EventHasToolContent(event interface{}) bool
}
