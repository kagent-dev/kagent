package converter

import (
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/event"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	adksession "google.golang.org/adk/session"
	gogenai "google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	// RequestEucFunctionCallName is the name of the request_euc function call
	requestEucFunctionCallName = "request_euc"
)

// extractErrorCode extracts error_code from an event using type switches.
func extractErrorCode(e interface{}) string {
	switch ev := e.(type) {
	case event.ErrorEventProvider:
		return ev.GetErrorCode()
	case *adksession.Event:
		if ev != nil && ev.LLMResponse.Content != nil {
			return string(ev.LLMResponse.FinishReason)
		}
		return ""
	default:
		return ""
	}
}

// extractErrorMessage extracts error_message from an event using type switches.
func extractErrorMessage(e interface{}) string {
	switch ev := e.(type) {
	case event.ErrorEventProvider:
		return ev.GetErrorMessage()
	case *adksession.Event:
		// ADK events don't have explicit error messages; derive from finish reason
		if ev != nil && ev.LLMResponse.Content != nil {
			return genai.GetErrorMessage(string(ev.LLMResponse.FinishReason))
		}
		return ""
	default:
		return ""
	}
}

// getContextMetadata gets the context metadata for the event using type switches.
// This matches Python's _get_context_metadata function.
func getContextMetadata(e interface{}, cc a2a.ConversionContext) map[string]interface{} {
	metadata := map[string]interface{}{
		a2a.MetadataKeyAppName:       cc.AppName,
		a2a.MetadataKeyUserIDFull:    cc.UserID,
		a2a.MetadataKeySessionIDFull: cc.SessionID,
	}

	switch ev := e.(type) {
	case *adksession.Event:
		if ev != nil {
			if ev.Author != "" {
				metadata[a2a.MetadataKeyAuthor] = ev.Author
			}
			if ev.InvocationID != "" {
				metadata[a2a.MetadataKeyInvocationID] = ev.InvocationID
			}
			if errorCode := extractErrorCode(ev); errorCode != "" {
				metadata[a2a.MetadataKeyErrorCode] = errorCode
			}
		}
	case *event.RunnerErrorEvent:
		if ev != nil {
			if ev.ErrorCode != "" {
				metadata[a2a.MetadataKeyErrorCode] = ev.ErrorCode
			}
		}
	}

	return metadata
}

// processLongRunningTool processes long-running tool metadata for an A2A part.
// This matches Python's _process_long_running_tool function.
func processLongRunningTool(a2aPart protocol.Part, e interface{}) {
	longRunningToolIDs := extractLongRunningToolIDs(e)

	dataPart, ok := a2aPart.(*protocol.DataPart)
	if !ok {
		return
	}

	if dataPart.Metadata == nil {
		dataPart.Metadata = make(map[string]interface{})
	}

	partType, _ := dataPart.Metadata[a2a.MetadataKeyType].(string)
	if partType != a2a.A2ADataPartMetadataTypeFunctionCall {
		return
	}

	dataMap, ok := dataPart.Data.(map[string]interface{})
	if !ok {
		return
	}

	id, _ := dataMap["id"].(string)
	if id == "" {
		return
	}

	for _, longRunningID := range longRunningToolIDs {
		if id == longRunningID {
			dataPart.Metadata[a2a.MetadataKeyIsLongRunning] = true
			break
		}
	}
}

// extractLongRunningToolIDs extracts LongRunningToolIDs from an event using type switches.
func extractLongRunningToolIDs(e interface{}) []string {
	switch ev := e.(type) {
	case *adksession.Event:
		if ev != nil {
			return ev.LongRunningToolIDs
		}
	}
	return nil
}

// createErrorStatusEvent creates a TaskStatusUpdateEvent for error scenarios.
// This matches Python's _create_error_status_event function
func createErrorStatusEvent(event interface{}, cc a2a.ConversionContext) *protocol.TaskStatusUpdateEvent {
	errorCode := extractErrorCode(event)
	errorMessage := extractErrorMessage(event)

	metadata := getContextMetadata(event, cc)
	if errorCode != "" && errorMessage == "" {
		errorMessage = genai.GetErrorMessage(errorCode)
	}

	// Build message metadata with error code if present
	messageMetadata := make(map[string]interface{})
	if errorCode != "" {
		messageMetadata[a2a.MetadataKeyErrorCode] = errorCode
	}

	return &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    cc.TaskID,
		ContextID: cc.ContextID,
		Metadata:  metadata,
		Status: protocol.TaskStatus{
			State: protocol.TaskStateFailed,
			Message: &protocol.Message{
				MessageID: uuid.New().String(),
				Role:      protocol.MessageRoleAgent,
				Parts: []protocol.Part{
					protocol.NewTextPart(errorMessage),
				},
				Metadata: messageMetadata,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false, // Not final - error events are not final (matching Python)
	}
}

// ConvertEventToA2AEvents converts runner events to A2A events. Uses only *adksession.Event (Google ADK) and RunnerErrorEvent.
// No internal event/content types: GenAI parts from ADK → A2A only.
func ConvertEventToA2AEvents(event interface{}, cc a2a.ConversionContext) []protocol.Event {
	if adkEvent, ok := event.(*adksession.Event); ok {
		return convertADKEventToA2AEvents(adkEvent, cc)
	}

	// RunnerErrorEvent or any type with ErrorCode: only error path
	errorCode := extractErrorCode(event)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		return []protocol.Event{createErrorStatusEvent(event, cc)}
	}
	// STOP with no content or unknown type: no events
	return nil
}

// convertADKEventToA2AEvents converts *adksession.Event to A2A events (like Python convert_event_to_a2a_events(adk_event)).
// Uses GenAIPartToA2APart for direct genai.Part → protocol.Part conversion.
func convertADKEventToA2AEvents(adkEvent *adksession.Event, cc a2a.ConversionContext) []protocol.Event {
	errorCode := extractErrorCode(adkEvent)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		return []protocol.Event{createErrorStatusEvent(adkEvent, cc)}
	}

	// Use LLMResponse.Content with fallback to Content so tool/progress events are not missed
	content := adkEventContent(adkEvent)
	if errorCode == genai.FinishReasonStop && (content == nil || len(content.Parts) == 0) {
		return nil
	}
	if content == nil || len(content.Parts) == 0 {
		return nil
	}

	var a2aParts []protocol.Part
	for _, part := range content.Parts {
		a2aPart, err := GenAIPartToA2APart(part)
		if err != nil || a2aPart == nil {
			continue
		}
		processLongRunningTool(a2aPart, adkEvent)
		a2aParts = append(a2aParts, a2aPart)
	}

	if len(a2aParts) == 0 {
		return nil
	}

	messageMetadata := make(map[string]interface{})
	if adkEvent.Partial {
		messageMetadata["adk_partial"] = true
	}
	message := &protocol.Message{
		Kind:      protocol.KindMessage,
		MessageID: uuid.New().String(),
		Role:      protocol.MessageRoleAgent,
		Parts:     a2aParts,
		Metadata:  messageMetadata,
	}

	state := determineTaskState(a2aParts)
	metadata := getContextMetadata(adkEvent, cc)

	return []protocol.Event{&protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    cc.TaskID,
		ContextID: cc.ContextID,
		Status: protocol.TaskStatus{
			State:     state,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Message:   message,
		},
		Metadata: metadata,
		Final:    false,
	}}
}

// determineTaskState inspects converted parts to decide the A2A task state.
// working by default; auth_required if any part is long-running function_call with name "request_euc";
// else input_required if any part is long-running function_call.
func determineTaskState(parts []protocol.Part) protocol.TaskState {
	state := protocol.TaskStateWorking
	for _, part := range parts {
		dataPart, ok := part.(*protocol.DataPart)
		if !ok || dataPart.Metadata == nil {
			continue
		}
		partType, _ := dataPart.Metadata[a2a.MetadataKeyType].(string)
		isLongRunning, _ := dataPart.Metadata[a2a.MetadataKeyIsLongRunning].(bool)
		if partType == a2a.A2ADataPartMetadataTypeFunctionCall && isLongRunning {
			if dataMap, ok := dataPart.Data.(map[string]interface{}); ok {
				if name, _ := dataMap[a2a.PartKeyName].(string); name == requestEucFunctionCallName {
					return protocol.TaskStateAuthRequired
				}
				state = protocol.TaskStateInputRequired
			}
		}
	}
	return state
}

// adkEventContent returns the content from an ADK event, preferring LLMResponse.Content
// over Content. This avoids missing tool/progress events.
func adkEventContent(e *adksession.Event) *gogenai.Content {
	if e.LLMResponse.Content != nil {
		return e.LLMResponse.Content
	}
	return e.Content
}

// IsPartialEvent checks if the event is partial (only *adksession.Event has Partial).
func IsPartialEvent(event interface{}) bool {
	if e, ok := event.(*adksession.Event); ok {
		return e.Partial
	}
	return false
}
