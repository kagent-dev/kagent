package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

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

// GetHitlPayload returns a Message extension payload only when the Message
// explicitly declares the exact HITL extension URI.
func GetHitlPayload(message *a2atype.Message) map[string]any {
	if message == nil || !containsString(message.Extensions, HITLExtensionURI) || message.Metadata == nil {
		return nil
	}
	payload, _ := message.Metadata[HITLExtensionURI].(map[string]any)
	return payload
}

// AttachHitlExtension attaches the A2A Protocol §4.6 Message extension payload.
// https://a2a-protocol.org/latest/specification/#46-extensions
func AttachHitlExtension(message *a2atype.Message, payload map[string]any) *a2atype.Message {
	if message == nil {
		return nil
	}
	if message.Metadata == nil {
		message.Metadata = make(map[string]any)
	}
	message.Metadata[HITLExtensionURI] = payload
	if !containsString(message.Extensions, HITLExtensionURI) {
		message.Extensions = append(message.Extensions, HITLExtensionURI)
	}
	return message
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// OriginalFunctionCall is the original tool call inside an adk_request_confirmation event.
type OriginalFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
}

// HitlPartInfo is a structured representation of one adk_request_confirmation DataPart.
// Port of _hitl_utils.py:HitlPartInfo.
type HitlPartInfo struct {
	Name                 string               `json:"name"`
	ID                   string               `json:"id,omitempty"`
	OriginalFunctionCall OriginalFunctionCall `json:"originalFunctionCall"`
}

// AskUserAnswer is one positional answer returned from the ask_user tool.
type AskUserAnswer struct {
	Answer []string `json:"answer"`
}

// HitlConfirmationPayload is the structured payload stored in ToolConfirmation.
// It is used both for direct HITL metadata (rejection reasons, ask_user answers)
// and for subagent resume state (task/context IDs, hitl_parts, batch decisions).
type HitlConfirmationPayload struct {
	TaskID          string           `json:"task_id,omitempty"`
	ContextID       string           `json:"context_id,omitempty"`
	SubagentName    string           `json:"subagent_name,omitempty"`
	HitlParts       []HitlPartInfo   `json:"hitl_parts,omitempty"`
	Approvals       []ApprovalResult `json:"approvals,omitempty"`
	RejectionReason string           `json:"rejection_reason,omitempty"`
	Answers         []AskUserAnswer  `json:"answers,omitempty"`
}

// HasSubagentHitl reports whether the payload carries nested HITL state from a subagent.
func (p HitlConfirmationPayload) HasSubagentHitl() bool {
	return len(p.HitlParts) > 0
}

// ToMap converts the structured payload back into the wire-format map expected
// by ADK ToolConfirmation payloads.
func (p HitlConfirmationPayload) ToMap() map[string]any {
	result := make(map[string]any)
	if p.TaskID != "" {
		result["task_id"] = p.TaskID
	}
	if p.ContextID != "" {
		result["context_id"] = p.ContextID
	}
	if p.SubagentName != "" {
		result["subagent_name"] = p.SubagentName
	}
	if len(p.HitlParts) > 0 {
		hitlParts := make([]map[string]any, 0, len(p.HitlParts))
		for _, hp := range p.HitlParts {
			hitlParts = append(hitlParts, map[string]any{
				"name": hp.Name,
				"id":   hp.ID,
				"originalFunctionCall": map[string]any{
					"name": hp.OriginalFunctionCall.Name,
					"args": hp.OriginalFunctionCall.Args,
					"id":   hp.OriginalFunctionCall.ID,
				},
			})
		}
		result["hitl_parts"] = hitlParts
	}
	if len(p.Approvals) > 0 {
		approvals := make([]map[string]any, 0, len(p.Approvals))
		for _, approval := range p.Approvals {
			item := map[string]any{"id": approval.ID, "approved": approval.Approved}
			if approval.RejectionReason != "" {
				item["rejection_reason"] = approval.RejectionReason
			}
			approvals = append(approvals, item)
		}
		result["approvals"] = approvals
	}
	if p.RejectionReason != "" {
		result["rejection_reason"] = p.RejectionReason
	}
	if len(p.Answers) > 0 {
		answers := make([]map[string]any, 0, len(p.Answers))
		for _, answer := range p.Answers {
			answers = append(answers, map[string]any{"answer": answer.Answer})
		}
		result["answers"] = answers
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ParseHitlConfirmationPayload decodes a raw ToolConfirmation payload map into
// its structured form.
func ParseHitlConfirmationPayload(raw map[string]any) HitlConfirmationPayload {
	if len(raw) == 0 {
		return HitlConfirmationPayload{}
	}

	var payload HitlConfirmationPayload
	payload.TaskID, _ = raw["task_id"].(string)
	payload.ContextID, _ = raw["context_id"].(string)
	payload.SubagentName, _ = raw["subagent_name"].(string)
	payload.RejectionReason, _ = raw["rejection_reason"].(string)
	payload.Approvals = parseApprovalResultsValue(raw["approvals"])
	payload.Answers = parseAskUserAnswersValue(raw["answers"])
	payload.HitlParts = parseHitlPartsValue(raw["hitl_parts"])

	return payload
}

// GetKAgentMetadataKey returns the prefixed metadata key.
func GetKAgentMetadataKey(key string) string {
	return KAgentMetadataKeyPrefix + key
}

// asDataPart extracts map-backed data content from an A2A part.
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

// IsHITLResponse reports whether a Message carries a valid HITL response type.
func IsHITLResponse(message *a2atype.Message) bool {
	payload := GetHitlPayload(message)
	return payload != nil && (payload["type"] == HITLTypeToolApprovalResponse || payload["type"] == HITLTypeAskUserResponse)
}

// ApprovalResult is one independently resolved public tool approval.
type ApprovalResult struct {
	ID              string `json:"id"`
	Approved        bool   `json:"approved"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}

// ExtractApprovalResults extracts the flat approval list keyed by opaque ID.
func ExtractApprovalResults(message *a2atype.Message) (map[string]ApprovalResult, error) {
	payload := GetHitlPayload(message)
	if payload == nil || payload["type"] != HITLTypeToolApprovalResponse {
		return nil, fmt.Errorf("message does not contain a tool approval response")
	}
	results := make(map[string]ApprovalResult)
	for _, raw := range anySlice(payload["approvals"]) {
		approval, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool approval response contains an invalid approval")
		}
		id := stringValue(approval["id"])
		approved, hasApproved := approval["approved"].(bool)
		if id == "" || !hasApproved {
			return nil, fmt.Errorf("tool approval response is missing id or approved")
		}
		if _, exists := results[id]; exists {
			return nil, fmt.Errorf("tool approval response contains duplicate id %s", id)
		}
		results[id] = ApprovalResult{ID: id, Approved: approved, RejectionReason: stringValue(approval["rejection_reason"])}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("tool approval response contains no approvals")
	}
	return results, nil
}

// ExtractAskUserAnswersFromMessage extracts ask-user answers from a response message.
func ExtractAskUserAnswersFromMessage(message *a2atype.Message) []map[string]any {
	payload := GetHitlPayload(message)
	if payload == nil || payload["type"] != HITLTypeAskUserResponse {
		return nil
	}
	answers := parseAskUserAnswersValue(payload["answers"])
	if len(answers) > 0 {
		result := make([]map[string]any, 0, len(answers))
		for _, answer := range answers {
			answerValues := make([]any, 0, len(answer.Answer))
			for _, value := range answer.Answer {
				answerValues = append(answerValues, value)
			}
			result = append(result, map[string]any{"answer": answerValues})
		}
		return result
	}
	return nil
}

// HitlPartInfoFromDataPartData constructs a HitlPartInfo from a raw DataPart.Data map.
func HitlPartInfoFromDataPartData(data map[string]any) HitlPartInfo {
	name, _ := data["name"].(string)
	if name == "" {
		name = toolconfirmation.FunctionCallName
	}
	id, _ := data["id"].(string)
	var ofc OriginalFunctionCall
	if ofcRaw, ok := data["originalFunctionCall"].(map[string]any); ok {
		ofc.Name, _ = ofcRaw["name"].(string)
		ofc.ID, _ = ofcRaw["id"].(string)
		if argsInner, ok := ofcRaw["args"].(map[string]any); ok {
			ofc.Args = argsInner
		}
	} else if args, ok := data["args"].(map[string]any); ok {
		if ofcRaw, ok := args["originalFunctionCall"].(map[string]any); ok {
			ofc.Name, _ = ofcRaw["name"].(string)
			ofc.ID, _ = ofcRaw["id"].(string)
			if argsInner, ok := ofcRaw["args"].(map[string]any); ok {
				ofc.Args = argsInner
			}
		}
	}
	return HitlPartInfo{Name: name, ID: id, OriginalFunctionCall: ofc}
}

// ExtractHitlInfoFromParts scans A2A content parts for adk_request_confirmation DataParts.
func ExtractHitlInfoFromParts(parts a2atype.ContentParts) []HitlPartInfo {
	var result []HitlPartInfo
	for _, part := range parts {
		dpData := asDataPart(part)
		if dpData == nil || part.Metadata == nil {
			continue
		}
		partType, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataTypeKey)
		isLR, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataIsLongRunningKey)
		if partType == A2ADataPartMetadataTypeFunctionCall && isLR == true {
			result = append(result, HitlPartInfoFromDataPartData(dpData))
		}
	}
	return result
}

// ExtractHitlInfoFromMessage translates the framework-neutral extension from
// a child agent into the ADK-internal confirmation state used by remote tools.
func ExtractHitlInfoFromMessage(message *a2atype.Message) []HitlPartInfo {
	payload := GetHitlPayload(message)
	if payload == nil {
		return nil
	}
	var tools any
	switch payload["type"] {
	case HITLTypeToolApprovalRequest:
		if nested, ok := payload["nested"].(map[string]any); ok {
			tools = nested["tools"]
		}
		if tools == nil {
			tools = payload["tools"]
		}
	case HITLTypeAskUserRequest:
		if nested, ok := payload["nested"].(map[string]any); ok {
			tools = nested["tools"]
		}
		if tools == nil {
			tools = []any{map[string]any{
				"id":      payload["id"],
				"call_id": payload["id"],
				"name":    "ask_user",
				"args":    map[string]any{"questions": payload["questions"]},
			}}
		}
	default:
		return nil
	}

	var result []HitlPartInfo
	for _, raw := range anySlice(tools) {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if name == "" {
			continue
		}
		callID, _ := tool["call_id"].(string)
		approvalID, _ := tool["id"].(string)
		args, _ := tool["args"].(map[string]any)
		result = append(result, HitlPartInfo{
			Name:                 toolconfirmation.FunctionCallName,
			ID:                   approvalID,
			OriginalFunctionCall: OriginalFunctionCall{Name: name, ID: callID, Args: args},
		})
	}
	return result
}

// BuildHITLStatusMessage converts the upstream Go ADK input-required message
// into the public A2A HITL extension. ADK confirmation parts never cross the
// A2A boundary. Without activation, only a human-readable pause is exposed.
func BuildHITLStatusMessage(message *a2atype.Message, activated bool) *a2atype.Message {
	if message == nil {
		return nil
	}
	type confirmationRequest struct {
		info    HitlPartInfo
		payload map[string]any
	}
	var requests []confirmationRequest
	hint := "Human input is required before the agent can continue."
	for _, part := range message.Parts {
		data := asDataPart(part)
		if data == nil || part.Metadata == nil {
			continue
		}
		partType, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataTypeKey)
		isLongRunning, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataIsLongRunningKey)
		if partType != A2ADataPartMetadataTypeFunctionCall || isLongRunning != true {
			continue
		}
		request := confirmationRequest{info: HitlPartInfoFromDataPartData(data)}
		args, _ := data[PartKeyArgs].(map[string]any)
		if confirmation, ok := args["toolConfirmation"].(map[string]any); ok {
			if value, _ := confirmation["hint"].(string); value != "" {
				hint = value
			}
			request.payload, _ = confirmation["payload"].(map[string]any)
		}
		requests = append(requests, request)
	}
	if len(requests) == 0 {
		return message
	}

	tools := make([]any, 0, len(requests))
	var nested map[string]any
	for _, request := range requests {
		payload := ParseHitlConfirmationPayload(request.payload)
		if payload.HasSubagentHitl() {
			nestedTools := make([]any, 0, len(payload.HitlParts))
			for _, child := range payload.HitlParts {
				nestedTools = append(nestedTools, hitlToolMap(child.OriginalFunctionCall, child.ID))
			}
			nested = map[string]any{
				"subagent_name": payload.SubagentName,
				"task_id":       payload.TaskID,
				"context_id":    payload.ContextID,
				"tools":         nestedTools,
			}
		}
		tools = append(tools, hitlToolMap(request.info.OriginalFunctionCall, request.info.ID))
	}

	public := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart(hint))
	public.TaskID, public.ContextID = message.TaskID, message.ContextID
	if !activated {
		return public
	}
	var askUserArgs map[string]any
	if len(requests) == 1 && requests[0].info.OriginalFunctionCall.Name == "ask_user" {
		askUserArgs = requests[0].info.OriginalFunctionCall.Args
	} else if nested != nil {
		nestedTools := anySlice(nested["tools"])
		if len(nestedTools) == 1 {
			if tool, ok := nestedTools[0].(map[string]any); ok && tool["name"] == "ask_user" {
				askUserArgs, _ = tool["args"].(map[string]any)
			}
		}
	}
	if askUserArgs != nil {
		payload := map[string]any{
			"type":      HITLTypeAskUserRequest,
			"id":        requests[0].info.ID,
			"questions": askUserArgs["questions"],
		}
		if nested != nil {
			payload["nested"] = nested
		}
		return AttachHitlExtension(public, payload)
	}
	payload := map[string]any{
		"type": HITLTypeToolApprovalRequest,
		"hint": hint, "tools": tools,
	}
	if nested != nil {
		payload["nested"] = nested
	}
	return AttachHitlExtension(public, payload)
}

func hitlToolMap(call OriginalFunctionCall, approvalID string) map[string]any {
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}
	return map[string]any{"id": approvalID, "call_id": call.ID, "name": call.Name, "args": args}
}

// BuildResumeHITLMessage converts an inbound user HITL response into the
// adk_request_confirmation FunctionResponse message expected by the Go ADK
// executor. Correlation comes from the stored public A2A pause; upstream ADK
// remains responsible for matching these responses to its session state.
func BuildResumeHITLMessage(storedTask *a2atype.Task, incoming *a2atype.Message) (*a2atype.Message, error) {
	if !IsHITLResponse(incoming) {
		return nil, fmt.Errorf("message does not contain a HITL response")
	}
	if storedTask == nil || storedTask.Status.State != a2atype.TaskStateInputRequired {
		return nil, fmt.Errorf("HITL decision requires a stored input-required task")
	}
	request := GetHitlPayload(storedTask.Status.Message)
	if request == nil {
		return nil, fmt.Errorf("stored input-required task has no HITL request")
	}
	responseParts, err := processStoredHITLRequest(request, incoming)
	if err != nil {
		return nil, err
	}
	if len(responseParts) == 0 {
		return nil, fmt.Errorf("stored HITL request contains no approvals")
	}
	return a2atype.NewMessage(a2atype.MessageRoleUser, responseParts...), nil
}

func processStoredHITLRequest(request map[string]any, message *a2atype.Message) ([]*a2atype.Part, error) {
	if nested, ok := request["nested"].(map[string]any); ok {
		return processNestedHITLRequest(request, nested, message)
	}
	if request["type"] == HITLTypeAskUserRequest {
		response := GetHitlPayload(message)
		if response == nil || response["type"] != HITLTypeAskUserResponse {
			return nil, fmt.Errorf("ask_user request requires an ask_user response")
		}
		approvalID := stringValue(request["id"])
		answers := parseAskUserAnswersValue(ExtractAskUserAnswersFromMessage(message))
		if approvalID == "" || stringValue(response["id"]) != approvalID || len(answers) == 0 {
			return nil, fmt.Errorf("ask_user decision is missing approval correlation or answers")
		}
		payload := HitlConfirmationPayload{Answers: answers}
		return []*a2atype.Part{buildConfirmationResponsePart(approvalID, true, payload.ToMap())}, nil
	}

	tools := anySlice(request["tools"])
	if len(tools) == 0 {
		return nil, fmt.Errorf("tool approval request contains no tools")
	}
	approvals, err := ExtractApprovalResults(message)
	if err != nil {
		return nil, err
	}
	var parts []*a2atype.Part
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool approval request contains an invalid tool")
		}
		approvalID := stringValue(tool["id"])
		if approvalID == "" {
			return nil, fmt.Errorf("tool approval is missing id")
		}
		approval, exists := approvals[approvalID]
		if !exists {
			return nil, fmt.Errorf("tool approval response is missing id %s", approvalID)
		}
		payload := make(map[string]any)
		if !approval.Approved && approval.RejectionReason != "" {
			payload["rejection_reason"] = approval.RejectionReason
		}
		parts = append(parts, buildConfirmationResponsePart(approvalID, approval.Approved, nilIfEmpty(payload)))
		delete(approvals, approvalID)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("tool approval response contains unknown approval ids")
	}
	return parts, nil
}

func processNestedHITLRequest(request, nested map[string]any, message *a2atype.Message) ([]*a2atype.Part, error) {
	outerTools := anySlice(request["tools"])
	approvalID := stringValue(request["id"])
	if len(outerTools) == 1 {
		if outer, ok := outerTools[0].(map[string]any); ok {
			approvalID = stringValue(outer["id"])
		}
	}
	if approvalID == "" {
		return nil, fmt.Errorf("nested HITL request is missing parent approval correlation")
	}
	payload := HitlConfirmationPayload{
		TaskID:       stringValue(nested["task_id"]),
		ContextID:    stringValue(nested["context_id"]),
		SubagentName: stringValue(nested["subagent_name"]),
	}
	nestedTools := anySlice(nested["tools"])
	for _, raw := range nestedTools {
		tool, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("nested HITL request contains an invalid tool")
		}
		payload.HitlParts = append(payload.HitlParts, HitlPartInfo{
			Name: toolconfirmation.FunctionCallName,
			ID:   stringValue(tool["id"]),
			OriginalFunctionCall: OriginalFunctionCall{
				ID: stringValue(tool["call_id"]), Name: stringValue(tool["name"]), Args: cloneMap(tool["args"]),
			},
		})
	}
	if request["type"] == HITLTypeAskUserRequest {
		response := GetHitlPayload(message)
		if response == nil || response["type"] != HITLTypeAskUserResponse || len(payload.HitlParts) != 1 || stringValue(response["id"]) != payload.HitlParts[0].ID {
			return nil, fmt.Errorf("nested ask_user response has invalid correlation")
		}
		payload.Answers = parseAskUserAnswersValue(ExtractAskUserAnswersFromMessage(message))
		if len(payload.Answers) == 0 {
			return nil, fmt.Errorf("nested ask_user decision contains no answers")
		}
		return []*a2atype.Part{buildConfirmationResponsePart(approvalID, true, payload.ToMap())}, nil
	}

	approvals, err := ExtractApprovalResults(message)
	if err != nil {
		return nil, err
	}
	confirmed := true
	for _, tool := range payload.HitlParts {
		approval, exists := approvals[tool.ID]
		if !exists {
			return nil, fmt.Errorf("nested tool approval response is missing id %s", tool.ID)
		}
		if !approval.Approved {
			confirmed = false
		}
		payload.Approvals = append(payload.Approvals, approval)
		delete(approvals, tool.ID)
	}
	if len(approvals) > 0 {
		return nil, fmt.Errorf("nested tool approval response contains unknown approval ids")
	}
	return []*a2atype.Part{buildConfirmationResponsePart(approvalID, confirmed, payload.ToMap())}, nil
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func cloneMap(value any) map[string]any {
	raw, _ := value.(map[string]any)
	result := make(map[string]any, len(raw))
	maps.Copy(result, raw)
	return result
}

func nilIfEmpty(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// buildConfirmationResponsePart builds the A2A DataPart for a ToolConfirmation FunctionResponse.
func buildConfirmationResponsePart(fcID string, confirmed bool, payload map[string]any) *a2atype.Part {
	tc := toolconfirmation.ToolConfirmation{
		Confirmed: confirmed,
		Payload:   payload,
	}
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

func parseApprovalResultsValue(raw any) []ApprovalResult {
	switch typed := raw.(type) {
	case []ApprovalResult:
		if len(typed) == 0 {
			return nil
		}
		return append([]ApprovalResult(nil), typed...)
	case []any:
		result := make([]ApprovalResult, 0, len(typed))
		for _, item := range typed {
			value, ok := item.(map[string]any)
			if !ok {
				continue
			}
			approved, hasApproved := value["approved"].(bool)
			id := stringValue(value["id"])
			if id == "" || !hasApproved {
				continue
			}
			result = append(result, ApprovalResult{ID: id, Approved: approved, RejectionReason: stringValue(value["rejection_reason"])})
		}
		return result
	default:
		return nil
	}
}

func parseAskUserAnswersValue(raw any) []AskUserAnswer {
	switch typed := raw.(type) {
	case []AskUserAnswer:
		if len(typed) == 0 {
			return nil
		}
		return append([]AskUserAnswer(nil), typed...)
	case []map[string]any:
		if len(typed) == 0 {
			return nil
		}
		result := make([]AskUserAnswer, 0, len(typed))
		for _, item := range typed {
			answer := parseAnswerStrings(item["answer"])
			result = append(result, AskUserAnswer{Answer: answer})
		}
		return result
	case []any:
		if len(typed) == 0 {
			return nil
		}
		result := make([]AskUserAnswer, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				result = append(result, AskUserAnswer{Answer: parseAnswerStrings(m["answer"])})
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		return nil
	}
}

func parseAnswerStrings(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func anySlice(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = typed[i]
		}
		return result
	default:
		return nil
	}
}

func parseHitlPartsValue(raw any) []HitlPartInfo {
	switch typed := raw.(type) {
	case []HitlPartInfo:
		if len(typed) == 0 {
			return nil
		}
		return append([]HitlPartInfo(nil), typed...)
	case []map[string]any:
		if len(typed) == 0 {
			return nil
		}
		result := make([]HitlPartInfo, 0, len(typed))
		for _, item := range typed {
			result = append(result, HitlPartInfoFromDataPartData(item))
		}
		return result
	case []any:
		if len(typed) == 0 {
			return nil
		}
		result := make([]HitlPartInfo, 0, len(typed))
		for _, item := range typed {
			if part, ok := item.(map[string]any); ok {
				result = append(result, HitlPartInfoFromDataPartData(part))
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		return nil
	}
}
