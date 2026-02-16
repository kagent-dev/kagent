package event

import (
	adksession "google.golang.org/adk/session"
)

// ErrorEventProvider is an interface for events that carry error information.
// This reduces the need for reflection when extracting error details from events.
type ErrorEventProvider interface {
	GetErrorCode() string
	GetErrorMessage() string
}

// RunnerErrorEvent is the only internal event type: it carries runner errors to A2A.
// Success events are always *adksession.Event (Google ADK). We use only A2A and Google ADK types otherwise.
type RunnerErrorEvent struct {
	ErrorCode    string
	ErrorMessage string
}

// GetErrorCode implements ErrorEventProvider
func (e *RunnerErrorEvent) GetErrorCode() string {
	return e.ErrorCode
}

// GetErrorMessage implements ErrorEventProvider
func (e *RunnerErrorEvent) GetErrorMessage() string {
	return e.ErrorMessage
}

// Compile-time interface compliance check
var _ ErrorEventProvider = (*RunnerErrorEvent)(nil)

// EventHasToolContent returns true if the ADK event has Content.Parts with FunctionCall or FunctionResponse.
// Used to decide whether to append partial tool events to session.
func EventHasToolContent(e *adksession.Event) bool {
	if e == nil {
		return false
	}
	content := e.LLMResponse.Content
	if content == nil || len(content.Parts) == 0 {
		return false
	}
	for _, p := range content.Parts {
		if p == nil {
			continue
		}
		if p.FunctionCall != nil || p.FunctionResponse != nil {
			return true
		}
	}
	return false
}
