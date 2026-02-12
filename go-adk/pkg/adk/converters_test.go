package adk

import (
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/model"
	gogenai "google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestConvertEventToA2AEvents_StopWithEmptyContent(t *testing.T) {
	// STOP with no content: RunnerErrorEvent (or any non-ADK) with ErrorCode STOP → no events
	event1 := &RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result1 := ConvertEventToA2AEvents(
		event1,
		"test_task_1",
		"test_context_1",
		"test_app",
		"test_user",
		"test_session",
	)

	// Count error events and working events
	var errorEvents, workingEvents int
	for _, e := range result1 {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case protocol.TaskStateFailed:
				errorEvents++
			case protocol.TaskStateWorking:
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
	// STOP, no content to convert (non-ADK) → no events
	event2 := &RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result2 := ConvertEventToA2AEvents(
		event2,
		"test_task_2",
		"test_context_2",
		"test_app",
		"test_user",
		"test_session",
	)

	var errorEvents, workingEvents int
	for _, e := range result2 {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case protocol.TaskStateFailed:
				errorEvents++
			case protocol.TaskStateWorking:
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
	// STOP, no content → no events
	event3 := &RunnerErrorEvent{
		ErrorCode: genai.FinishReasonStop,
	}

	result3 := ConvertEventToA2AEvents(
		event3,
		"test_task_3",
		"test_context_3",
		"test_app",
		"test_user",
		"test_session",
	)

	var errorEvents, workingEvents int
	for _, e := range result3 {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			switch statusUpdate.Status.State {
			case protocol.TaskStateFailed:
				errorEvents++
			case protocol.TaskStateWorking:
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
	// RunnerErrorEvent with actual error code → one failed status event
	event4 := &RunnerErrorEvent{
		ErrorCode: genai.FinishReasonMalformedFunctionCall,
	}

	result4 := ConvertEventToA2AEvents(
		event4,
		"test_task_4",
		"test_context_4",
		"test_app",
		"test_user",
		"test_session",
	)

	var errorEvents []*protocol.TaskStatusUpdateEvent
	for _, e := range result4 {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == protocol.TaskStateFailed {
				errorEvents = append(errorEvents, statusUpdate)
			}
		}
	}

	if len(errorEvents) != 1 {
		t.Fatalf("Expected 1 error event for MALFORMED_FUNCTION_CALL, got %d", len(errorEvents))
	}

	// Check that the error event has the correct error code in metadata
	errorEvent := errorEvents[0]
	errorCodeKey := core.GetKAgentMetadataKey("error_code")
	if errorCode, ok := errorEvent.Metadata[errorCodeKey].(string); !ok {
		t.Errorf("Expected error_code in metadata, got %v", errorEvent.Metadata[errorCodeKey])
	} else if errorCode != genai.FinishReasonMalformedFunctionCall {
		t.Errorf("Expected error_code = %q, got %q", genai.FinishReasonMalformedFunctionCall, errorCode)
	}
}

func TestConvertEventToA2AEvents_ErrorCodeWithErrorMessage(t *testing.T) {
	// RunnerErrorEvent with message → used in status event
	event := &RunnerErrorEvent{
		ErrorCode:    genai.FinishReasonMaxTokens,
		ErrorMessage: "Custom error message",
	}

	result := ConvertEventToA2AEvents(
		event,
		"test_task",
		"test_context",
		"test_app",
		"test_user",
		"test_session",
	)

	var errorEvents []*protocol.TaskStatusUpdateEvent
	for _, e := range result {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == protocol.TaskStateFailed {
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

	// Handle both pointer and value types
	var textPart *protocol.TextPart
	if tp, ok := errorEvent.Status.Message.Parts[0].(*protocol.TextPart); ok {
		textPart = tp
	} else if tp, ok := errorEvent.Status.Message.Parts[0].(protocol.TextPart); ok {
		textPart = &tp
	} else {
		t.Fatalf("Expected TextPart, got %T", errorEvent.Status.Message.Parts[0])
	}

	if textPart.Text != "Custom error message" {
		t.Errorf("Expected custom error message, got %q", textPart.Text)
	}
}

func TestConvertEventToA2AEvents_ErrorCodeWithoutErrorMessage(t *testing.T) {
	// RunnerErrorEvent without message → GetErrorMessage used
	event := &RunnerErrorEvent{
		ErrorCode:    genai.FinishReasonMaxTokens,
		ErrorMessage: "",
	}

	result := ConvertEventToA2AEvents(
		event,
		"test_task",
		"test_context",
		"test_app",
		"test_user",
		"test_session",
	)

	var errorEvents []*protocol.TaskStatusUpdateEvent
	for _, e := range result {
		if statusUpdate, ok := e.(*protocol.TaskStatusUpdateEvent); ok {
			if statusUpdate.Status.State == protocol.TaskStateFailed {
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

	// Handle both pointer and value types
	var textPart *protocol.TextPart
	if tp, ok := errorEvent.Status.Message.Parts[0].(*protocol.TextPart); ok {
		textPart = tp
	} else if tp, ok := errorEvent.Status.Message.Parts[0].(protocol.TextPart); ok {
		textPart = &tp
	} else {
		t.Fatalf("Expected TextPart, got %T", errorEvent.Status.Message.Parts[0])
	}

	expectedMessage := genai.GetErrorMessage(genai.FinishReasonMaxTokens)
	if textPart.Text != expectedMessage {
		t.Errorf("Expected error message from GetErrorMessage, got %q, want %q", textPart.Text, expectedMessage)
	}
}

// TestConvertEventToA2AEvents_UserResponseAndQuestions verifies that user response/question
// states (input_required, auth_required) match Python kagent-adk _create_status_update_event.
func TestConvertEventToA2AEvents_UserResponseAndQuestions(t *testing.T) {
	t.Run("long_running_function_call_sets_input_required", func(t *testing.T) {
		// One long-running function call (not request_euc) → input_required (user approval/questions).
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
		result := ConvertEventToA2AEvents(e, "task1", "ctx1", "app", "user", "session")
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
		if statusEvent.Status.State != protocol.TaskStateInputRequired {
			t.Errorf("Expected state input_required, got %v", statusEvent.Status.State)
		}
	})

	t.Run("long_running_request_euc_sets_auth_required", func(t *testing.T) {
		// Long-running function call with name "request_euc" → auth_required (matches Python).
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
		result := ConvertEventToA2AEvents(e, "task2", "ctx2", "app", "user", "session")
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
		if statusEvent.Status.State != protocol.TaskStateAuthRequired {
			t.Errorf("Expected state auth_required, got %v", statusEvent.Status.State)
		}
	})

	t.Run("no_long_running_keeps_working", func(t *testing.T) {
		// Function call without long_running metadata → state stays working.
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
			LongRunningToolIDs: nil, // not long-running
		}
		result := ConvertEventToA2AEvents(e, "task3", "ctx3", "app", "user", "session")
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
