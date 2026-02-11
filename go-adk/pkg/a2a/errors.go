package a2a

import (
	adksession "google.golang.org/adk/session"
)

// ADKEventHasToolContent returns true if the ADK event has Content.Parts with FunctionCall or FunctionResponse.
func ADKEventHasToolContent(e *adksession.Event) bool {
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
