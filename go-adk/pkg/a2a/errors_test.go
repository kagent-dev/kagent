package a2a

import (
	"testing"

	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestADKEventHasToolContent_FunctionCall(t *testing.T) {
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
	if !ADKEventHasToolContent(e) {
		t.Error("ADKEventHasToolContent should be true for *adksession.Event with FunctionCall part")
	}
}

func TestADKEventHasToolContent_FunctionResponse(t *testing.T) {
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
	if !ADKEventHasToolContent(e) {
		t.Error("ADKEventHasToolContent should be true for *adksession.Event with FunctionResponse part")
	}
}

func TestADKEventHasToolContent_NoToolContent(t *testing.T) {
	e := &adksession.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: "Hello"}},
			},
			Partial: true,
		},
	}
	if ADKEventHasToolContent(e) {
		t.Error("ADKEventHasToolContent should be false for *adksession.Event with only text part")
	}
}

func TestADKEventHasToolContent_NilContent(t *testing.T) {
	e := &adksession.Event{}
	if ADKEventHasToolContent(e) {
		t.Error("ADKEventHasToolContent should be false for *adksession.Event with nil Content")
	}
}

func TestADKEventHasToolContent_NilEvent(t *testing.T) {
	if ADKEventHasToolContent(nil) {
		t.Error("ADKEventHasToolContent should be false for nil event")
	}
}
