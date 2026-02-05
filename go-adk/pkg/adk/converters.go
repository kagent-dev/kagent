package adk

import (
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	adksession "google.golang.org/adk/session"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	// RequestEucFunctionCallName is the name of the request_euc function call
	requestEucFunctionCallName = "request_euc"
)

// extractErrorCode extracts error_code from an event using reflection
// This is a helper function to work with generic event interface{}
func extractErrorCode(event interface{}) string {
	return extractStringField(event, "ErrorCode")
}

// extractErrorMessage extracts error_message from an event using reflection
func extractErrorMessage(event interface{}) string {
	return extractStringField(event, "ErrorMessage")
}

// extractStringField extracts a string field from an event using reflection
func extractStringField(event interface{}, fieldName string) string {
	if event == nil {
		return ""
	}
	v := getStructValue(event)
	if !v.IsValid() {
		return ""
	}
	field := v.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

// getStructValue gets the struct value from an event, handling pointers
func getStructValue(event interface{}) reflect.Value {
	if event == nil {
		return reflect.Value{}
	}
	v := reflect.ValueOf(event)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return v
}

// getContextMetadata gets the context metadata for the event
// This matches Python's _get_context_metadata function
func getContextMetadata(
	event interface{},
	appName string,
	userID string,
	sessionID string,
) map[string]interface{} {
	metadata := map[string]interface{}{
		core.GetKAgentMetadataKey("app_name"):                appName,
		core.GetKAgentMetadataKey(core.MetadataKeyUserID):    userID,
		core.GetKAgentMetadataKey(core.MetadataKeySessionID): sessionID,
	}

	// Extract optional metadata fields from event using reflection
	if event != nil {
		v := reflect.ValueOf(event)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}
		if v.Kind() == reflect.Struct {
			// Extract author
			if authorField := v.FieldByName("Author"); authorField.IsValid() && authorField.Kind() == reflect.String {
				if author := authorField.String(); author != "" {
					metadata[core.GetKAgentMetadataKey("author")] = author
				}
			}

			// Extract invocation_id (if present)
			if invocationIDField := v.FieldByName("InvocationID"); invocationIDField.IsValid() {
				if invocationIDField.Kind() == reflect.String {
					if id := invocationIDField.String(); id != "" {
						metadata[core.GetKAgentMetadataKey("invocation_id")] = id
					}
				}
			}

			// Extract error_code (if present)
			if errorCode := extractErrorCode(event); errorCode != "" {
				metadata[core.GetKAgentMetadataKey("error_code")] = errorCode
			}

			// Extract optional fields: branch, grounding_metadata, custom_metadata, usage_metadata
			// These would require more complex reflection or type assertions
			// For now, we'll skip them as they're optional
		}
	}

	return metadata
}

// processLongRunningTool processes long-running tool metadata for an A2A part
// This matches Python's _process_long_running_tool function
func processLongRunningTool(a2aPart protocol.Part, event interface{}) {
	// Extract long_running_tool_ids from event using reflection
	longRunningToolIDs := extractLongRunningToolIDs(event)

	// Check if this part is a long-running tool
	dataPart, ok := a2aPart.(*protocol.DataPart)
	if !ok {
		return
	}

	if dataPart.Metadata == nil {
		dataPart.Metadata = make(map[string]interface{})
	}

	partType, _ := dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)].(string)
	if partType != core.A2ADataPartMetadataTypeFunctionCall {
		return
	}

	// Check if this function call ID is in the long-running list
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
			dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataIsLongRunningKey)] = true
			break
		}
	}
}

// extractLongRunningToolIDs extracts LongRunningToolIDs from an event using reflection
func extractLongRunningToolIDs(event interface{}) []string {
	if event == nil {
		return nil
	}
	v := getStructValue(event)
	if !v.IsValid() {
		return nil
	}

	field := v.FieldByName("LongRunningToolIDs")
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return nil
	}

	var ids []string
	for i := 0; i < field.Len(); i++ {
		if id := field.Index(i).String(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// createErrorStatusEvent creates a TaskStatusUpdateEvent for error scenarios.
// This matches Python's _create_error_status_event function
func createErrorStatusEvent(
	event interface{},
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) *protocol.TaskStatusUpdateEvent {
	errorCode := extractErrorCode(event)
	errorMessage := extractErrorMessage(event)

	metadata := getContextMetadata(event, appName, userID, sessionID)
	if errorCode != "" && errorMessage == "" {
		errorMessage = genai.GetErrorMessage(errorCode)
	}

	// Build message metadata with error code if present
	messageMetadata := make(map[string]interface{})
	if errorCode != "" {
		messageMetadata[core.GetKAgentMetadataKey("error_code")] = errorCode
	}

	return &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
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
func ConvertEventToA2AEvents(
	event interface{}, // *adksession.Event or *RunnerErrorEvent
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) []protocol.Event {
	if adkEvent, ok := event.(*adksession.Event); ok {
		return convertADKEventToA2AEvents(adkEvent, taskID, contextID, appName, userID, sessionID)
	}

	// RunnerErrorEvent or any type with ErrorCode: only error path
	errorCode := extractErrorCode(event)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		return []protocol.Event{createErrorStatusEvent(event, taskID, contextID, appName, userID, sessionID)}
	}
	// STOP with no content or unknown type: no events
	return nil
}

// convertADKEventToA2AEvents converts *adksession.Event to A2A events (like Python convert_event_to_a2a_events(adk_event)).
// Uses genai.Part → map via GenAIPartStructToMap then ConvertGenAIPartToA2APart (same as Python convert_genai_part_to_a2a_part).
func convertADKEventToA2AEvents(
	adkEvent *adksession.Event,
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) []protocol.Event {
	var a2aEvents []protocol.Event
	timestamp := time.Now().UTC().Format(time.RFC3339)
	metadata := map[string]interface{}{
		core.GetKAgentMetadataKey("app_name"):                appName,
		core.GetKAgentMetadataKey(core.MetadataKeyUserID):    userID,
		core.GetKAgentMetadataKey(core.MetadataKeySessionID): sessionID,
	}

	errorCode := extractErrorCode(adkEvent)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		a2aEvents = append(a2aEvents, createErrorStatusEvent(adkEvent, taskID, contextID, appName, userID, sessionID))
		return a2aEvents
	}

	// Use LLMResponse.Content (same as event.go adkEventHasToolContent) so tool/progress events are not missed
	content := adkEvent.LLMResponse.Content
	if content == nil {
		content = adkEvent.Content
	}
	if errorCode == genai.FinishReasonStop {
		hasContent := content != nil && len(content.Parts) > 0
		if !hasContent {
			return a2aEvents
		}
	}

	if content == nil || len(content.Parts) == 0 {
		return a2aEvents
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
		return a2aEvents
	}

	isPartial := adkEvent.Partial
	messageMetadata := make(map[string]interface{})
	if isPartial {
		messageMetadata["adk_partial"] = true
	}
	message := &protocol.Message{
		Kind:      protocol.KindMessage,
		MessageID: uuid.New().String(),
		Role:      protocol.MessageRoleAgent,
		Parts:     a2aParts,
		Metadata:  messageMetadata,
	}

	state := protocol.TaskStateWorking
	for _, part := range a2aParts {
		if dataPart, ok := part.(*protocol.DataPart); ok && dataPart.Metadata != nil {
			partType, _ := dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)].(string)
			isLongRunning, _ := dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataIsLongRunningKey)].(bool)
			if partType == core.A2ADataPartMetadataTypeFunctionCall && isLongRunning {
				if dataMap, ok := dataPart.Data.(map[string]interface{}); ok {
					if name, _ := dataMap[core.PartKeyName].(string); name == requestEucFunctionCallName {
						state = protocol.TaskStateAuthRequired
						break
					}
					state = protocol.TaskStateInputRequired
				}
			}
		}
	}

	a2aEvents = append(a2aEvents, &protocol.TaskStatusUpdateEvent{
		Kind:      "status-update",
		TaskID:    taskID,
		ContextID: contextID,
		Status: protocol.TaskStatus{
			State:     state,
			Timestamp: timestamp,
			Message:   message,
		},
		Metadata: metadata,
		Final:    false,
	})
	return a2aEvents
}

// IsPartialEvent checks if the event is partial (only *adksession.Event has Partial).
func IsPartialEvent(event interface{}) bool {
	if e, ok := event.(*adksession.Event); ok {
		return e.Partial
	}
	return false
}
