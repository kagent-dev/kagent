package adk

import (
	"reflect"

	adksession "google.golang.org/adk/session"
)

// Event is the internal agent event type used for runner output and session history.
// It is converted to/from Google ADK session events and to A2A protocol events.
type Event struct {
	ID           string
	Author       string
	Timestamp    string
	Content      *Content
	Partial      bool
	ErrorCode    string
	ErrorMessage string
}

// Content holds role and parts for an event (matches session and A2A conversion).
type Content struct {
	Role  string
	Parts []interface{}
}

// EventHasToolContent returns true if the event contains function_call or function_response parts.
// Used to decide whether to report partial events to the session (report partial when tool-related).
// The Google ADK runner only calls AppendEvent for non-partial events (runner.go:164), so we must
// append partial tool events ourselves so they reach the session and UI (matching Python behavior).
func EventHasToolContent(event interface{}) bool {
	if event == nil {
		return false
	}
	// Direct check for *adksession.Event (what the wrapper receives from runner.Run).
	// Avoids reflection so tool events are reliably detected and appended to session.
	if adkE, ok := event.(*adksession.Event); ok {
		return adkEventHasToolContent(adkE)
	}
	switch e := event.(type) {
	case *Event:
		return eventHasToolContentOur(e)
	case *Content:
		return contentHasToolParts(e)
	default:
		// Map-shaped or other event
		return eventHasToolContentReflect(event)
	}
}

// adkEventHasToolContent returns true if the ADK event has Content.Parts with FunctionCall or FunctionResponse.
// adksession.Event embeds model.LLMResponse (field name LLMResponse), so content is at e.LLMResponse.Content.
func adkEventHasToolContent(e *adksession.Event) bool {
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

// partHasToolContent returns true if the part is a map with function_call/function_response or has tool fields via reflection.
func partHasToolContent(part interface{}) bool {
	if part == nil {
		return false
	}
	if m, ok := part.(map[string]interface{}); ok {
		if _, hasFC := m[PartKeyFunctionCall]; hasFC {
			return true
		}
		if _, hasFR := m[PartKeyFunctionResponse]; hasFR {
			return true
		}
		return false
	}
	return partHasToolFieldsReflect(part)
}

func eventHasToolContentOur(e *Event) bool {
	if e == nil || e.Content == nil || len(e.Content.Parts) == 0 {
		return false
	}
	for _, p := range e.Content.Parts {
		if partHasToolContent(p) {
			return true
		}
	}
	return false
}

func contentHasToolParts(c *Content) bool {
	if c == nil || len(c.Parts) == 0 {
		return false
	}
	for _, p := range c.Parts {
		if partHasToolContent(p) {
			return true
		}
	}
	return false
}

// eventHasToolContentReflect checks event content for tool parts using reflection (e.g. *adksession.Event with *genai.Part).
func eventHasToolContentReflect(event interface{}) bool {
	content := extractContent(event)
	if content == nil {
		return false
	}
	for _, p := range extractContentParts(content) {
		if partHasToolContent(p) {
			return true
		}
	}
	return false
}

// partHasToolFieldsReflect returns true if the part (e.g. *genai.Part) has FunctionCall or FunctionResponse set.
func partHasToolFieldsReflect(part interface{}) bool {
	if part == nil {
		return false
	}
	v := reflect.ValueOf(part)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	for _, name := range []string{"FunctionCall", "FunctionResponse"} {
		f := v.FieldByName(name)
		if f.IsValid() && (f.Kind() == reflect.Ptr || f.Kind() == reflect.Interface) && !f.IsNil() {
			return true
		}
	}
	return false
}
