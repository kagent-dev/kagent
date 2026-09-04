package a2a

import (
	"encoding/json"
	"fmt"
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

const (
	// HITLExtensionURI identifies the versioned kagent HITL A2A extension.
	HITLExtensionURI             = "https://kagent.dev/extensions/hitl/v1"
	HITLTypeToolApprovalRequest  = "tool_approval_request"
	HITLTypeAskUserRequest       = "ask_user_request"
	HITLTypeToolApprovalResponse = "tool_approval_response"
	HITLTypeAskUserResponse      = "ask_user_response"
)

// HITLTool describes one tool invocation awaiting a human decision.
type HITLTool struct {
	ID     string         `json:"id"`
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

// NestedHITLRequest identifies tool approvals propagated from a child agent.
type NestedHITLRequest struct {
	SubagentName string     `json:"subagent_name,omitempty"`
	TaskID       string     `json:"task_id,omitempty"`
	ContextID    string     `json:"context_id,omitempty"`
	Tools        []HITLTool `json:"tools"`
}

// ToolApprovalRequest is the public request payload carried by input-required.
type ToolApprovalRequest struct {
	Type   string             `json:"type"`
	Hint   string             `json:"hint,omitempty"`
	Tools  []HITLTool         `json:"tools"`
	Nested *NestedHITLRequest `json:"nested,omitempty"`
}

// ToolApproval is one human decision for a requested tool invocation.
type ToolApproval struct {
	ID              string `json:"id"`
	Approved        bool   `json:"approved"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}

// ToolApprovalResponse is the public response payload for tool approvals.
type ToolApprovalResponse struct {
	Type      string         `json:"type"`
	Approvals []ToolApproval `json:"approvals"`
}

// HITLQuestion is one question supported by the public HITL extension.
type HITLQuestion struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
	Multiple bool     `json:"multiple"`
}

// AskUserRequest is the public request payload for one or more questions.
type AskUserRequest struct {
	Type      string             `json:"type"`
	ID        string             `json:"id"`
	Questions []HITLQuestion     `json:"questions"`
	Nested    *NestedHITLRequest `json:"nested,omitempty"`
}

// AskUserAnswer contains the selections for one question, in request order.
type AskUserAnswer struct {
	Answer []string `json:"answer"`
}

// AskUserResponse answers the questions from one correlated request.
type AskUserResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Answers []AskUserAnswer `json:"answers,omitempty"`
}

// AttachHITL adds a typed HITL payload and extension declaration to a message.
func AttachHITL(message *a2atype.Message, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return err
	}
	if message.Metadata == nil {
		message.Metadata = map[string]any{}
	}
	message.Metadata[HITLExtensionURI] = raw
	if !slices.Contains(message.Extensions, HITLExtensionURI) {
		message.Extensions = append(message.Extensions, HITLExtensionURI)
	}
	return nil
}

// ParseToolApprovalResponse decodes a non-empty set of tool decisions. The
// caller validates that it decides the pending request exactly once per tool.
func ParseToolApprovalResponse(message *a2atype.Message) (*ToolApprovalResponse, error) {
	if message == nil || !slices.Contains(message.Extensions, HITLExtensionURI) {
		return nil, fmt.Errorf("tool approval response must declare %s", HITLExtensionURI)
	}
	raw, ok := message.Metadata[HITLExtensionURI].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool approval response payload is required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var response ToolApprovalResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		return nil, fmt.Errorf("decode tool approval response: %w", err)
	}
	if response.Type != HITLTypeToolApprovalResponse || len(response.Approvals) == 0 {
		return nil, fmt.Errorf("at least one tool approval decision is required")
	}
	for _, approval := range response.Approvals {
		if approval.ID == "" {
			return nil, fmt.Errorf("tool approval decision ID is required")
		}
	}
	return &response, nil
}

// ParseAskUserResponse decodes an ask-user response. Cross-message correlation
// and answer completeness are checked by ValidateAskUserResponse.
func ParseAskUserResponse(message *a2atype.Message) (*AskUserResponse, error) {
	raw, ok := hitlPayload(message)
	if !ok {
		return nil, fmt.Errorf("ask-user response must declare %s", HITLExtensionURI)
	}
	if raw["type"] != HITLTypeAskUserResponse {
		return nil, fmt.Errorf("ask-user response payload is required")
	}
	var response AskUserResponse
	if err := decodeHITLPayload(raw, &response); err != nil {
		return nil, fmt.Errorf("decode ask-user response: %w", err)
	}
	if response.ID == "" {
		return nil, fmt.Errorf("ask-user response ID is required")
	}
	return &response, nil
}

// ParseToolApprovalRequest decodes a tool approval request when one is present.
func ParseToolApprovalRequest(message *a2atype.Message) (*ToolApprovalRequest, error) {
	raw, ok := hitlPayload(message)
	if !ok || raw["type"] != HITLTypeToolApprovalRequest {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var request ToolApprovalRequest
	if err := json.Unmarshal(encoded, &request); err != nil {
		return nil, fmt.Errorf("decode tool approval request: %w", err)
	}
	if len(request.Tools) == 0 {
		return nil, fmt.Errorf("tool approval request has no tools")
	}
	return &request, nil
}

// ParseAskUserRequest decodes an ask-user request when one is present.
func ParseAskUserRequest(message *a2atype.Message) (*AskUserRequest, error) {
	raw, ok := hitlPayload(message)
	if !ok || raw["type"] != HITLTypeAskUserRequest {
		return nil, nil
	}
	var request AskUserRequest
	if err := decodeHITLPayload(raw, &request); err != nil {
		return nil, fmt.Errorf("decode ask-user request: %w", err)
	}
	if request.ID == "" {
		return nil, fmt.Errorf("ask-user request ID is required")
	}
	return &request, nil
}

// ValidateToolApprovalResponse verifies that every request ID is decided exactly once.
func ValidateToolApprovalResponse(request *ToolApprovalRequest, response *ToolApprovalResponse) error {
	if request == nil || response == nil || len(request.Tools) != len(response.Approvals) {
		return fmt.Errorf("tool approval response must decide every requested tool")
	}
	want := make(map[string]struct{}, len(request.Tools))
	for _, tool := range request.Tools {
		if tool.ID == "" {
			return fmt.Errorf("tool approval request contains an empty ID")
		}
		want[tool.ID] = struct{}{}
	}
	for _, approval := range response.Approvals {
		if _, ok := want[approval.ID]; !ok {
			return fmt.Errorf("tool approval response contains unknown or duplicate ID %q", approval.ID)
		}
		if approval.Approved && approval.RejectionReason != "" {
			return fmt.Errorf("approved tool %q cannot have a rejection reason", approval.ID)
		}
		delete(want, approval.ID)
	}
	if len(want) != 0 {
		return fmt.Errorf("tool approval response omits a requested tool")
	}
	return nil
}

// ValidateAskUserResponse verifies correlation and positional completeness.
func ValidateAskUserResponse(request *AskUserRequest, response *AskUserResponse) error {
	if request == nil || response == nil || request.ID != response.ID {
		return fmt.Errorf("ask-user response has invalid correlation")
	}
	if len(response.Answers) == 0 {
		return fmt.Errorf("ask-user response contains no answers")
	}
	if len(response.Answers) != len(request.Questions) {
		return fmt.Errorf("ask-user response must answer every question")
	}
	return nil
}

func decodeHITLPayload(raw map[string]any, target any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

func hitlPayload(message *a2atype.Message) (map[string]any, bool) {
	if message == nil || !slices.Contains(message.Extensions, HITLExtensionURI) || message.Metadata == nil {
		return nil, false
	}
	raw, ok := message.Metadata[HITLExtensionURI].(map[string]any)
	return raw, ok
}
