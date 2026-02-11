package a2a

import (
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	gogenai "google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestConvertADKEventToA2AEvents_WithTextContent(t *testing.T) {
	adkEvent := &adksession.Event{
		Author: "agent",
		LLMResponse: adkmodel.LLMResponse{
			Content: &gogenai.Content{
				Parts: []*gogenai.Part{
					{Text: "Hello from agent"},
				},
			},
		},
	}

	result := ConvertADKEventToA2AEvents(adkEvent, "task1", "ctx1", "app", "user", "session")
	if len(result) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(result))
	}

	statusEvent, ok := result[0].(*protocol.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", result[0])
	}
	if statusEvent.Status.State != protocol.TaskStateWorking {
		t.Errorf("Expected state working, got %v", statusEvent.Status.State)
	}
	if statusEvent.Status.Message == nil || len(statusEvent.Status.Message.Parts) == 0 {
		t.Fatal("Expected message with parts")
	}
}

func TestConvertADKEventToA2AEvents_EmptyContent(t *testing.T) {
	adkEvent := &adksession.Event{
		Author:      "agent",
		LLMResponse: adkmodel.LLMResponse{},
	}

	result := ConvertADKEventToA2AEvents(adkEvent, "task1", "ctx1", "app", "user", "session")
	if len(result) != 0 {
		t.Errorf("Expected 0 events for empty content, got %d", len(result))
	}
}

func TestCreateErrorA2AEvent_Basic(t *testing.T) {
	event := CreateErrorA2AEvent(
		model.FinishReasonMalformedFunctionCall,
		"Custom error message",
		"test_task",
		"test_context",
		"test_app",
		"test_user",
		"test_session",
	)

	if event == nil {
		t.Fatal("Expected non-nil event")
	}
	if event.Status.State != protocol.TaskStateFailed {
		t.Errorf("Expected state failed, got %v", event.Status.State)
	}

	errorCodeKey := GetKAgentMetadataKey("error_code")
	if errorCode, ok := event.Metadata[errorCodeKey].(string); !ok {
		t.Errorf("Expected error_code in metadata, got %v", event.Metadata[errorCodeKey])
	} else if errorCode != model.FinishReasonMalformedFunctionCall {
		t.Errorf("Expected error_code = %q, got %q", model.FinishReasonMalformedFunctionCall, errorCode)
	}

	if event.Status.Message == nil || len(event.Status.Message.Parts) == 0 {
		t.Fatal("Expected error event to have message with parts")
	}
	switch tp := event.Status.Message.Parts[0].(type) {
	case *protocol.TextPart:
		if tp.Text != "Custom error message" {
			t.Errorf("Expected custom error message, got %q", tp.Text)
		}
	case protocol.TextPart:
		if tp.Text != "Custom error message" {
			t.Errorf("Expected custom error message, got %q", tp.Text)
		}
	default:
		t.Fatalf("Expected TextPart, got %T", event.Status.Message.Parts[0])
	}
}

func TestCreateErrorA2AEvent_WithoutMessage(t *testing.T) {
	event := CreateErrorA2AEvent(
		model.FinishReasonMaxTokens,
		"",
		"test_task",
		"test_context",
		"test_app",
		"test_user",
		"test_session",
	)

	if event == nil {
		t.Fatal("Expected non-nil event")
	}
	if event.Status.Message == nil || len(event.Status.Message.Parts) == 0 {
		t.Fatal("Expected error event to have message with parts")
	}

	expectedMessage := model.GetErrorMessage(model.FinishReasonMaxTokens)
	switch tp := event.Status.Message.Parts[0].(type) {
	case *protocol.TextPart:
		if tp.Text != expectedMessage {
			t.Errorf("Expected error message from GetErrorMessage, got %q, want %q", tp.Text, expectedMessage)
		}
	case protocol.TextPart:
		if tp.Text != expectedMessage {
			t.Errorf("Expected error message from GetErrorMessage, got %q, want %q", tp.Text, expectedMessage)
		}
	default:
		t.Fatalf("Expected TextPart, got %T", event.Status.Message.Parts[0])
	}
}

func TestConvertADKEventToA2AEvents_UserResponseAndQuestions(t *testing.T) {
	t.Run("long_running_function_call_sets_input_required", func(t *testing.T) {
		e := &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &gogenai.Content{
					Parts: []*gogenai.Part{{
						FunctionCall: &gogenai.FunctionCall{
							Name: "get_weather",
							Args: map[string]any{"city": "NYC"},
							ID:   "fc1",
						},
					}},
				},
			},
			LongRunningToolIDs: []string{"fc1"},
		}
		result := ConvertADKEventToA2AEvents(e, "task1", "ctx1", "app", "user", "session")
		var statusEvent *protocol.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*protocol.TaskStatusUpdateEvent); ok && se.Status.State == protocol.TaskStateInputRequired {
				statusEvent = se
				break
			}
		}
		if statusEvent == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent with state input_required")
		}
	})

	t.Run("long_running_request_euc_sets_auth_required", func(t *testing.T) {
		e := &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &gogenai.Content{
					Parts: []*gogenai.Part{{
						FunctionCall: &gogenai.FunctionCall{
							Name: "request_euc",
							Args: map[string]any{},
							ID:   "fc_euc",
						},
					}},
				},
			},
			LongRunningToolIDs: []string{"fc_euc"},
		}
		result := ConvertADKEventToA2AEvents(e, "task2", "ctx2", "app", "user", "session")
		var statusEvent *protocol.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*protocol.TaskStatusUpdateEvent); ok && se.Status.State == protocol.TaskStateAuthRequired {
				statusEvent = se
				break
			}
		}
		if statusEvent == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent with state auth_required")
		}
	})

	t.Run("no_long_running_keeps_working", func(t *testing.T) {
		e := &adksession.Event{
			LLMResponse: adkmodel.LLMResponse{
				Content: &gogenai.Content{
					Parts: []*gogenai.Part{{
						FunctionCall: &gogenai.FunctionCall{
							Name: "get_weather",
							Args: map[string]any{"city": "NYC"},
							ID:   "fc2",
						},
					}},
				},
			},
			LongRunningToolIDs: nil,
		}
		result := ConvertADKEventToA2AEvents(e, "task3", "ctx3", "app", "user", "session")
		var statusEvent *protocol.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*protocol.TaskStatusUpdateEvent); ok {
				statusEvent = se
				break
			}
		}
		if statusEvent == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent")
		}
		if statusEvent.Status.State != protocol.TaskStateWorking {
			t.Errorf("Expected state working when not long-running, got %v", statusEvent.Status.State)
		}
	})
}
