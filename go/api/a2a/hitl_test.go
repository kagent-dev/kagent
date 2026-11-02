package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/require"
)

func TestParseToolApprovalResponseAcceptsEveryPendingDecision(t *testing.T) {
	message := &a2atype.Message{}
	err := AttachHITL(message, ToolApprovalResponse{
		Type: HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{
			{ID: "one", Approved: true},
			{ID: "two", Approved: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	response, err := ParseToolApprovalResponse(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Approvals) != 2 {
		t.Fatalf("got %d decisions, want 2", len(response.Approvals))
	}
}

func TestValidateToolApprovalResponseRejectsReasonOnApproval(t *testing.T) {
	request := &ToolApprovalRequest{Tools: []HITLTool{{ID: "one"}}}
	response := &ToolApprovalResponse{Approvals: []ToolApproval{{
		ID: "one", Approved: true, RejectionReason: "contradictory",
	}}}

	if err := ValidateToolApprovalResponse(request, response); err == nil {
		t.Fatal("ValidateToolApprovalResponse() accepted a rejection reason on approval")
	}
}

func TestValidateToolApprovalResponseUsesNestedToolIDs(t *testing.T) {
	request := &ToolApprovalRequest{
		Tools:  []HITLTool{{ID: "parent"}},
		Nested: &NestedHITLRequest{Tools: []HITLTool{{ID: "child-one"}, {ID: "child-two"}}},
	}
	response := &ToolApprovalResponse{Approvals: []ToolApproval{
		{ID: "child-one", Approved: true},
		{ID: "child-two", Approved: false},
	}}

	if err := ValidateToolApprovalResponse(request, response); err != nil {
		t.Fatalf("ValidateToolApprovalResponse() rejected nested decisions: %v", err)
	}
}

func TestParseAndValidateAskUserResponse(t *testing.T) {
	requestMessage := &a2atype.Message{}
	if err := AttachHITL(requestMessage, AskUserRequest{
		Type: HITLTypeAskUserRequest,
		ID:   "question-1",
		Questions: []HITLQuestion{
			{Question: "Which namespace?"},
			{Question: "Which cluster?"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	request, err := ParseAskUserRequest(requestMessage)
	if err != nil {
		t.Fatal(err)
	}

	responseMessage := &a2atype.Message{}
	if err := AttachHITL(responseMessage, AskUserResponse{
		Type: HITLTypeAskUserResponse,
		ID:   "question-1",
		Answers: []AskUserAnswer{
			{Answer: []string{"default"}},
			{Answer: []string{"production"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	response, err := ParseAskUserResponse(responseMessage)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAskUserResponse(request, response); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAskUserResponseRequiresEveryAnswer(t *testing.T) {
	request := &AskUserRequest{ID: "question-1", Questions: []HITLQuestion{{Question: "One?"}, {Question: "Two?"}}}
	response := &AskUserResponse{ID: "question-1", Answers: []AskUserAnswer{{Answer: []string{"one"}}}}
	if err := ValidateAskUserResponse(request, response); err == nil {
		t.Fatal("ValidateAskUserResponse() accepted an incomplete response")
	}
}

func TestValidateAskUserResponseSelections(t *testing.T) {
	for _, test := range []struct {
		name     string
		multiple bool
		answer   []string
		wantErr  bool
	}{
		{name: "empty single", wantErr: true},
		{name: "empty multiple", multiple: true, wantErr: true},
		{name: "too many single", answer: []string{"one", "two"}, wantErr: true},
		{name: "free text", answer: []string{"custom answer"}},
		{name: "multiple selections", multiple: true, answer: []string{"one", "two"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &AskUserRequest{ID: "question", Questions: []HITLQuestion{{Question: "Choose", Multiple: test.multiple}}}
			response := &AskUserResponse{ID: request.ID, Answers: []AskUserAnswer{{Answer: test.answer}}}
			err := ValidateAskUserResponse(request, response)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
