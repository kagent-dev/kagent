package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

const (
	// HITLExtensionURI is the versioned A2A Message extension used at the HITL edge.
	HITLExtensionURI             = "https://kagent.dev/extensions/hitl/v1"
	HITLTypeToolApprovalRequest  = "tool_approval_request"
	HITLTypeAskUserRequest       = "ask_user_request"
	HITLTypeToolApprovalResponse = "tool_approval_response"
	HITLTypeAskUserResponse      = "ask_user_response"
	KAgentMetadataKeyPrefix      = "kagent_"
)

var hitlAgentExtension = a2atype.AgentExtension{URI: HITLExtensionURI, Required: false}

// HITLActivationInterceptor activates HITL when the client requested the exact
// versioned extension URI. The A2A transports then echo activated URIs.
func HITLActivationInterceptor() a2asrv.CallInterceptor {
	return &hitlActivationInterceptor{}
}

type hitlActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (*hitlActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if callCtx != nil && callCtx.Extensions().Requested(&hitlAgentExtension) {
		callCtx.Extensions().Activate(&hitlAgentExtension)
	}
	return ctx, nil, nil
}

// HitlActivated reports whether HITL was negotiated for this server call.
func HitlActivated(ctx context.Context) bool {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	return ok && extensions.Active(&hitlAgentExtension)
}

// Public extension schema (same shapes as kagent.core.a2a._hitl).

type HitlTool struct {
	ID     string         `json:"id"`
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
}

type NestedHitlRequest struct {
	SubagentName string     `json:"subagent_name,omitempty"`
	TaskID       string     `json:"task_id,omitempty"`
	ContextID    string     `json:"context_id,omitempty"`
	Tools        []HitlTool `json:"tools"`
}

type ToolApprovalRequest struct {
	Type   string             `json:"type"`
	Hint   string             `json:"hint,omitempty"`
	Tools  []HitlTool         `json:"tools"`
	Nested *NestedHitlRequest `json:"nested,omitempty"`
}

type AskUserRequest struct {
	Type      string             `json:"type"`
	ID        string             `json:"id"`
	Questions []map[string]any   `json:"questions"`
	Nested    *NestedHitlRequest `json:"nested,omitempty"`
}

type ToolApproval struct {
	ID              string `json:"id"`
	Approved        bool   `json:"approved"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}

type ToolApprovalResponse struct {
	Type      string         `json:"type"`
	Approvals []ToolApproval `json:"approvals"`
}

type AskUserAnswer struct {
	Answer []string `json:"answer"`
}

type AskUserResponse struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Answers []AskUserAnswer `json:"answers,omitempty"`
}

// rawHitlMap reads the HITL extension metadata as a map[string]any.
func rawHitlMap(message *a2atype.Message) map[string]any {
	if message == nil || !slices.Contains(message.Extensions, HITLExtensionURI) || message.Metadata == nil {
		return nil
	}
	payload, _ := message.Metadata[HITLExtensionURI].(map[string]any)
	return payload
}

// decodeJSON maps a wire object into T. Returns nil if type mismatches or decode fails.
func decodeJSON[T any](raw map[string]any, wantType string) *T {
	if raw == nil || raw["type"] != wantType {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil
	}
	return &v
}

func GetToolApprovalRequest(message *a2atype.Message) *ToolApprovalRequest {
	v := decodeJSON[ToolApprovalRequest](rawHitlMap(message), HITLTypeToolApprovalRequest)
	if v == nil || len(v.Tools) == 0 {
		return nil
	}
	normalizeTools(v.Tools)
	return v
}

func GetAskUserRequest(message *a2atype.Message) *AskUserRequest {
	v := decodeJSON[AskUserRequest](rawHitlMap(message), HITLTypeAskUserRequest)
	if v == nil || v.ID == "" {
		return nil
	}
	return v
}

func GetToolApprovalResponse(message *a2atype.Message) *ToolApprovalResponse {
	v := decodeJSON[ToolApprovalResponse](rawHitlMap(message), HITLTypeToolApprovalResponse)
	if v == nil || len(v.Approvals) == 0 {
		return nil
	}
	return v
}

func GetAskUserResponse(message *a2atype.Message) *AskUserResponse {
	v := decodeJSON[AskUserResponse](rawHitlMap(message), HITLTypeAskUserResponse)
	if v == nil || v.ID == "" {
		return nil
	}
	return v
}

// IsHITLResponse reports whether a Message carries a valid HITL response.
func IsHITLResponse(message *a2atype.Message) bool {
	return GetToolApprovalResponse(message) != nil || GetAskUserResponse(message) != nil
}

// AttachHitlExtension writes a typed payload (or map) into Message metadata + extensions.
func AttachHitlExtension(message *a2atype.Message, payload any) *a2atype.Message {
	if message == nil || payload == nil {
		return message
	}
	raw, err := toJSONMap(payload)
	if err != nil || raw == nil {
		return message
	}
	if message.Metadata == nil {
		message.Metadata = make(map[string]any)
	}
	message.Metadata[HITLExtensionURI] = raw
	if !slices.Contains(message.Extensions, HITLExtensionURI) {
		message.Extensions = append(message.Extensions, HITLExtensionURI)
	}
	return message
}

// toJSONMap converts a payload to a map[string]any.
func toJSONMap(payload any) (map[string]any, error) {
	if m, ok := payload.(map[string]any); ok {
		return m, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetKAgentMetadataKey returns a metadata key prefixed with the Kagent metadata key prefix.
func GetKAgentMetadataKey(key string) string {
	return KAgentMetadataKeyPrefix + key
}

// normalizeTools ensures that tools have non-nil Args.
func normalizeTools(tools []HitlTool) {
	for i := range tools {
		if tools[i].Args == nil {
			tools[i].Args = map[string]any{}
		}
	}
}

// RemoteHitlState is stored in ToolConfirmation while a child A2A task is paused.
// Request/response are typed public payloads; JSON round-trips via standard encoding/json.
type RemoteHitlState struct {
	TaskID       string `json:"task_id"`
	ContextID    string `json:"context_id,omitempty"`
	SubagentName string `json:"subagent_name"`
	// Exactly one request / at most one response is set.
	ToolApprovalRequest  *ToolApprovalRequest  `json:"tool_approval_request,omitempty"`
	AskUserRequest       *AskUserRequest       `json:"ask_user_request,omitempty"`
	ToolApprovalResponse *ToolApprovalResponse `json:"tool_approval_response,omitempty"`
	AskUserResponse      *AskUserResponse      `json:"ask_user_response,omitempty"`
}

// ToMap serializes for ToolConfirmation.Payload. Also emits hitl_request/hitl_response
// so the wire shape matches Python RemoteHitlState.
func (s RemoteHitlState) ToMap() map[string]any {
	out := map[string]any{
		"task_id":       s.TaskID,
		"subagent_name": s.SubagentName,
	}
	if s.ContextID != "" {
		out["context_id"] = s.ContextID
	}
	if req := s.requestMap(); req != nil {
		out["hitl_request"] = req
	}
	if resp := s.responseMap(); resp != nil {
		out["hitl_response"] = resp
	}
	return out
}

func (s RemoteHitlState) requestMap() map[string]any {
	switch {
	case s.ToolApprovalRequest != nil:
		m, _ := toJSONMap(s.ToolApprovalRequest)
		return m
	case s.AskUserRequest != nil:
		m, _ := toJSONMap(s.AskUserRequest)
		return m
	default:
		return nil
	}
}

func (s RemoteHitlState) responseMap() map[string]any {
	switch {
	case s.ToolApprovalResponse != nil:
		m, _ := toJSONMap(s.ToolApprovalResponse)
		return m
	case s.AskUserResponse != nil:
		m, _ := toJSONMap(s.AskUserResponse)
		return m
	default:
		return nil
	}
}

// ParseRemoteHitlState decodes confirmation payload state (Python-compatible wire keys).
func ParseRemoteHitlState(raw map[string]any) *RemoteHitlState {
	if raw == nil {
		return nil
	}
	req, _ := raw["hitl_request"].(map[string]any)
	if req == nil {
		return nil
	}
	state := &RemoteHitlState{
		TaskID:       stringValue(raw["task_id"]),
		ContextID:    stringValue(raw["context_id"]),
		SubagentName: stringValue(raw["subagent_name"]),
	}
	state.ToolApprovalRequest = decodeJSON[ToolApprovalRequest](req, HITLTypeToolApprovalRequest)
	if state.ToolApprovalRequest != nil {
		normalizeTools(state.ToolApprovalRequest.Tools)
	} else {
		state.AskUserRequest = decodeJSON[AskUserRequest](req, HITLTypeAskUserRequest)
	}
	if state.ToolApprovalRequest == nil && state.AskUserRequest == nil {
		return nil
	}
	if resp, ok := raw["hitl_response"].(map[string]any); ok {
		state.ToolApprovalResponse = decodeJSON[ToolApprovalResponse](resp, HITLTypeToolApprovalResponse)
		if state.ToolApprovalResponse == nil {
			state.AskUserResponse = decodeJSON[AskUserResponse](resp, HITLTypeAskUserResponse)
		}
	}
	return state
}

func BuildRemoteHitlState(task *a2atype.Task, subagentName string) *RemoteHitlState {
	if task == nil || task.Status.Message == nil {
		return nil
	}
	state := &RemoteHitlState{
		TaskID:       string(task.ID),
		ContextID:    task.ContextID,
		SubagentName: subagentName,
	}
	if req := GetToolApprovalRequest(task.Status.Message); req != nil {
		state.ToolApprovalRequest = req
		return state
	}
	if req := GetAskUserRequest(task.Status.Message); req != nil {
		state.AskUserRequest = req
		return state
	}
	return nil
}

func (s *RemoteHitlState) HasResponse() bool {
	return s != nil && (s.ToolApprovalResponse != nil || s.AskUserResponse != nil)
}

func (s *RemoteHitlState) ResponsePayload() any {
	if s.ToolApprovalResponse != nil {
		return s.ToolApprovalResponse
	}
	return s.AskUserResponse
}

func (s *RemoteHitlState) ResponseType() string {
	if s.ToolApprovalResponse != nil {
		return HITLTypeToolApprovalResponse
	}
	if s.AskUserResponse != nil {
		return HITLTypeAskUserResponse
	}
	return ""
}

// VisibleTools returns the tools the human should decide on.
func VisibleTools(approval *ToolApprovalRequest, ask *AskUserRequest) []HitlTool {
	if approval != nil {
		if approval.Nested != nil {
			return approval.Nested.Tools
		}
		return approval.Tools
	}
	if ask != nil {
		if ask.Nested != nil {
			return ask.Nested.Tools
		}
		return []HitlTool{{
			ID: ask.ID, CallID: ask.ID, Name: "ask_user",
			Args: map[string]any{"questions": ask.Questions},
		}}
	}
	return nil
}

// askUserQuestionText extracts the human-readable question text(s) from an
// ask_user request's typed Questions field, or "" if none carry text. This is
// the content RemoteHitlHint would otherwise discard, surfacing only the
// tool name ("ask_user") instead of what is actually being asked.
func askUserQuestionText(questions []map[string]any) string {
	texts := make([]string, 0, len(questions))
	for _, q := range questions {
		if text, ok := q["question"].(string); ok && text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, " ")
}

func RemoteHitlHint(state *RemoteHitlState) string {
	if state == nil {
		return "Remote agent requires human input before continuing."
	}
	// AskUserRequest.Questions carries the real question text whether or not
	// Nested is set (see BuildHITLStatusMessage), so read it directly rather
	// than re-deriving from VisibleTools()'s nested HitlTool.Args: those args
	// round-trip through JSON into a plain map[string]any, which decodes
	// "questions" as []any rather than []map[string]any, silently losing the
	// question in the nested case.
	if state.AskUserRequest != nil {
		if q := askUserQuestionText(state.AskUserRequest.Questions); q != "" {
			return fmt.Sprintf("Remote agent '%s' asks: %s", state.SubagentName, q)
		}
	}
	tools := VisibleTools(state.ToolApprovalRequest, state.AskUserRequest)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	if len(names) > 0 {
		return fmt.Sprintf("Remote agent '%s' requires approval for tool(s): %s",
			state.SubagentName, strings.Join(names, ", "))
	}
	return fmt.Sprintf("Remote agent '%s' requires human input before continuing.", state.SubagentName)
}

func asDataPart(part *a2atype.Part) map[string]any {
	if part == nil {
		return nil
	}
	data, ok := part.Data().(map[string]any)
	if !ok {
		return nil
	}
	return data
}

// confirmationTool is a parsed ADK adk_request_confirmation DataPart.
type confirmationTool struct {
	approvalID string
	callID     string
	name       string
	args       map[string]any
	hint       string
	payload    map[string]any
}

// Converts a ADK confirmation DataPart to a confirmationTool, which can be used to build a A2A HITL Message extension payload.
func parseConfirmationTool(data map[string]any) confirmationTool {
	tool := confirmationTool{approvalID: stringValue(data["id"])}
	args, _ := data[PartKeyArgs].(map[string]any)
	if original, ok := args["originalFunctionCall"].(map[string]any); ok {
		tool.callID = stringValue(original["id"])
		tool.name = stringValue(original["name"])
		tool.args, _ = original["args"].(map[string]any)
	}
	if confirmation, ok := args["toolConfirmation"].(map[string]any); ok {
		tool.hint = stringValue(confirmation["hint"])
		tool.payload, _ = confirmation["payload"].(map[string]any)
	}
	if tool.callID == "" {
		tool.callID = tool.approvalID
	}
	if tool.name == "" {
		tool.name = "tool"
	}
	if tool.args == nil {
		tool.args = map[string]any{}
	}
	return tool
}

func (tool confirmationTool) asHitlTool() HitlTool {
	return HitlTool{ID: tool.approvalID, CallID: tool.callID, Name: tool.name, Args: tool.args}
}

// BuildHITLStatusMessage: ADK confirmation DataParts → public HITL Message extension.
func BuildHITLStatusMessage(message *a2atype.Message, activated bool) *a2atype.Message {
	if message == nil {
		return nil
	}
	var tools []HitlTool
	var remote *RemoteHitlState
	hint := "Human input is required before the agent can continue."
	for _, part := range message.Parts {
		data := asDataPart(part)
		if data == nil || part.Metadata == nil {
			continue
		}
		partType, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataTypeKey)
		isLongRunning, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataIsLongRunningKey)
		if partType != A2ADataPartMetadataTypeFunctionCall || isLongRunning != true || data["name"] != toolconfirmation.FunctionCallName {
			continue
		}
		tool := parseConfirmationTool(data)
		tools = append(tools, tool.asHitlTool())
		if tool.hint != "" {
			hint = tool.hint
		}
		if candidate := ParseRemoteHitlState(tool.payload); candidate != nil {
			remote = candidate
		}
	}
	if len(tools) == 0 {
		return message
	}

	public := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(hint))
	public.TaskID, public.ContextID = message.TaskID, message.ContextID
	if !activated {
		return public
	}

	var nested *NestedHitlRequest
	if remote != nil {
		nested = &NestedHitlRequest{
			SubagentName: remote.SubagentName,
			TaskID:       remote.TaskID,
			ContextID:    remote.ContextID,
			Tools:        VisibleTools(remote.ToolApprovalRequest, remote.AskUserRequest),
		}
	}

	if remote != nil && remote.AskUserRequest != nil {
		return AttachHitlExtension(public, &AskUserRequest{
			Type: HITLTypeAskUserRequest, ID: tools[0].ID,
			Questions: remote.AskUserRequest.Questions, Nested: nested,
		})
	}
	if len(tools) == 1 && tools[0].Name == "ask_user" {
		var questions []map[string]any
		if raw, ok := tools[0].Args["questions"].([]any); ok {
			for _, item := range raw {
				if m, ok := item.(map[string]any); ok {
					questions = append(questions, m)
				}
			}
		} else if typed, ok := tools[0].Args["questions"].([]map[string]any); ok {
			questions = typed
		}
		return AttachHitlExtension(public, &AskUserRequest{
			Type: HITLTypeAskUserRequest, ID: tools[0].ID, Questions: questions,
		})
	}
	return AttachHitlExtension(public, &ToolApprovalRequest{
		Type: HITLTypeToolApprovalRequest, Hint: hint, Tools: tools, Nested: nested,
	})
}

// BuildResumeHITLMessage: client HITL response + stored request → ADK FunctionResponse parts.
func BuildResumeHITLMessage(storedTask *a2atype.Task, incoming *a2atype.Message) (*a2atype.Message, error) {
	if !IsHITLResponse(incoming) {
		return nil, fmt.Errorf("message does not contain a HITL response")
	}
	if storedTask == nil || storedTask.Status.State != a2atype.TaskStateInputRequired {
		return nil, fmt.Errorf("HITL decision requires a stored input-required task")
	}
	approvalReq := GetToolApprovalRequest(storedTask.Status.Message)
	askReq := GetAskUserRequest(storedTask.Status.Message)
	if approvalReq == nil && askReq == nil {
		return nil, fmt.Errorf("stored input-required task has no HITL request")
	}

	var parts []*a2atype.Part
	var err error
	switch {
	case askReq != nil && askReq.Nested != nil:
		parts, err = processNestedAskUser(askReq, incoming)
	case approvalReq != nil && approvalReq.Nested != nil:
		parts, err = processNestedApproval(approvalReq, incoming)
	case askReq != nil:
		parts, err = processDirectAskUser(askReq, incoming)
	default:
		parts, err = processDirectApproval(approvalReq, incoming)
	}
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("stored HITL request contains no approvals")
	}
	return a2atype.NewMessage(a2atype.MessageRoleUser, parts...), nil
}

func processDirectAskUser(req *AskUserRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	resp := GetAskUserResponse(message)
	if resp == nil || resp.ID != req.ID || len(resp.Answers) == 0 {
		return nil, fmt.Errorf("ask_user decision is missing approval correlation or answers")
	}
	return []*a2atype.Part{buildConfirmationResponsePart(req.ID, true, map[string]any{
		"answers": resp.Answers,
	})}, nil
}

func processDirectApproval(req *ToolApprovalRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	resp := GetToolApprovalResponse(message)
	if resp == nil {
		return nil, fmt.Errorf("tool approval request requires a tool approval response")
	}
	approvals := map[string]ToolApproval{}
	for _, a := range resp.Approvals {
		if _, dup := approvals[a.ID]; dup {
			return nil, fmt.Errorf("tool approval response contains duplicate id %s", a.ID)
		}
		approvals[a.ID] = a
	}
	var parts []*a2atype.Part
	for _, tool := range req.Tools {
		approval, ok := approvals[tool.ID]
		if !ok {
			return nil, fmt.Errorf("tool approval response is missing id %s", tool.ID)
		}
		var payload map[string]any
		if !approval.Approved && approval.RejectionReason != "" {
			payload = map[string]any{"rejection_reason": approval.RejectionReason}
		}
		parts = append(parts, buildConfirmationResponsePart(tool.ID, approval.Approved, payload))
		delete(approvals, tool.ID)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("tool approval response contains unknown approval ids")
	}
	return parts, nil
}

// processNestedAskUser: client returns nested.tools[0].id; parent FunctionResponse uses request.id.
func processNestedAskUser(req *AskUserRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	nested := req.Nested
	if nested.TaskID == "" || nested.SubagentName == "" {
		return nil, fmt.Errorf("nested HITL request is missing subagent task correlation")
	}
	if len(nested.Tools) != 1 || nested.Tools[0].Name != "ask_user" {
		return nil, fmt.Errorf("nested ask_user request must contain exactly one ask_user tool")
	}
	resp := GetAskUserResponse(message)
	childID := nested.Tools[0].ID
	if resp == nil || resp.ID != childID || len(resp.Answers) == 0 {
		return nil, fmt.Errorf("nested ask_user response has invalid correlation")
	}
	if req.ID == "" {
		return nil, fmt.Errorf("nested HITL request is missing parent approval correlation")
	}
	state := RemoteHitlState{
		TaskID: nested.TaskID, ContextID: nested.ContextID, SubagentName: nested.SubagentName,
		AskUserRequest:  &AskUserRequest{Type: HITLTypeAskUserRequest, ID: childID, Questions: req.Questions},
		AskUserResponse: &AskUserResponse{Type: HITLTypeAskUserResponse, ID: childID, Answers: resp.Answers},
	}
	return []*a2atype.Part{buildConfirmationResponsePart(req.ID, true, state.ToMap())}, nil
}

// processNestedApproval: client returns nested.tools IDs; parent FunctionResponse uses tools[0].id.
func processNestedApproval(req *ToolApprovalRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	nested := req.Nested
	if nested.TaskID == "" || nested.SubagentName == "" {
		return nil, fmt.Errorf("nested HITL request is missing subagent task correlation")
	}
	if len(req.Tools) != 1 {
		return nil, fmt.Errorf("nested HITL request must contain exactly one parent tool")
	}
	resp := GetToolApprovalResponse(message)
	if resp == nil {
		return nil, fmt.Errorf("nested tool approval request requires a tool approval response")
	}
	approvals := map[string]ToolApproval{}
	for _, a := range resp.Approvals {
		if _, dup := approvals[a.ID]; dup {
			return nil, fmt.Errorf("tool approval response contains duplicate id %s", a.ID)
		}
		approvals[a.ID] = a
	}
	confirmed := true
	nestedApprovals := make([]ToolApproval, 0, len(nested.Tools))
	for _, tool := range nested.Tools {
		approval, ok := approvals[tool.ID]
		if !ok {
			return nil, fmt.Errorf("nested tool approval response is missing id %s", tool.ID)
		}
		if !approval.Approved {
			confirmed = false
		}
		nestedApprovals = append(nestedApprovals, approval)
		delete(approvals, tool.ID)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("nested tool approval response contains unknown approval ids")
	}
	state := RemoteHitlState{
		TaskID: nested.TaskID, ContextID: nested.ContextID, SubagentName: nested.SubagentName,
		ToolApprovalRequest:  &ToolApprovalRequest{Type: HITLTypeToolApprovalRequest, Hint: req.Hint, Tools: nested.Tools},
		ToolApprovalResponse: &ToolApprovalResponse{Type: HITLTypeToolApprovalResponse, Approvals: nestedApprovals},
	}
	return []*a2atype.Part{buildConfirmationResponsePart(req.Tools[0].ID, confirmed, state.ToMap())}, nil
}

func buildConfirmationResponsePart(fcID string, confirmed bool, payload map[string]any) *a2atype.Part {
	tc := toolconfirmation.ToolConfirmation{Confirmed: confirmed, Payload: payload}
	serialized, _ := json.Marshal(tc)
	p := a2atype.NewDataPart(map[string]any{
		PartKeyName:     toolconfirmation.FunctionCallName,
		PartKeyID:       fcID,
		PartKeyResponse: map[string]any{"response": string(serialized)},
	})
	p.Metadata = map[string]any{
		GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
	}
	return p
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
