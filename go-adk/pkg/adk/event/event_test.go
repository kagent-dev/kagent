package event

import (
	"testing"

	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestEventHasToolContent_ADKEvent_FunctionCall(t *testing.T) {
	// *adksession.Event with FunctionCall in Content.Parts should be detected as tool content
	// so partial tool events get appended to session (runner only appends non-partial).
	e := &adksession.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]any{"city": "NYC"}}},
				},
			},
			Partial: true,
		},
	}
	if !EventHasToolContent(e) {
		t.Error("EventHasToolContent should be true for *adksession.Event with FunctionCall part")
	}
}

func TestEventHasToolContent_ADKEvent_FunctionResponse(t *testing.T) {
	e := &adksession.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{
					{FunctionResponse: &genai.FunctionResponse{Name: "get_weather", Response: map[string]any{"temp": 72}}},
				},
			},
			Partial: true,
		},
	}
	if !EventHasToolContent(e) {
		t.Error("EventHasToolContent should be true for *adksession.Event with FunctionResponse part")
	}
}

func TestEventHasToolContent_ADKEvent_NoToolContent(t *testing.T) {
	e := &adksession.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: "Hello"}},
			},
			Partial: true,
		},
	}
	if EventHasToolContent(e) {
		t.Error("EventHasToolContent should be false for *adksession.Event with only text part")
	}
}

func TestEventHasToolContent_ADKEvent_NilContent(t *testing.T) {
	e := &adksession.Event{}
	if EventHasToolContent(e) {
		t.Error("EventHasToolContent should be false for *adksession.Event with nil Content")
	}
}
