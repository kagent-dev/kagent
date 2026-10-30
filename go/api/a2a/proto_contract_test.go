package a2a_test

import (
	"testing"

	"buf.build/go/protovalidate"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestInteractionContractValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		message proto.Message
		valid   bool
	}{
		{"tool result", &apiv1.ToolResponse{Outcome: &apiv1.ToolResponse_Output{Output: structpb.NewNullValue()}}, true},
		{"missing tool outcome", &apiv1.ToolResponse{}, false},
		{"approval", &apiv1.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []*apiv1.ToolApproval{{Id: "call", Approved: true}}}, true},
		{"rejection", &apiv1.ToolApproval{Id: "call", RejectionReason: "read only"}, true},
		{"contradictory decision", &apiv1.ToolApproval{Id: "call", Approved: true, RejectionReason: "read only"}, false},
		{"missing decision id", &apiv1.ToolApproval{}, false},
		{"wrong discriminator", &apiv1.ToolApprovalResponse{Type: "unknown", Approvals: []*apiv1.ToolApproval{{Id: "call"}}}, false},
		{"no decisions", &apiv1.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse}, false},
		{"no questions", &apiv1.AskUserRequest{Type: apia2a.HITLTypeAskUserRequest, Id: "question"}, false},
		{"empty question", &apiv1.HitlQuestion{}, false},
		{"empty selection", &apiv1.AskUserAnswer{}, false},
		{"optional message id", &apiv1.InvokeSessionInput{SessionId: new("session"), Message: new("hello")}, true},
		{"missing message", &apiv1.InvokeSessionInput{SessionId: new("session")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := protovalidate.Validate(test.message)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGeneratedHITLRetainsWireNamesAndNegativeDecision(t *testing.T) {
	message := a2atype.NewMessage(a2atype.MessageRoleUser)
	require.NoError(t, apia2a.AttachHITL(message, &apiv1.ToolApprovalResponse{
		Type:      apia2a.HITLTypeToolApprovalResponse,
		Approvals: []*apiv1.ToolApproval{{Id: "child", RejectionReason: "production"}},
	}))
	raw := message.Metadata[apia2a.HITLExtensionURI].(map[string]any)
	decision := raw["approvals"].([]any)[0].(map[string]any)
	require.Equal(t, false, decision["approved"])
	require.Equal(t, "production", decision["rejection_reason"])
	response, err := apia2a.ParseToolApprovalResponse(message)
	require.NoError(t, err)
	require.NoError(t, apia2a.ValidateToolApprovalResponse(&apiv1.ToolApprovalRequest{
		Tools:  []*apiv1.HitlTool{{Id: "parent"}},
		Nested: &apiv1.NestedHitlRequest{Tools: []*apiv1.HitlTool{{Id: "child"}}},
	}, response))
	var nested apiv1.NestedHitlRequest
	require.NoError(t, protojson.Unmarshal([]byte(`{"subagent_name":"helper","task_id":"task","context_id":"session"}`), &nested))
	require.Equal(t, "task", nested.TaskId)
	require.Equal(t, "session", nested.ContextId)
}

func TestMalformedInteractionValuesFailAtTheBoundary(t *testing.T) {
	_, err := apia2a.NewToolCallPart("call", "lookup", "", map[string]any{"unsupported": make(chan string)})
	require.Error(t, err)
	message := a2atype.NewMessage(a2atype.MessageRoleUser)
	message.Extensions = []string{apia2a.HITLExtensionURI}
	message.Metadata = map[string]any{apia2a.HITLExtensionURI: map[string]any{
		"type": apia2a.HITLTypeToolApprovalResponse, "approvals": []any{nil},
	}}
	_, err = apia2a.ParseToolApprovalResponse(message)
	require.Error(t, err)
}
