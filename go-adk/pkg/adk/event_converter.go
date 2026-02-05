package adk

import (
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// EventConverter implements core.EventConverter using ADK event types
// (*adksession.Event, RunnerErrorEvent) and existing conversion helpers.
type EventConverter struct{}

// NewEventConverter returns an EventConverter that implements core.EventConverter.
func NewEventConverter() *EventConverter {
	return &EventConverter{}
}

// ConvertEventToA2AEvents delegates to the package-level ConvertEventToA2AEvents.
func (c *EventConverter) ConvertEventToA2AEvents(event interface{}, taskID, contextID, appName, userID, sessionID string) []protocol.Event {
	return ConvertEventToA2AEvents(event, taskID, contextID, appName, userID, sessionID)
}

// IsPartialEvent delegates to the package-level IsPartialEvent.
func (c *EventConverter) IsPartialEvent(event interface{}) bool {
	return IsPartialEvent(event)
}

// EventHasToolContent delegates to the package-level EventHasToolContent.
func (c *EventConverter) EventHasToolContent(event interface{}) bool {
	return EventHasToolContent(event)
}
