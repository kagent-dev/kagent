package a2a

import (
	"encoding/json"
	"fmt"
	"slices"

	"buf.build/go/protovalidate"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

const (
	// HITLExtensionURI identifies the versioned kagent HITL A2A extension.
	HITLExtensionURI             = "https://kagent.dev/extensions/hitl/v1"
	HITLTypeToolApprovalRequest  = "tool_approval_request"
	HITLTypeAskUserRequest       = "ask_user_request"
	HITLTypeToolApprovalResponse = "tool_approval_response"
	HITLTypeAskUserResponse      = "ask_user_response"
)

// HITLExtension returns the optional Agent Card capability implemented by
// managed kagent harnesses.
func HITLExtension() a2atype.AgentExtension {
	return a2atype.AgentExtension{
		URI: HITLExtensionURI, Description: "Human approval and user input", Required: false,
	}
}

type HITLTool = apiv1.HitlTool

// NewHITLTool encodes JSON-compatible SDK arguments in the public payload.
func NewHITLTool(id, callID, name string, args map[string]any) (*HITLTool, error) {
	if args == nil {
		args = map[string]any{}
	}
	value, err := toolActivityValue(args)
	if err != nil {
		return nil, fmt.Errorf("encode tool approval arguments: %w", err)
	}
	return &HITLTool{Id: id, CallId: callID, Name: name, Args: value.GetStructValue()}, nil
}

type NestedHITLRequest = apiv1.NestedHitlRequest

type ToolApprovalRequest = apiv1.ToolApprovalRequest

type ToolApproval = apiv1.ToolApproval

type ToolApprovalResponse = apiv1.ToolApprovalResponse

type HITLQuestion = apiv1.HitlQuestion

type AskUserRequest = apiv1.AskUserRequest

type AskUserAnswer = apiv1.AskUserAnswer

type AskUserResponse = apiv1.AskUserResponse

// AttachHITL adds a typed HITL payload and extension declaration to a message.
func AttachHITL(message *a2atype.Message, payload proto.Message) error {
	encoded, err := (protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}).Marshal(payload)
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
	var response ToolApprovalResponse
	if err := decodeHITLPayload(raw, &response); err != nil {
		return nil, fmt.Errorf("decode tool approval response: %w", err)
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
	var response AskUserResponse
	if err := decodeHITLPayload(raw, &response); err != nil {
		return nil, fmt.Errorf("decode ask-user response: %w", err)
	}
	return &response, nil
}

// ParseToolApprovalRequest decodes a tool approval request when one is present.
func ParseToolApprovalRequest(message *a2atype.Message) (*ToolApprovalRequest, error) {
	raw, ok := hitlPayload(message)
	if !ok || raw["type"] != HITLTypeToolApprovalRequest {
		return nil, nil
	}
	var request ToolApprovalRequest
	if err := decodeHITLPayload(raw, &request); err != nil {
		return nil, fmt.Errorf("decode tool approval request: %w", err)
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
	return &request, nil
}

// ValidateToolApprovalResponse verifies that every request ID is decided exactly once.
func ValidateToolApprovalResponse(request *ToolApprovalRequest, response *ToolApprovalResponse) error {
	if request == nil || response == nil {
		return fmt.Errorf("tool approval response must decide every requested tool")
	}
	tools := request.Tools
	if request.Nested != nil {
		tools = request.Nested.Tools
	}
	if len(tools) != len(response.Approvals) {
		return fmt.Errorf("tool approval response must decide every requested tool")
	}
	want := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if err := protovalidate.Validate(tool); err != nil {
			return fmt.Errorf("invalid requested tool: %w", err)
		}
		want[tool.GetId()] = struct{}{}
	}
	for _, approval := range response.Approvals {
		if _, ok := want[approval.GetId()]; !ok {
			return fmt.Errorf("tool approval response contains unknown or duplicate ID %q", approval.GetId())
		}
		if err := protovalidate.Validate(approval); err != nil {
			return fmt.Errorf("invalid tool approval: %w", err)
		}
		delete(want, approval.GetId())
	}
	if len(want) != 0 {
		return fmt.Errorf("tool approval response omits a requested tool")
	}
	return nil
}

// ValidateAskUserResponse verifies correlation and positional completeness.
func ValidateAskUserResponse(request *AskUserRequest, response *AskUserResponse) error {
	if request == nil || response == nil || request.Id != response.Id {
		return fmt.Errorf("ask-user response has invalid correlation")
	}
	if len(response.Answers) != len(request.Questions) {
		return fmt.Errorf("ask-user response must answer every question")
	}
	for i, answer := range response.Answers {
		if err := protovalidate.Validate(answer); err != nil {
			return fmt.Errorf("invalid answer for question %d: %w", i, err)
		}
		if !request.Questions[i].GetMultiple() && len(answer.GetAnswer()) != 1 {
			return fmt.Errorf("ask-user response has invalid selection for question %d", i)
		}
	}
	return nil
}

func decodeHITLPayload(raw map[string]any, target proto.Message) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := protojson.Unmarshal(encoded, target); err != nil {
		return err
	}
	return protovalidate.Validate(target)
}

func hitlPayload(message *a2atype.Message) (map[string]any, bool) {
	if message == nil || !slices.Contains(message.Extensions, HITLExtensionURI) || message.Metadata == nil {
		return nil, false
	}
	raw, ok := message.Metadata[HITLExtensionURI].(map[string]any)
	return raw, ok
}
