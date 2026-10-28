package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

const (
	// HITLExtensionURI is the versioned A2A Message extension used at the HITL edge.
	HITLExtensionURI             = apia2a.HITLExtensionURI
	HITLTypeToolApprovalRequest  = apia2a.HITLTypeToolApprovalRequest
	HITLTypeAskUserRequest       = apia2a.HITLTypeAskUserRequest
	HITLTypeToolApprovalResponse = apia2a.HITLTypeToolApprovalResponse
	HITLTypeAskUserResponse      = apia2a.HITLTypeAskUserResponse

	// genericHITLText is the status text for a pause that names no tool.
	genericHITLText = "Human input is required before the agent can continue."
)

var hitlAgentExtension = apia2a.HITLExtension()

// HITLActivationInterceptor activates HITL when the client requested the exact
// versioned extension URI. The A2A transports then echo activated URIs.
func HITLActivationInterceptor() a2asrv.CallInterceptor {
	return &hitlActivationInterceptor{}
}

type hitlActivationInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

func (*hitlActivationInterceptor) Before(ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request) (context.Context, any, error) {
	if extensionRequested(callCtx, HITLExtensionURI) {
		callCtx.Extensions().Activate(&hitlAgentExtension)
	}
	return ctx, nil, nil
}

// extensionRequested reports whether the client listed uri in A2A-Extensions.
// The header is a comma-separated list and the transports keep a joined value
// as one entry, which a2asrv's exact match would miss.
func extensionRequested(callCtx *a2asrv.CallContext, uri string) bool {
	if callCtx == nil {
		return false
	}
	for _, value := range callCtx.Extensions().RequestedURIs() {
		for requested := range strings.SplitSeq(value, ",") {
			if strings.TrimSpace(requested) == uri {
				return true
			}
		}
	}
	return false
}

// HitlActivated reports whether HITL was negotiated for this server call.
func HitlActivated(ctx context.Context) bool {
	extensions, ok := a2asrv.ExtensionsFrom(ctx)
	return ok && extensions.Active(&hitlAgentExtension)
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
func decodeJSON[T proto.Message](raw map[string]any, wantType string, target T) T {
	var zero T
	if raw == nil || raw["type"] != wantType {
		return zero
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return zero
	}
	if err := protojson.Unmarshal(b, target); err != nil {
		return zero
	}
	return target
}

func GetToolApprovalRequest(message *a2atype.Message) *apia2a.ToolApprovalRequest {
	v := decodeJSON(rawHitlMap(message), HITLTypeToolApprovalRequest, &apia2a.ToolApprovalRequest{})
	if v == nil || len(v.Tools) == 0 {
		return nil
	}
	normalizeTools(v.Tools)
	return v
}

func GetAskUserRequest(message *a2atype.Message) *apia2a.AskUserRequest {
	v, _ := apia2a.ParseAskUserRequest(message)
	return v
}

func GetToolApprovalResponse(message *a2atype.Message) *apia2a.ToolApprovalResponse {
	v := decodeJSON(rawHitlMap(message), HITLTypeToolApprovalResponse, &apia2a.ToolApprovalResponse{})
	if v == nil || len(v.Approvals) == 0 {
		return nil
	}
	return v
}

func GetAskUserResponse(message *a2atype.Message) *apia2a.AskUserResponse {
	v, _ := apia2a.ParseAskUserResponse(message)
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
	var b []byte
	var err error
	if message, ok := payload.(proto.Message); ok {
		b, err = (protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}).Marshal(message)
	} else {
		b, err = json.Marshal(payload)
	}
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// normalizeTools ensures that tools have non-nil Args.
func normalizeTools(tools []*apia2a.HITLTool) {
	for i := range tools {
		if tools[i].Args == nil {
			tools[i].Args = &structpb.Struct{}
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
	ToolApprovalRequest  *apia2a.ToolApprovalRequest  `json:"tool_approval_request,omitempty"`
	AskUserRequest       *apia2a.AskUserRequest       `json:"ask_user_request,omitempty"`
	ToolApprovalResponse *apia2a.ToolApprovalResponse `json:"tool_approval_response,omitempty"`
	AskUserResponse      *apia2a.AskUserResponse      `json:"ask_user_response,omitempty"`
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
	state.ToolApprovalRequest = decodeJSON(req, HITLTypeToolApprovalRequest, &apia2a.ToolApprovalRequest{})
	if state.ToolApprovalRequest != nil {
		normalizeTools(state.ToolApprovalRequest.Tools)
	} else {
		state.AskUserRequest = decodeJSON(req, HITLTypeAskUserRequest, &apia2a.AskUserRequest{})
	}
	if state.ToolApprovalRequest == nil && state.AskUserRequest == nil {
		return nil
	}
	if resp, ok := raw["hitl_response"].(map[string]any); ok {
		state.ToolApprovalResponse = decodeJSON(resp, HITLTypeToolApprovalResponse, &apia2a.ToolApprovalResponse{})
		if state.ToolApprovalResponse == nil {
			state.AskUserResponse = decodeJSON(resp, HITLTypeAskUserResponse, &apia2a.AskUserResponse{})
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
func VisibleTools(approval *apia2a.ToolApprovalRequest, ask *apia2a.AskUserRequest) []*apia2a.HITLTool {
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
		return []*apia2a.HITLTool{{
			Id: ask.Id, CallId: ask.Id, Name: "ask_user",
			Args: questionArgs(ask.Questions),
		}}
	}
	return nil
}

func questionArgs(questions []*apia2a.HITLQuestion) *structpb.Struct {
	values := make([]*structpb.Value, 0, len(questions))
	for _, question := range questions {
		choices := make([]*structpb.Value, 0, len(question.Choices))
		for _, choice := range question.Choices {
			choices = append(choices, structpb.NewStringValue(choice))
		}
		values = append(values, structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{
			"question": structpb.NewStringValue(question.Question),
			"choices":  structpb.NewListValue(&structpb.ListValue{Values: choices}),
			"multiple": structpb.NewBoolValue(question.Multiple),
		}}))
	}
	return &structpb.Struct{Fields: map[string]*structpb.Value{"questions": structpb.NewListValue(&structpb.ListValue{Values: values})}}
}

// askUserQuestionText joins the question text from a typed Questions field, or "" if none carry text.
func askUserQuestionText(questions []*apia2a.HITLQuestion) string {
	texts := make([]string, 0, len(questions))
	for _, q := range questions {
		if q.Question != "" {
			texts = append(texts, q.Question)
		}
	}
	return strings.Join(texts, " ")
}

func RemoteHitlHint(state *RemoteHitlState) string {
	if state == nil {
		return "Remote agent requires human input before continuing."
	}
	// Read Questions directly: VisibleTools()'s nested Args round-trip through
	// JSON to []any, silently losing the question text in the nested case.
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

func (tool confirmationTool) asHitlTool() (*apia2a.HITLTool, error) {
	return apia2a.NewHITLTool(tool.approvalID, tool.callID, tool.name, tool.args)
}

// askUserQuestions returns the answerable questions of an ask_user pause, or nil.
// A pause qualifies when it holds exactly one ask_user call, and that call carries
// at least one question with text. A question without text cannot be rendered, and
// an answer for it cannot be correlated, so it is dropped.
func askUserQuestions(tools []*apia2a.HITLTool) []*apia2a.HITLQuestion {
	if len(tools) != 1 || tools[0].Name != "ask_user" {
		return nil
	}
	questions := publicAskUserQuestions(tools[0].GetArgs().AsMap()["questions"])
	answerable := make([]*apia2a.HITLQuestion, 0, len(questions))
	for _, question := range questions {
		if question.Question != "" {
			answerable = append(answerable, question)
		}
	}
	if len(answerable) == 0 {
		return nil
	}
	return answerable
}

// pendingQuestionText joins the questions an ask_user call is waiting on.
func pendingQuestionText(questions []*apia2a.HITLQuestion) string {
	texts := make([]string, 0, len(questions))
	for _, question := range questions {
		texts = append(texts, question.Question)
	}
	return strings.Join(texts, "; ")
}

// hitlStatusText renders a pause as one human-readable line. An ask_user pause speaks
// for itself; every other pause names its tools so no pending tool stays hidden.
func hitlStatusText(tools []*apia2a.HITLTool, hints []string) string {
	if questions := pendingQuestionText(askUserQuestions(tools)); questions != "" {
		return questions
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	switch {
	case len(hints) > 0 && len(names) > 0:
		return fmt.Sprintf("%s (%s)", strings.Join(hints, "; "), strings.Join(names, ", "))
	case len(hints) > 0:
		return strings.Join(hints, "; ")
	case len(names) > 0:
		return fmt.Sprintf("Approval is required for tool(s): %s", strings.Join(names, ", "))
	}
	return genericHITLText
}

// BuildHITLStatusMessage: ADK confirmation DataParts → public HITL Message extension.
func BuildHITLStatusMessage(message *a2atype.Message, activated bool) (*a2atype.Message, error) {
	if message == nil {
		return nil, nil
	}
	var tools []*apia2a.HITLTool
	var remote *RemoteHitlState
	var hints []string
	for _, part := range message.Parts {
		data := asDataPart(part)
		if data == nil || part.Metadata == nil {
			continue
		}
		partType := part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey)]
		isLongRunning := part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataIsLongRunningKey)]
		if partType != A2ADataPartMetadataTypeFunctionCall || isLongRunning != true || data["name"] != toolconfirmation.FunctionCallName {
			continue
		}
		tool := parseConfirmationTool(data)
		visible, err := tool.asHitlTool()
		if err != nil {
			return nil, err
		}
		tools = append(tools, visible)
		if tool.hint != "" {
			hints = append(hints, tool.hint)
		}
		if candidate := ParseRemoteHitlState(tool.payload); candidate != nil {
			remote = candidate
		}
	}
	if len(tools) == 0 {
		return message, nil
	}

	// The text part carries the same information as the typed payload, so a client
	// that did not activate the extension still learns what it is being asked.
	text := hitlStatusText(tools, hints)

	public := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(text))
	public.TaskID, public.ContextID = message.TaskID, message.ContextID
	if !activated {
		return public, nil
	}

	var nested *apia2a.NestedHITLRequest
	if remote != nil {
		nested = &apia2a.NestedHITLRequest{
			SubagentName: remote.SubagentName,
			TaskId:       remote.TaskID,
			ContextId:    remote.ContextID,
			Tools:        VisibleTools(remote.ToolApprovalRequest, remote.AskUserRequest),
		}
	}

	if remote != nil && remote.AskUserRequest != nil {
		return AttachHitlExtension(public, &apia2a.AskUserRequest{
			Type: HITLTypeAskUserRequest, Id: tools[0].Id,
			Questions: remote.AskUserRequest.Questions, Nested: nested,
		}), nil
	}
	// An ask_user call with no answerable question becomes an approval: an empty
	// question list gives a request that no response can satisfy, because resume
	// requires answers.
	if questions := askUserQuestions(tools); questions != nil {
		return AttachHitlExtension(public, &apia2a.AskUserRequest{
			Type: HITLTypeAskUserRequest, Id: tools[0].Id,
			Questions: questions,
		}), nil
	}
	return AttachHitlExtension(public, &apia2a.ToolApprovalRequest{
		Type: HITLTypeToolApprovalRequest, Hint: text, Tools: tools, Nested: nested,
	}), nil
}

func publicAskUserQuestions(value any) []*apia2a.HITLQuestion {
	items, ok := value.([]any)
	if !ok {
		if maps, typed := value.([]map[string]any); typed {
			items = make([]any, len(maps))
			for index := range maps {
				items[index] = maps[index]
			}
		}
	}
	questions := make([]*apia2a.HITLQuestion, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		question, _ := raw["question"].(string)
		multiple, _ := raw["multiple"].(bool)
		questions = append(questions, &apia2a.HITLQuestion{
			Question: question, Choices: stringSlice(raw["choices"]), Multiple: multiple,
		})
	}
	return questions
}

func stringSlice(value any) []string {
	if values, ok := value.([]string); ok {
		return append([]string(nil), values...)
	}
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
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

func processDirectAskUser(req *apia2a.AskUserRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	resp := GetAskUserResponse(message)
	if resp == nil || resp.Id != req.Id || len(resp.Answers) == 0 {
		return nil, fmt.Errorf("ask_user decision is missing approval correlation or answers")
	}
	part, err := buildConfirmationResponsePart(req.Id, true, map[string]any{"answers": resp.Answers})
	return []*a2atype.Part{part}, err
}

func processDirectApproval(req *apia2a.ToolApprovalRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	resp := GetToolApprovalResponse(message)
	if resp == nil {
		return nil, fmt.Errorf("tool approval request requires a tool approval response")
	}
	approvals := map[string]*apia2a.ToolApproval{}
	for _, a := range resp.Approvals {
		if _, dup := approvals[a.Id]; dup {
			return nil, fmt.Errorf("tool approval response contains duplicate id %s", a.Id)
		}
		approvals[a.Id] = a
	}
	var parts []*a2atype.Part
	for _, tool := range req.Tools {
		approval, ok := approvals[tool.Id]
		if !ok {
			return nil, fmt.Errorf("tool approval response is missing id %s", tool.Id)
		}
		var payload map[string]any
		if !approval.Approved && approval.RejectionReason != "" {
			payload = map[string]any{"rejection_reason": approval.RejectionReason}
		}
		part, err := buildConfirmationResponsePart(tool.Id, approval.Approved, payload)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
		delete(approvals, tool.Id)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("tool approval response contains unknown approval ids")
	}
	return parts, nil
}

// processNestedAskUser: client returns nested.tools[0].id; parent FunctionResponse uses request.id.
func processNestedAskUser(req *apia2a.AskUserRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	nested := req.Nested
	if nested.TaskId == "" || nested.SubagentName == "" {
		return nil, fmt.Errorf("nested HITL request is missing subagent task correlation")
	}
	if len(nested.Tools) != 1 || nested.Tools[0].Name != "ask_user" {
		return nil, fmt.Errorf("nested ask_user request must contain exactly one ask_user tool")
	}
	resp := GetAskUserResponse(message)
	childID := nested.Tools[0].Id
	if resp == nil || resp.Id != childID || len(resp.Answers) == 0 {
		return nil, fmt.Errorf("nested ask_user response has invalid correlation")
	}
	if req.Id == "" {
		return nil, fmt.Errorf("nested HITL request is missing parent approval correlation")
	}
	state := RemoteHitlState{
		TaskID: nested.TaskId, ContextID: nested.ContextId, SubagentName: nested.SubagentName,
		AskUserRequest:  &apia2a.AskUserRequest{Type: HITLTypeAskUserRequest, Id: childID, Questions: req.Questions},
		AskUserResponse: &apia2a.AskUserResponse{Type: HITLTypeAskUserResponse, Id: childID, Answers: resp.Answers},
	}
	part, err := buildConfirmationResponsePart(req.Id, true, state.ToMap())
	return []*a2atype.Part{part}, err
}

// processNestedApproval: client returns nested.tools IDs; parent FunctionResponse uses tools[0].id.
func processNestedApproval(req *apia2a.ToolApprovalRequest, message *a2atype.Message) ([]*a2atype.Part, error) {
	nested := req.Nested
	if nested.TaskId == "" || nested.SubagentName == "" {
		return nil, fmt.Errorf("nested HITL request is missing subagent task correlation")
	}
	if len(req.Tools) != 1 {
		return nil, fmt.Errorf("nested HITL request must contain exactly one parent tool")
	}
	resp := GetToolApprovalResponse(message)
	if resp == nil {
		return nil, fmt.Errorf("nested tool approval request requires a tool approval response")
	}
	approvals := map[string]*apia2a.ToolApproval{}
	for _, a := range resp.Approvals {
		if _, dup := approvals[a.Id]; dup {
			return nil, fmt.Errorf("tool approval response contains duplicate id %s", a.Id)
		}
		approvals[a.Id] = a
	}
	confirmed := true
	nestedApprovals := make([]*apia2a.ToolApproval, 0, len(nested.Tools))
	for _, tool := range nested.Tools {
		approval, ok := approvals[tool.Id]
		if !ok {
			return nil, fmt.Errorf("nested tool approval response is missing id %s", tool.Id)
		}
		if !approval.Approved {
			confirmed = false
		}
		nestedApprovals = append(nestedApprovals, approval)
		delete(approvals, tool.Id)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("nested tool approval response contains unknown approval ids")
	}
	state := RemoteHitlState{
		TaskID: nested.TaskId, ContextID: nested.ContextId, SubagentName: nested.SubagentName,
		ToolApprovalRequest:  &apia2a.ToolApprovalRequest{Type: HITLTypeToolApprovalRequest, Hint: req.Hint, Tools: nested.Tools},
		ToolApprovalResponse: &apia2a.ToolApprovalResponse{Type: HITLTypeToolApprovalResponse, Approvals: nestedApprovals},
	}
	part, err := buildConfirmationResponsePart(req.Tools[0].Id, confirmed, state.ToMap())
	return []*a2atype.Part{part}, err
}

func buildConfirmationResponsePart(fcID string, confirmed bool, payload map[string]any) (*a2atype.Part, error) {
	tc := toolconfirmation.ToolConfirmation{Confirmed: confirmed, Payload: payload}
	serialized, err := json.Marshal(tc)
	if err != nil {
		return nil, err
	}
	response, err := apia2a.NewToolResponse(map[string]any{"response": string(serialized)}, false)
	if err != nil {
		return nil, err
	}
	return apia2a.NewToolResultPart(fcID, toolconfirmation.FunctionCallName, response)
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}
