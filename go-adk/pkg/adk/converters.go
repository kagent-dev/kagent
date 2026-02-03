package adk

import (
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
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

// extractContent extracts Content field from an event using reflection
func extractContent(event interface{}) interface{} {
	if event == nil {
		return nil
	}
	v := reflect.ValueOf(event)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	contentField := v.FieldByName("Content")
	if !contentField.IsValid() {
		return nil
	}
	// IsNil() is only valid for Chan, Func, Map, Ptr, Slice, Interface; panics otherwise
	switch contentField.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Ptr, reflect.Slice, reflect.Interface:
		if contentField.IsNil() {
			return nil
		}
	}
	return contentField.Interface()
}

// extractContentParts extracts Parts from content using reflection
func extractContentParts(content interface{}) []interface{} {
	if content == nil {
		return nil
	}
	v := reflect.ValueOf(content)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	partsField := v.FieldByName("Parts")
	if !partsField.IsValid() {
		return nil
	}
	if partsField.Kind() != reflect.Slice {
		return nil
	}
	var parts []interface{}
	for i := 0; i < partsField.Len(); i++ {
		parts = append(parts, partsField.Index(i).Interface())
	}
	return parts
}

// extractPartial extracts Partial field from an event using reflection
// This matches Python's event.partial field
func extractPartial(event interface{}) bool {
	if event == nil {
		return false
	}
	v := getStructValue(event)
	if !v.IsValid() {
		return false
	}
	partialField := v.FieldByName("Partial")
	if !partialField.IsValid() || partialField.Kind() != reflect.Bool {
		return false
	}
	return partialField.Bool()
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
		core.GetKAgentMetadataKey("app_name"):   appName,
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
		errorMessage = GetErrorMessage(errorCode)
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

// ConvertEventToA2AEvents converts internal agent events to A2A events.
// This matches the Python implementation's convert_event_to_a2a_events function.
// Accepts either *adksession.Event (Python-style: ADK event → A2A directly) or our *Event.
func ConvertEventToA2AEvents(
	event interface{}, // *adksession.Event or our *Event
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) []protocol.Event {
	// Python-style: convert ADK Event to A2A directly (no intermediate "ours" type)
	if adkEvent, ok := event.(*adksession.Event); ok {
		return convertADKEventToA2AEvents(adkEvent, taskID, contextID, appName, userID, sessionID)
	}

	var a2aEvents []protocol.Event
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Build context metadata (matching Python _get_context_metadata)
	metadata := map[string]interface{}{
		core.GetKAgentMetadataKey("app_name"):   appName,
		core.GetKAgentMetadataKey(core.MetadataKeyUserID):    userID,
		core.GetKAgentMetadataKey(core.MetadataKeySessionID): sessionID,
	}

	// Handle error scenarios (matching Python: if event.error_code and not _is_normal_completion(event.error_code))
	errorCode := extractErrorCode(event)
	if errorCode != "" && !IsNormalCompletion(errorCode) {
		errorEvent := createErrorStatusEvent(event, taskID, contextID, appName, userID, sessionID)
		a2aEvents = append(a2aEvents, errorEvent)
		return a2aEvents
	}

	// If error code is STOP (normal completion) with no content, don't create any events
	if errorCode == FinishReasonStop {
		// Check if there's any content to convert
		hasContent := false
		if content := extractContent(event); content != nil {
			if parts := extractContentParts(content); len(parts) > 0 {
				hasContent = true
			}
		}
		// If no content, return empty events (normal completion with nothing to show)
		if !hasContent {
			return a2aEvents
		}
	}

	// Handle regular message content (matching Python: convert_event_to_a2a_message)
	// Convert event content to A2A message with parts
	content := extractContent(event)
	if content == nil {
		// No content to convert, return empty events
		return a2aEvents
	}

	parts := extractContentParts(content)
	if len(parts) == 0 {
		// No parts to convert, return empty events
		return a2aEvents
	}

	// Convert each part from internal format to A2A parts
	// Internal format: map[string]interface{} with PartKeyText or PartKeyFunctionCall
	// A2A format: protocol.Part (TextPart, DataPart, etc.)
	var a2aParts []protocol.Part
	for _, part := range parts {
		// Convert internal part (map[string]interface{}) to A2A part
		if partMap, ok := part.(map[string]interface{}); ok {
			// Convert to GenAI part format first, then to A2A part
			genaiPart := partMap // Already in GenAI format from runner
			a2aPart, err := ConvertGenAIPartToA2APart(genaiPart)
			if err != nil {
				// Log error but continue with other parts
				continue
			}
			if a2aPart != nil {
				// Process long-running tool metadata (matching Python: _process_long_running_tool)
				processLongRunningTool(a2aPart, event)
				a2aParts = append(a2aParts, a2aPart)
			}
		}
	}

	if len(a2aParts) == 0 {
		// No valid parts converted, return empty events
		return a2aEvents
	}

	// Extract partial flag from event (matching Python: event.partial)
	isPartial := extractPartial(event)

	// Set adk_partial metadata in message (matching Python: message_metadata = {"adk_partial": event.partial})
	messageMetadata := make(map[string]interface{})
	if isPartial {
		messageMetadata["adk_partial"] = true
	}

	// Create A2A message with converted parts (matching Python: convert_event_to_a2a_message)
	message := &protocol.Message{
		Kind:      protocol.KindMessage,
		MessageID: uuid.New().String(),
		Role:      protocol.MessageRoleAgent,
		Parts:     a2aParts,
		Metadata:  messageMetadata,
	}

	// Create status update event (matching Python: _create_status_update_event)
	// Default state is working, but could be auth_required or input_required for long-running tools
	state := protocol.TaskStateWorking

	// Check for long-running tools and set state accordingly (matching Python _create_status_update_event)
	// Check for auth_required (REQUEST_EUC_FUNCTION_CALL_NAME)
	for _, part := range a2aParts {
		if dataPart, ok := part.(*protocol.DataPart); ok {
			if dataPart.Metadata != nil {
				partType, _ := dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)].(string)
				isLongRunning, _ := dataPart.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataIsLongRunningKey)].(bool)

				if partType == core.A2ADataPartMetadataTypeFunctionCall && isLongRunning {
					// Check if it's the REQUEST_EUC function call
					if dataMap, ok := dataPart.Data.(map[string]interface{}); ok {
						if name, _ := dataMap[PartKeyName].(string); name == requestEucFunctionCallName {
							state = protocol.TaskStateAuthRequired
							break
						}
						// Otherwise, it's a regular long-running tool requiring input
						state = protocol.TaskStateInputRequired
					}
				}
			}
		}
	}

	statusUpdate := &protocol.TaskStatusUpdateEvent{
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
	}
	a2aEvents = append(a2aEvents, statusUpdate)

	return a2aEvents
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
		core.GetKAgentMetadataKey("app_name"):   appName,
		core.GetKAgentMetadataKey(core.MetadataKeyUserID):    userID,
		core.GetKAgentMetadataKey(core.MetadataKeySessionID): sessionID,
	}

	errorCode := extractErrorCode(adkEvent)
	if errorCode != "" && !IsNormalCompletion(errorCode) {
		a2aEvents = append(a2aEvents, createErrorStatusEvent(adkEvent, taskID, contextID, appName, userID, sessionID))
		return a2aEvents
	}

	// Use LLMResponse.Content (same as event.go adkEventHasToolContent) so tool/progress events are not missed
	content := adkEvent.LLMResponse.Content
	if content == nil {
		content = adkEvent.Content
	}
	if errorCode == FinishReasonStop {
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
		partMap := GenAIPartStructToMap(part)
		if partMap == nil {
			continue
		}
		a2aPart, err := ConvertGenAIPartToA2APart(partMap)
		if err != nil || a2aPart == nil {
			continue
		}
		processLongRunningTool(a2aPart, adkEvent)
		a2aParts = append(a2aParts, a2aPart)
	}

	if len(a2aParts) == 0 {
		return a2aEvents
	}

	isPartial := extractPartial(adkEvent)
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
					if name, _ := dataMap[PartKeyName].(string); name == requestEucFunctionCallName {
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

// IsPartialEvent checks if an internal event is partial (for filtering from aggregation)
func IsPartialEvent(event interface{}) bool {
	return extractPartial(event)
}

// ConvertA2ARequestToRunArgs converts an A2A request to internal agent run arguments.
// This matches the Python implementation's convert_a2a_request_to_adk_run_args function
func ConvertA2ARequestToRunArgs(req *protocol.SendMessageParams, userID, sessionID string) map[string]interface{} {
	if req == nil {
		// Return minimal args if request is nil (matching Python: raises ValueError)
		return map[string]interface{}{
			ArgKeyUserID:    userID,
			ArgKeySessionID: sessionID,
		}
	}

	args := make(map[string]interface{})

	// Set user_id (matching Python: _get_user_id(request))
	args[ArgKeyUserID] = userID
	args[ArgKeySessionID] = sessionID

	// Convert A2A message parts to GenAI format (matching Python: convert_a2a_part_to_genai_part)
	var genaiParts []map[string]interface{}
	for _, part := range req.Message.Parts {
		genaiPart, err := ConvertA2APartToGenAIPart(part)
		if err != nil {
			// Log error but continue with other parts
			continue
		}
		if genaiPart != nil {
			genaiParts = append(genaiParts, genaiPart)
		}
	}

	// Create Content object (matching Python: genai_types.Content(role="user", parts=[...]))
	args[ArgKeyNewMessage] = map[string]interface{}{
		PartKeyRole:  "user",
		PartKeyParts: genaiParts,
	}
	// Also set as message for compatibility
	args[ArgKeyMessage] = req.Message

	// Extract streaming mode from request if available
	// In Python: RunConfig(streaming_mode=StreamingMode.SSE if stream else StreamingMode.NONE)
	// For now, we'll set a default - the executor config will determine actual streaming mode
	args[ArgKeyRunConfig] = map[string]interface{}{
		RunConfigKeyStreamingMode: "NONE", // Default, will be overridden by executor config
	}

	return args
}
