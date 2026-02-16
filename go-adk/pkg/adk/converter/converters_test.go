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

func TestConvertEventToA2AEvents_StopWithNoContent(t *testing.T) {
	tests := []struct {
		name      string
		taskID    string
		contextID string
	}{
		{"empty_content", "test_task_1", "test_context_1"},
		{"empty_parts", "test_task_2", "test_context_2"},
		{"missing_content", "test_task_3", "test_context_3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := &event.RunnerErrorEvent{
				ErrorCode: genai.FinishReasonStop,
			}
			result := ConvertEventToA2AEvents(evt, testCC(tt.taskID, tt.contextID))

			var errorEvents, workingEvents int
			for _, e := range result {
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
				t.Errorf("Expected no error events for STOP with no content, got %d", errorEvents)
			}
			if workingEvents != 0 {
				t.Errorf("Expected no working events for STOP with no content, got %d", workingEvents)
			}
		})
	}
}

// extractErrorEvents filters TaskStatusUpdateEvents with TaskStateFailed from a slice of events.
func extractErrorEvents(events []a2atype.Event) []*a2atype.TaskStatusUpdateEvent {
	var out []*a2atype.TaskStatusUpdateEvent
	for _, e := range events {
		if su, ok := e.(*a2atype.TaskStatusUpdateEvent); ok && su.Status.State == a2atype.TaskStateFailed {
			out = append(out, su)
		}
	}
	return out
}

// findStatusEventByState returns the first TaskStatusUpdateEvent matching the given state.
func findStatusEventByState(events []a2atype.Event, state a2atype.TaskState) *a2atype.TaskStatusUpdateEvent {
	for _, e := range events {
		if su, ok := e.(*a2atype.TaskStatusUpdateEvent); ok && su.Status.State == state {
			return su
		}
	}
	return nil
}

// requireErrorEventText extracts a single error event and returns its text message.
func requireErrorEventText(t *testing.T, events []a2atype.Event) string {
	t.Helper()
	errorEvents := extractErrorEvents(events)
	if len(errorEvents) != 1 {
		t.Fatalf("Expected 1 error event, got %d", len(errorEvents))
	}
	ee := errorEvents[0]
	if ee.Status.Message == nil || len(ee.Status.Message.Parts) == 0 {
		t.Fatal("Expected error event to have message with parts")
	}
	tp, ok := ee.Status.Message.Parts[0].(a2atype.TextPart)
	if !ok {
		t.Fatalf("Expected TextPart, got %T", ee.Status.Message.Parts[0])
	}
	return tp.Text
}

func TestConvertEventToA2AEvents_ErrorCodes(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    string
		errorMessage string
		wantText     string
		wantMetaCode string
	}{
		{
			name:         "actual_error_code_malformed",
			errorCode:    genai.FinishReasonMalformedFunctionCall,
			wantMetaCode: genai.FinishReasonMalformedFunctionCall,
		},
		{
			name:         "error_code_with_custom_message",
			errorCode:    genai.FinishReasonMaxTokens,
			errorMessage: "Custom error message",
			wantText:     "Custom error message",
		},
		{
			name:      "error_code_without_message_uses_default",
			errorCode: genai.FinishReasonMaxTokens,
			wantText:  genai.GetErrorMessage(genai.FinishReasonMaxTokens),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := &event.RunnerErrorEvent{
				ErrorCode:    tt.errorCode,
				ErrorMessage: tt.errorMessage,
			}
			result := ConvertEventToA2AEvents(evt, testCC("test_task", "test_context"))

			errorEvents := extractErrorEvents(result)
			if len(errorEvents) != 1 {
				t.Fatalf("Expected 1 error event, got %d", len(errorEvents))
			}
			ee := errorEvents[0]

			if tt.wantMetaCode != "" {
				if code, ok := ee.Metadata[a2a.MetadataKeyErrorCode].(string); !ok || code != tt.wantMetaCode {
					t.Errorf("Expected metadata error_code = %q, got %v", tt.wantMetaCode, ee.Metadata[a2a.MetadataKeyErrorCode])
				}
			}

			if tt.wantText != "" {
				text := requireErrorEventText(t, result)
				if text != tt.wantText {
					t.Errorf("Expected text %q, got %q", tt.wantText, text)
				}
			}
		})
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
		if se := findStatusEventByState(result, a2atype.TaskStateInputRequired); se == nil {
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
		if se := findStatusEventByState(result, a2atype.TaskStateAuthRequired); se == nil {
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
		if se := findStatusEventByState(result, a2atype.TaskStateWorking); se == nil {
			t.Fatal("Expected one TaskStatusUpdateEvent with state working")
		}
	})
}

func TestIsPartialEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    interface{}
		expected bool
	}{
		{
			name:     "adk_event_partial",
			event:    &adksession.Event{LLMResponse: model.LLMResponse{Partial: true}},
			expected: true,
		},
		{
			name:     "adk_event_not_partial",
			event:    &adksession.Event{},
			expected: false,
		},
		{
			name:     "runner_error_event",
			event:    &event.RunnerErrorEvent{ErrorCode: "ERR"},
			expected: false,
		},
		{
			name:     "nil_event",
			event:    nil,
			expected: false,
		},
		{
			name:     "string_event",
			event:    "not an event",
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPartialEvent(tt.event)
			if result != tt.expected {
				t.Errorf("IsPartialEvent() = %v, want %v", result, tt.expected)
			}
		})
	}
}
