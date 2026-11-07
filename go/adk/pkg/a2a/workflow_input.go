package a2a

import (
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/workflow"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// Keep the canonical call so resume reaches the workflow input dispatcher.
func buildWorkflowInputStatus(message *a2atype.Message, activated bool) (*a2atype.Message, error) {
	var public *a2atype.Message
	var pending int
	for _, part := range message.Parts {
		data := asDataPart(part)
		if data == nil ||
			part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey)] != A2ADataPartMetadataTypeFunctionCall ||
			part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataIsLongRunningKey)] != true {
			continue
		}
		pending++
		if data["name"] != workflow.WorkflowInputFunctionCallName {
			continue
		}
		if public != nil {
			return nil, fmt.Errorf("workflow pause requires exactly one input request")
		}
		converted, err := adka2a.ToGenAIPart(part)
		if err != nil {
			return nil, fmt.Errorf("failed to decode workflow input request: %w", err)
		}
		if converted == nil || converted.FunctionCall == nil || converted.FunctionCall.ID == "" {
			return nil, fmt.Errorf("workflow input request requires a correlation id")
		}
		call := converted.FunctionCall
		text, _ := call.Args["message"].(string)
		if text == "" {
			text = "Provide input to continue the workflow."
		}
		public = a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text), apia2a.NewToolCallPart(call.ID, call.Name, call.Args))
		public.TaskID, public.ContextID = message.TaskID, message.ContextID
		if activated {
			AttachHitlExtension(public, &apia2a.AskUserRequest{
				Type: HITLTypeAskUserRequest, ID: call.ID,
				Questions: []apia2a.HITLQuestion{{Question: text}},
			})
		}
	}
	if public != nil && pending != 1 {
		return nil, fmt.Errorf("workflow pause requires exactly one input request")
	}
	return public, nil
}

func isWorkflowInput(message *a2atype.Message, id string) bool {
	for _, part := range message.Parts {
		call, ok := apia2a.ParseToolActivity(part)
		if ok && call.Kind == apia2a.ToolCallKind && call.Name == workflow.WorkflowInputFunctionCallName && call.ID == id {
			return true
		}
	}
	return false
}

func workflowInputResponse(request *apia2a.AskUserRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	response := GetAskUserResponse(message)
	if response == nil || response.ID != request.ID || len(response.Answers) != 1 || len(response.Answers[0].Answer) != 1 {
		return nil, fmt.Errorf("workflow input response requires one correlated answer")
	}
	return []*a2atype.Part{apia2a.NewToolResultPart(request.ID, workflow.WorkflowInputFunctionCallName,
		map[string]any{"response": response.Answers[0].Answer[0]})}, nil
}
