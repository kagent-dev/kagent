package converter

import (
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// EventConverter implements a2a.EventConverter by delegating to package-level functions.
// It is stateless; exists only to satisfy the interface.
type EventConverter struct{}

// NewEventConverter returns an EventConverter that implements a2a.EventConverter.
func NewEventConverter() *EventConverter {
	return &EventConverter{}
}

// ConvertEventToA2AEvents delegates to the package-level ConvertEventToA2AEvents.
func (c *EventConverter) ConvertEventToA2AEvents(evt interface{}, cc a2a.ConversionContext) []protocol.Event {
	return ConvertEventToA2AEvents(evt, cc)
}

// IsPartialEvent delegates to the package-level IsPartialEvent.
func (c *EventConverter) IsPartialEvent(evt interface{}) bool {
	return IsPartialEvent(evt)
}
