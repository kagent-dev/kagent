package a2a

import (
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	requestEucFunctionCallName = "request_euc"
)

// getContextMetadata builds context metadata for an A2A event from a typed ADK event.
func getContextMetadata(adkEvent *adksession.Event, appName, userID, sessionID string) map[string]any {
	metadata := map[string]any{
		GetKAgentMetadataKey("app_name"):   appName,
		GetKAgentMetadataKey("user_id"):    userID,
		GetKAgentMetadataKey("session_id"): sessionID,
	}
	if adkEvent != nil {
		if adkEvent.Author != "" {
			metadata[GetKAgentMetadataKey("author")] = adkEvent.Author
		}
		if adkEvent.InvocationID != "" {
			metadata[GetKAgentMetadataKey("invocation_id")] = adkEvent.InvocationID
		}
	}
	return metadata
}

// processLongRunningTool processes long-running tool metadata for an A2A part.
func processLongRunningTool(a2aPart a2atype.Part, adkEvent *adksession.Event) {
	if adkEvent == nil {
		return
	}
	dataPart, ok := a2aPart.(*a2atype.DataPart)
	if !ok {
		return
	}
	if dataPart.Metadata == nil {
		dataPart.Metadata = make(map[string]any)
	}
	partType, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string)
	if partType != A2ADataPartMetadataTypeFunctionCall {
		return
	}
	id, _ := dataPart.Data[PartKeyID].(string)
	if id == "" {
		return
	}
	for _, longRunningID := range adkEvent.LongRunningToolIDs {
		if id == longRunningID {
			dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataIsLongRunningKey)] = true
			break
		}
	}
}

// CreateErrorA2AEvent creates a TaskStatusUpdateEvent for an error from the runner iterator.
func CreateErrorA2AEvent(
	errorCode, errorMsg string,
	infoProvider a2atype.TaskInfoProvider,
	appName, userID, sessionID string,
) *a2atype.TaskStatusUpdateEvent {
	metadata := map[string]any{
		GetKAgentMetadataKey("app_name"):   appName,
		GetKAgentMetadataKey("user_id"):    userID,
		GetKAgentMetadataKey("session_id"): sessionID,
	}
	if errorCode != "" {
		metadata[GetKAgentMetadataKey("error_code")] = errorCode
	}
	if errorCode != "" && errorMsg == "" {
		errorMsg = GetErrorMessage(errorCode)
	}

	messageMetadata := make(map[string]any)
	if errorCode != "" {
		messageMetadata[GetKAgentMetadataKey("error_code")] = errorCode
	}

	msg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: errorMsg})
	msg.Metadata = messageMetadata

	event := a2atype.NewStatusUpdateEvent(infoProvider, a2atype.TaskStateFailed, msg)
	event.Metadata = metadata
	event.Final = false
	return event
}

// ConvertADKEventToA2AEvents converts *adksession.Event to A2A events.
func ConvertADKEventToA2AEvents(
	adkEvent *adksession.Event,
	infoProvider a2atype.TaskInfoProvider,
	appName, userID, sessionID string,
) []a2atype.Event {
	if adkEvent == nil {
		return nil
	}

	var a2aEvents []a2atype.Event
	metadata := getContextMetadata(adkEvent, appName, userID, sessionID)

	// LLMResponse is embedded in Event, so LLMResponse.Content and
	// Content are the same field. Access it directly.
	content := adkEvent.Content
	if content == nil || len(content.Parts) == 0 {
		return a2aEvents
	}

	var a2aParts a2atype.ContentParts
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

	messageMetadata := make(map[string]any)
	if adkEvent.Partial {
		messageMetadata["adk_partial"] = true
	}
	message := a2atype.NewMessage(a2atype.MessageRoleAgent, a2aParts...)
	message.Metadata = messageMetadata

	// Determine task state based on long-running tools
	state := a2atype.TaskStateWorking
	for _, part := range a2aParts {
		if dataPart, ok := part.(*a2atype.DataPart); ok && dataPart.Metadata != nil {
			partType, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string)
			isLongRunning, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataIsLongRunningKey)].(bool)
			if partType == A2ADataPartMetadataTypeFunctionCall && isLongRunning {
				if name, _ := dataPart.Data[PartKeyName].(string); name == requestEucFunctionCallName {
					state = a2atype.TaskStateAuthRequired
					break
				}
				state = a2atype.TaskStateInputRequired
			}
		}
	}

	now := time.Now().UTC()
	event := &a2atype.TaskStatusUpdateEvent{
		TaskID:    infoProvider.TaskInfo().TaskID,
		ContextID: infoProvider.TaskInfo().ContextID,
		Status: a2atype.TaskStatus{
			State:     state,
			Timestamp: &now,
			Message:   message,
		},
		Metadata: metadata,
		Final:    false,
	}
	a2aEvents = append(a2aEvents, event)
	return a2aEvents
}

// ExtractToolApprovalRequests checks an ADK event for long-running function
// calls that require user approval and returns them as ToolApprovalRequest
// objects. Auth-related function calls (request_euc) are excluded.
func ExtractToolApprovalRequests(adkEvent *adksession.Event) []ToolApprovalRequest {
	if adkEvent == nil || adkEvent.Partial || len(adkEvent.LongRunningToolIDs) == 0 {
		return nil
	}

	content := adkEvent.Content
	if content == nil || len(content.Parts) == 0 {
		return nil
	}

	longRunningSet := make(map[string]bool, len(adkEvent.LongRunningToolIDs))
	for _, id := range adkEvent.LongRunningToolIDs {
		longRunningSet[id] = true
	}

	var requests []ToolApprovalRequest
	for _, part := range content.Parts {
		fc := extractFunctionCall(part)
		if fc == nil || fc.Name == requestEucFunctionCallName {
			continue
		}
		if fc.ID != "" && longRunningSet[fc.ID] {
			requests = append(requests, ToolApprovalRequest{
				Name: fc.Name,
				Args: fc.Args,
				ID:   fc.ID,
			})
		}
	}
	return requests
}

// extractFunctionCall returns the FunctionCall from a genai.Part, or nil.
func extractFunctionCall(part *genai.Part) *genai.FunctionCall {
	if part == nil || part.FunctionCall == nil {
		return nil
	}
	return part.FunctionCall
}
