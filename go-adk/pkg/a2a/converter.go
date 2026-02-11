package a2a

import (
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	adksession "google.golang.org/adk/session"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

const (
	// requestEucFunctionCallName is the name of the request_euc function call
	requestEucFunctionCallName = "request_euc"
)

// getContextMetadata builds context metadata for an A2A event from a typed ADK event.
func getContextMetadata(
	adkEvent *adksession.Event,
	appName string,
	userID string,
	sessionID string,
) map[string]any {
	metadata := map[string]any{
		GetKAgentMetadataKey("app_name"):           appName,
		GetKAgentMetadataKey(MetadataKeyUserID):    userID,
		GetKAgentMetadataKey(MetadataKeySessionID): sessionID,
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
func processLongRunningTool(a2aPart protocol.Part, adkEvent *adksession.Event) {
	if adkEvent == nil {
		return
	}

	dataPart, ok := a2aPart.(*protocol.DataPart)
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

	dataMap, ok := dataPart.Data.(map[string]any)
	if !ok {
		return
	}

	id, _ := dataMap["id"].(string)
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
	errorCode string,
	errorMsg string,
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) *protocol.TaskStatusUpdateEvent {
	metadata := map[string]any{
		GetKAgentMetadataKey("app_name"):           appName,
		GetKAgentMetadataKey(MetadataKeyUserID):    userID,
		GetKAgentMetadataKey(MetadataKeySessionID): sessionID,
	}
	if errorCode != "" {
		metadata[GetKAgentMetadataKey("error_code")] = errorCode
	}

	if errorCode != "" && errorMsg == "" {
		errorMsg = model.GetErrorMessage(errorCode)
	}

	messageMetadata := make(map[string]any)
	if errorCode != "" {
		messageMetadata[GetKAgentMetadataKey("error_code")] = errorCode
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
					protocol.NewTextPart(errorMsg),
				},
				Metadata: messageMetadata,
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Final: false, // Not final - error events are not final (matching Python)
	}
}

// ConvertADKEventToA2AEvents converts *adksession.Event to A2A events.
// Uses genai.Part -> map via GenAIPartStructToMap then ConvertGenAIPartToA2APart.
func ConvertADKEventToA2AEvents(
	adkEvent *adksession.Event,
	taskID string,
	contextID string,
	appName string,
	userID string,
	sessionID string,
) []protocol.Event {
	var a2aEvents []protocol.Event
	timestamp := time.Now().UTC().Format(time.RFC3339)
	metadata := getContextMetadata(adkEvent, appName, userID, sessionID)

	// Use LLMResponse.Content so tool/progress events are not missed
	content := adkEvent.LLMResponse.Content
	if content == nil {
		content = adkEvent.Content
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
	messageMetadata := make(map[string]any)
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

	// User response and questions: set task state so clients know when to prompt the user.
	state := protocol.TaskStateWorking
	for _, part := range a2aParts {
		if dataPart, ok := part.(*protocol.DataPart); ok && dataPart.Metadata != nil {
			partType, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string)
			isLongRunning, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataIsLongRunningKey)].(bool)
			if partType == A2ADataPartMetadataTypeFunctionCall && isLongRunning {
				if dataMap, ok := dataPart.Data.(map[string]any); ok {
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
