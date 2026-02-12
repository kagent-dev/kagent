package a2a

import (
	"testing"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	gogenai "google.golang.org/genai"
)

// mockTaskInfoProvider implements a2aschema.TaskInfoProvider for tests.
type mockTaskInfoProvider struct {
	taskID    a2aschema.TaskID
	contextID string
}

func (m *mockTaskInfoProvider) TaskInfo() a2aschema.TaskInfo {
	return a2aschema.TaskInfo{
		TaskID:    m.taskID,
		ContextID: m.contextID,
	}
}

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

	infoProvider := &mockTaskInfoProvider{taskID: "task1", contextID: "ctx1"}
	result := ConvertADKEventToA2AEvents(adkEvent, infoProvider, "app", "user", "session")
	if len(result) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(result))
	}

	statusEvent, ok := result[0].(*a2aschema.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", result[0])
	}
	if statusEvent.Status.State != a2aschema.TaskStateWorking {
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

	infoProvider := &mockTaskInfoProvider{taskID: "task1", contextID: "ctx1"}
	result := ConvertADKEventToA2AEvents(adkEvent, infoProvider, "app", "user", "session")
	if len(result) != 0 {
		t.Errorf("Expected 0 events for empty content, got %d", len(result))
	}
}

func TestCreateErrorA2AEvent_Basic(t *testing.T) {
	infoProvider := &mockTaskInfoProvider{taskID: "test_task", contextID: "test_context"}
	event := CreateErrorA2AEvent(
		model.FinishReasonMalformedFunctionCall,
		"Custom error message",
		infoProvider,
		"test_app",
		"test_user",
		"test_session",
	)

	if event == nil {
		t.Fatal("Expected non-nil event")
	}
	if event.Status.State != a2aschema.TaskStateFailed {
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
	tp, ok := event.Status.Message.Parts[0].(*a2aschema.TextPart)
	if !ok {
		t.Fatalf("Expected *TextPart, got %T", event.Status.Message.Parts[0])
	}
	if tp.Text != "Custom error message" {
		t.Errorf("Expected custom error message, got %q", tp.Text)
	}
}

func TestCreateErrorA2AEvent_WithoutMessage(t *testing.T) {
	infoProvider := &mockTaskInfoProvider{taskID: "test_task", contextID: "test_context"}
	event := CreateErrorA2AEvent(
		model.FinishReasonMaxTokens,
		"",
		infoProvider,
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
	tp, ok := event.Status.Message.Parts[0].(*a2aschema.TextPart)
	if !ok {
		t.Fatalf("Expected *TextPart, got %T", event.Status.Message.Parts[0])
	}
	if tp.Text != expectedMessage {
		t.Errorf("Expected error message from GetErrorMessage, got %q, want %q", tp.Text, expectedMessage)
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
		infoProvider := &mockTaskInfoProvider{taskID: "task1", contextID: "ctx1"}
		result := ConvertADKEventToA2AEvents(e, infoProvider, "app", "user", "session")
		var statusEvent *a2aschema.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2aschema.TaskStatusUpdateEvent); ok && se.Status.State == a2aschema.TaskStateInputRequired {
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
		infoProvider := &mockTaskInfoProvider{taskID: "task2", contextID: "ctx2"}
		result := ConvertADKEventToA2AEvents(e, infoProvider, "app", "user", "session")
		var statusEvent *a2aschema.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2aschema.TaskStatusUpdateEvent); ok && se.Status.State == a2aschema.TaskStateAuthRequired {
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
		infoProvider := &mockTaskInfoProvider{taskID: "task3", contextID: "ctx3"}
		result := ConvertADKEventToA2AEvents(e, infoProvider, "app", "user", "session")
		var statusEvent *a2aschema.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2aschema.TaskStatusUpdateEvent); ok {
				statusEvent = se
				break
			}
		}
		if statusEvent == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent")
		}
		if statusEvent.Status.State != a2aschema.TaskStateWorking {
			t.Errorf("Expected state working when not long-running, got %v", statusEvent.Status.State)
		}
	})
}
