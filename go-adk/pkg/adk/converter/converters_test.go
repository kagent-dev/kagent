package converter

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/event"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	gogenai "google.golang.org/genai"
)

func testCC(taskID, contextID string) a2a.ConversionContext {
	return a2a.ConversionContext{
		TaskID: taskID, ContextID: contextID,
		AppName: "test_app", UserID: "test_user", SessionID: "test_session",
	}
}

func TestConvertEventToA2AEvents_StopWithEmptyContent(t *testing.T) {
	event1 := &event.RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result1 := ConvertEventToA2AEvents(event1, testCC("test_task_1", "test_context_1"))

	var errorEvents, workingEvents int
	for _, e := range result1 {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case a2atype.TaskStateFailed:
				errorEvents++
			case a2atype.TaskStateWorking:
				workingEvents++
			}
		}
	}

	if errorEvents != 0 {
		t.Errorf("Expected no error events for STOP with empty content, got %d", errorEvents)
	}
	if workingEvents != 0 {
		t.Errorf("Expected no working events for STOP with empty content (no content to convert), got %d", workingEvents)
	}
}

func TestConvertEventToA2AEvents_StopWithEmptyParts(t *testing.T) {
	event2 := &event.RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result2 := ConvertEventToA2AEvents(event2, testCC("test_task_2", "test_context_2"))

	var errorEvents, workingEvents int
	for _, e := range result2 {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case a2atype.TaskStateFailed:
				errorEvents++
			case a2atype.TaskStateWorking:
				workingEvents++
			}
		}
	}

	if errorEvents != 0 {
		t.Errorf("Expected no error events for STOP with empty parts, got %d", errorEvents)
	}
	if workingEvents != 0 {
		t.Errorf("Expected no working events for STOP with empty parts (no content to convert), got %d", workingEvents)
	}
}

func TestConvertEventToA2AEvents_StopWithMissingContent(t *testing.T) {
	event3 := &event.RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result3 := ConvertEventToA2AEvents(event3, testCC("test_task_3", "test_context_3"))

	var errorEvents, workingEvents int
	for _, e := range result3 {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case a2atype.TaskStateFailed:
				errorEvents++
			case a2atype.TaskStateWorking:
				workingEvents++
			}
		}
	}
	if errorEvents != 0 {
		t.Errorf("Expected no error events for STOP with missing content, got %d", errorEvents)
	}
	if workingEvents != 0 {
		t.Errorf("Expected no working events for STOP with missing content (no content to convert), got %d", workingEvents)
	}
}

func TestConvertEventToA2AEvents_ActualErrorCode(t *testing.T) {
	event4 := &event.RunnerErrorEvent{
		ErrorCode: genai.FinishReasonMalformedFunctionCall,
	}

	result4 := ConvertEventToA2AEvents(event4, testCC("test_task_4", "test_context_4"))

	var errorEvents []*a2atype.TaskStatusUpdateEvent
	for _, e := range result4 {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == a2atype.TaskStateFailed {
				errorEvents = append(errorEvents, statusUpdate)
			}
		}
	}

	if len(errorEvents) != 1 {
		t.Fatalf("Expected 1 error event for MALFORMED_FUNCTION_CALL, got %d", len(errorEvents))
	}

	errorEvent := errorEvents[0]
	errorCodeKey := a2a.GetKAgentMetadataKey("error_code")
	if errorCode, ok := errorEvent.Metadata[errorCodeKey].(string); !ok {
		t.Errorf("Expected error_code in metadata, got %v", errorEvent.Metadata[errorCodeKey])
	} else if errorCode != genai.FinishReasonMalformedFunctionCall {
		t.Errorf("Expected error_code = %q, got %q", genai.FinishReasonMalformedFunctionCall, errorCode)
	}
}

func TestConvertEventToA2AEvents_ErrorCodeWithErrorMessage(t *testing.T) {
	evt := &event.RunnerErrorEvent{
		ErrorCode:    genai.FinishReasonMaxTokens,
		ErrorMessage: "Custom error message",
	}

	result := ConvertEventToA2AEvents(evt, testCC("test_task", "test_context"))

	var errorEvents []*a2atype.TaskStatusUpdateEvent
	for _, e := range result {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == a2atype.TaskStateFailed {
				errorEvents = append(errorEvents, statusUpdate)
			}
		}
	}

	if len(errorEvents) != 1 {
		t.Fatalf("Expected 1 error event, got %d", len(errorEvents))
	}

	errorEvent := errorEvents[0]
	if errorEvent.Status.Message == nil || len(errorEvent.Status.Message.Parts) == 0 {
		t.Fatal("Expected error event to have message with parts")
	}

	var textPart a2atype.TextPart
	if tp, ok := errorEvent.Status.Message.Parts[0].(a2atype.TextPart); ok {
		textPart = tp
	} else {
		t.Fatalf("Expected TextPart, got %T", errorEvent.Status.Message.Parts[0])
	}

	if textPart.Text != "Custom error message" {
		t.Errorf("Expected custom error message, got %q", textPart.Text)
	}
}

func TestConvertEventToA2AEvents_ErrorCodeWithoutErrorMessage(t *testing.T) {
	evt := &event.RunnerErrorEvent{
		ErrorCode:    genai.FinishReasonMaxTokens,
		ErrorMessage: "",
	}

	result := ConvertEventToA2AEvents(evt, testCC("test_task", "test_context"))

	var errorEvents []*a2atype.TaskStatusUpdateEvent
	for _, e := range result {
		if statusUpdate, ok := e.(*a2atype.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == a2atype.TaskStateFailed {
				errorEvents = append(errorEvents, statusUpdate)
			}
		}
	}

	if len(errorEvents) != 1 {
		t.Fatalf("Expected 1 error event, got %d", len(errorEvents))
	}

	errorEvent := errorEvents[0]
	if errorEvent.Status.Message == nil || len(errorEvent.Status.Message.Parts) == 0 {
		t.Fatal("Expected error event to have message with parts")
	}

	var textPart a2atype.TextPart
	if tp, ok := errorEvent.Status.Message.Parts[0].(a2atype.TextPart); ok {
		textPart = tp
	} else {
		t.Fatalf("Expected TextPart, got %T", errorEvent.Status.Message.Parts[0])
	}

	expectedMessage := genai.GetErrorMessage(genai.FinishReasonMaxTokens)
	if textPart.Text != expectedMessage {
		t.Errorf("Expected error message from GetErrorMessage, got %q, want %q", textPart.Text, expectedMessage)
	}
}

func TestConvertEventToA2AEvents_UserResponseAndQuestions(t *testing.T) {
	t.Run("long_running_function_call_sets_input_required", func(t *testing.T) {
		e := &adksession.Event{
			LLMResponse: model.LLMResponse{
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
		result := ConvertEventToA2AEvents(e, testCC("task1", "ctx1"))
		var statusEvent *a2atype.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateInputRequired {
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
			LLMResponse: model.LLMResponse{
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
		result := ConvertEventToA2AEvents(e, testCC("task2", "ctx2"))
		var statusEvent *a2atype.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateAuthRequired {
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
			LLMResponse: model.LLMResponse{
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
		result := ConvertEventToA2AEvents(e, testCC("task3", "ctx3"))
		var statusEvent *a2atype.TaskStatusUpdateEvent
		for _, ev := range result {
			if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok {
				statusEvent = se
				break
			}
		}
		if statusEvent == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent")
		}
		if statusEvent.Status.State != a2atype.TaskStateWorking {
			t.Errorf("Expected state working when not long-running, got %v", statusEvent.Status.State)
		}
	})
}
