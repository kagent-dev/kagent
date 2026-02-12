package a2a

import (
	"time"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/model"
	adksession "google.golang.org/adk/session"
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
func processLongRunningTool(a2aPart a2aschema.Part, adkEvent *adksession.Event) {
	if adkEvent == nil {
		return
	}

	dataPart, ok := a2aPart.(*a2aschema.DataPart)
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

	id, _ := dataPart.Data["id"].(string)
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
	infoProvider a2aschema.TaskInfoProvider,
	appName string,
	userID string,
	sessionID string,
) *a2aschema.TaskStatusUpdateEvent {
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

	msg := a2aschema.NewMessage(a2aschema.MessageRoleAgent, &a2aschema.TextPart{Text: errorMsg})
	msg.Metadata = messageMetadata

	event := a2aschema.NewStatusUpdateEvent(infoProvider, a2aschema.TaskStateFailed, msg)
	event.Metadata = metadata
	event.Final = false // Not final - error events are not final (matching Python)
	return event
}

// ConvertADKEventToA2AEvents converts *adksession.Event to A2A events.
// Uses genai.Part -> map via GenAIPartStructToMap then ConvertGenAIPartToA2APart.
func ConvertADKEventToA2AEvents(
	adkEvent *adksession.Event,
	infoProvider a2aschema.TaskInfoProvider,
	appName string,
	userID string,
	sessionID string,
) []a2aschema.Event {
	var a2aEvents []a2aschema.Event
	metadata := getContextMetadata(adkEvent, appName, userID, sessionID)

	// Use LLMResponse.Content so tool/progress events are not missed
	content := adkEvent.LLMResponse.Content
	if content == nil {
		content = adkEvent.Content
	}

	if content == nil || len(content.Parts) == 0 {
		return a2aEvents
	}

	var a2aParts a2aschema.ContentParts
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
	message := a2aschema.NewMessage(a2aschema.MessageRoleAgent, a2aParts...)
	message.Metadata = messageMetadata

	// User response and questions: set task state so clients know when to prompt the user.
	state := a2aschema.TaskStateWorking
	for _, part := range a2aParts {
		if dataPart, ok := part.(*a2aschema.DataPart); ok && dataPart.Metadata != nil {
			partType, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)].(string)
			isLongRunning, _ := dataPart.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataIsLongRunningKey)].(bool)
			if partType == A2ADataPartMetadataTypeFunctionCall && isLongRunning {
				if name, _ := dataPart.Data["name"].(string); name == requestEucFunctionCallName {
					state = a2aschema.TaskStateAuthRequired
					break
				}
				state = a2aschema.TaskStateInputRequired
			}
		}
	}

	now := time.Now().UTC()
	event := &a2aschema.TaskStatusUpdateEvent{
		TaskID:    infoProvider.TaskInfo().TaskID,
		ContextID: infoProvider.TaskInfo().ContextID,
		Status: a2aschema.TaskStatus{
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
