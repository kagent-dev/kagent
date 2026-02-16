package converter

import (
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/event"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/genai"
	adksession "google.golang.org/adk/session"
	gogenai "google.golang.org/genai"
)

func timePtr(t time.Time) *time.Time { return &t }

const (
	// RequestEucFunctionCallName is the name of the request_euc function call
	requestEucFunctionCallName = "request_euc"
)

// extractErrorInfo extracts error code and message from an event in a single type switch.
func extractErrorInfo(e interface{}) (errorCode, errorMessage string) {
	switch ev := e.(type) {
	case event.ErrorEventProvider:
		return ev.GetErrorCode(), ev.GetErrorMessage()
	case *adksession.Event:
		if ev != nil && ev.LLMResponse.Content != nil {
			code := string(ev.LLMResponse.FinishReason)
			return code, genai.GetErrorMessage(code)
		}
	}
	return "", ""
}

// getContextMetadata gets the context metadata for the event using type switches.
// errorCode is passed in to avoid redundant extraction from the same event.
// This matches Python's _get_context_metadata function.
func getContextMetadata(e interface{}, cc a2a.ConversionContext, errorCode string) map[string]interface{} {
	metadata := map[string]interface{}{
		a2a.MetadataKeyAppName:       cc.AppName,
		a2a.MetadataKeyUserIDFull:    cc.UserID,
		a2a.MetadataKeySessionIDFull: cc.SessionID,
	}

	if ev, ok := e.(*adksession.Event); ok && ev != nil {
		if ev.Author != "" {
			metadata[a2a.MetadataKeyAuthor] = ev.Author
		}
		if ev.InvocationID != "" {
			metadata[a2a.MetadataKeyInvocationID] = ev.InvocationID
		}
	}

	if errorCode != "" {
		metadata[a2a.MetadataKeyErrorCode] = errorCode
	}

	return metadata
}

// dataPartType returns the kagent_type metadata value for a DataPart, or "".
func dataPartType(dp *a2atype.DataPart) string {
	if dp == nil || dp.Metadata == nil {
		return ""
	}
	t, _ := dp.Metadata[a2a.MetadataKeyType].(string)
	return t
}

// processLongRunningTool processes long-running tool metadata for an A2A part.
// This matches Python's _process_long_running_tool function.
func processLongRunningTool(a2aPart a2atype.Part, e interface{}) {
	longRunningToolIDs := extractLongRunningToolIDs(e)

	dataPart, ok := a2aPart.(*a2atype.DataPart)
	if !ok || dataPartType(dataPart) != a2a.A2ADataPartMetadataTypeFunctionCall {
		return
	}

	dataMap := dataPart.Data
	if dataMap == nil {
		return
	}

	id, _ := dataMap[a2a.PartKeyID].(string)
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
// errorCode and errorMessage are passed in to avoid redundant extraction.
// This matches Python's _create_error_status_event function.
func createErrorStatusEvent(evt interface{}, cc a2a.ConversionContext, errorCode, errorMessage string) *a2atype.TaskStatusUpdateEvent {
	metadata := getContextMetadata(evt, cc, errorCode)
	if errorCode != "" && errorMessage == "" {
		errorMessage = genai.GetErrorMessage(errorCode)
	}

	// Build message metadata with error code if present
	messageMetadata := make(map[string]interface{})
	if errorCode != "" {
		messageMetadata[a2a.MetadataKeyErrorCode] = errorCode
	}

	return &a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(cc.TaskID),
		ContextID: cc.ContextID,
		Metadata:  metadata,
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateFailed,
			Message: &a2atype.Message{
				ID:   uuid.New().String(),
				Role: a2atype.MessageRoleAgent,
				Parts: a2atype.ContentParts{
					a2atype.TextPart{Text: errorMessage},
				},
				Metadata: messageMetadata,
			},
			Timestamp: timePtr(time.Now()),
		},
		Final: false, // Not final - error events are not final (matching Python)
	}
}

// ConvertEventToA2AEvents converts runner events to A2A events. Uses only *adksession.Event (Google ADK) and RunnerErrorEvent.
// No internal event/content types: GenAI parts from ADK → A2A only.
func ConvertEventToA2AEvents(event interface{}, cc a2a.ConversionContext) []a2atype.Event {
	if adkEvent, ok := event.(*adksession.Event); ok {
		return convertADKEventToA2AEvents(adkEvent, cc)
	}

	// RunnerErrorEvent or any type with ErrorCode: only error path
	errorCode, errorMessage := extractErrorInfo(event)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		return []a2atype.Event{createErrorStatusEvent(event, cc, errorCode, errorMessage)}
	}
	// STOP with no content or unknown type: no events
	return nil
}

// convertADKEventToA2AEvents converts *adksession.Event to A2A events (like Python convert_event_to_a2a_events(adk_event)).
// Uses GenAIPartToA2APart for direct genai.Part → A2A Part conversion.
func convertADKEventToA2AEvents(adkEvent *adksession.Event, cc a2a.ConversionContext) []a2atype.Event {
	errorCode, errorMessage := extractErrorInfo(adkEvent)
	if errorCode != "" && !genai.IsNormalCompletion(errorCode) {
		return []a2atype.Event{createErrorStatusEvent(adkEvent, cc, errorCode, errorMessage)}
	}

	// Use LLMResponse.Content with fallback to Content so tool/progress events are not missed
	content := adkEventContent(adkEvent)
	if content == nil || len(content.Parts) == 0 {
		return nil
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
		return nil
	}

	messageMetadata := make(map[string]interface{})
	if adkEvent.Partial {
		messageMetadata[a2a.MetadataKeyAdkPartial] = true
	}
	message := &a2atype.Message{
		ID:       uuid.New().String(),
		Role:     a2atype.MessageRoleAgent,
		Parts:    a2aParts,
		Metadata: messageMetadata,
	}

	state := determineTaskState(a2aParts)
	metadata := getContextMetadata(adkEvent, cc, errorCode)

	return []a2atype.Event{&a2atype.TaskStatusUpdateEvent{
		TaskID:    a2atype.TaskID(cc.TaskID),
		ContextID: cc.ContextID,
		Status: a2atype.TaskStatus{
			State:     state,
			Timestamp: timePtr(time.Now()),
			Message:   message,
		},
		Metadata: metadata,
		Final:    false,
	}}
}

// determineTaskState inspects converted parts to decide the A2A task state.
// working by default; auth_required if any part is long-running function_call with name "request_euc";
// else input_required if any part is long-running function_call.
func determineTaskState(parts a2atype.ContentParts) a2atype.TaskState {
	state := a2atype.TaskStateWorking
	for _, part := range parts {
		dataPart, ok := part.(*a2atype.DataPart)
		if !ok {
			continue
		}
		isLongRunning, _ := dataPart.Metadata[a2a.MetadataKeyIsLongRunning].(bool)
		if dataPartType(dataPart) == a2a.A2ADataPartMetadataTypeFunctionCall && isLongRunning {
			if dataMap := dataPart.Data; dataMap != nil {
				if name, _ := dataMap[a2a.PartKeyName].(string); name == requestEucFunctionCallName {
					return a2atype.TaskStateAuthRequired
				}
				state = a2atype.TaskStateInputRequired
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
