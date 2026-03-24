package a2a

import (
	"encoding/json"
	"maps"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/adk/tool/toolconfirmation"
)

const (
	KAgentMetadataKeyPrefix = "kagent_"

	KAgentHitlInterruptTypeToolApproval = "tool_approval"
	KAgentHitlDecisionTypeKey           = "decision_type"
	KAgentHitlDecisionTypeApprove       = "approve"
	KAgentHitlDecisionTypeReject        = "reject"
)

// DecisionType represents a HITL decision.
type DecisionType string

const (
	DecisionApprove DecisionType = "approve"
	DecisionReject  DecisionType = "reject"
	DecisionBatch   DecisionType = "batch"
)

// ToolApprovalRequest is kept for logging / HitlPartInfo compatibility.
type ToolApprovalRequest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	ID   string         `json:"id,omitempty"`
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

// PendingConfirmation holds info about an unresponded adk_request_confirmation.
type PendingConfirmation struct {
	OriginalID      string         // originalFunctionCall.id
	OriginalPayload map[string]any // toolConfirmation.payload (may be nil)
}

// AskUserAnswer is one positional answer returned from the ask_user tool.
type AskUserAnswer struct {
	Answer []string `json:"answer"`
}

// HitlConfirmationPayload is the structured payload stored in ToolConfirmation.
// It is used both for direct HITL metadata (rejection reasons, ask_user answers)
// and for subagent resume state (task/context IDs, hitl_parts, batch decisions).
type HitlConfirmationPayload struct {
	TaskID           string                  `json:"task_id,omitempty"`
	ContextID        string                  `json:"context_id,omitempty"`
	SubagentName     string                  `json:"subagent_name,omitempty"`
	HitlParts        []HitlPartInfo          `json:"hitl_parts,omitempty"`
	BatchDecisions   map[string]DecisionType `json:"batch_decisions,omitempty"`
	RejectionReasons map[string]string       `json:"rejection_reasons,omitempty"`
	RejectionReason  string                  `json:"rejection_reason,omitempty"`
	Answers          []AskUserAnswer         `json:"answers,omitempty"`
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
	if len(p.BatchDecisions) > 0 {
		batch := make(map[string]any, len(p.BatchDecisions))
		for id, decision := range p.BatchDecisions {
			batch[id] = string(decision)
		}
		result["batch_decisions"] = batch
	}
	if len(p.RejectionReasons) > 0 {
		reasons := make(map[string]any, len(p.RejectionReasons))
		for id, reason := range p.RejectionReasons {
			reasons[id] = reason
		}
		result["rejection_reasons"] = reasons
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
	payload.BatchDecisions = parseDecisionMap(raw["batch_decisions"])
	payload.RejectionReasons = parseStringMap(raw["rejection_reasons"])
	payload.Answers = parseAskUserAnswersValue(raw["answers"])
	payload.HitlParts = parseHitlPartsValue(raw["hitl_parts"])

	return payload
}

// GetKAgentMetadataKey returns the prefixed metadata key.
func GetKAgentMetadataKey(key string) string {
	return KAgentMetadataKeyPrefix + key
}

// asDataPart extracts a *DataPart from an A2A Part, handling both value and
// pointer types. The a2a-go library may deserialize parts as either
// a2atype.DataPart (value) or *a2atype.DataPart (pointer).
func asDataPart(part a2atype.Part) *a2atype.DataPart {
	switch p := part.(type) {
	case *a2atype.DataPart:
		return p
	case a2atype.DataPart:
		return &p
	}
	return nil
}

// ExtractDecisionFromMessage extracts a decision from an A2A message.
// Only structured DataPart decisions are supported (no text keyword matching).
// Port of _hitl_utils.py:extract_decision_from_message().
func ExtractDecisionFromMessage(message *a2atype.Message) DecisionType {
	if message == nil || len(message.Parts) == 0 {
		return ""
	}
	for _, part := range message.Parts {
		if dataPart := asDataPart(part); dataPart != nil {
			if decision, ok := dataPart.Data[KAgentHitlDecisionTypeKey].(string); ok {
				switch decision {
				case KAgentHitlDecisionTypeApprove:
					return DecisionApprove
				case KAgentHitlDecisionTypeReject:
					return DecisionReject
				case string(DecisionBatch):
					return DecisionBatch
				}
			}
		}
	}
	return ""
}

// ExtractBatchDecisionsFromMessage extracts per-tool decisions from a batch decision message.
// Returns map[originalToolCallID]DecisionType.
// Port of _hitl_utils.py:extract_batch_decisions_from_message().
func ExtractBatchDecisionsFromMessage(message *a2atype.Message) map[string]DecisionType {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		dp := asDataPart(part)
		if dp == nil || dp.Data[KAgentHitlDecisionTypeKey] != string(DecisionBatch) {
			continue
		}
		return parseDecisionMap(dp.Data[KAgentHitlDecisionsKey])
	}
	return nil
}

// ExtractRejectionReasonsFromMessage extracts rejection reasons.
// For uniform reject: returns {"*": reason}. For batch: returns {toolCallID: reason}.
// Port of _hitl_utils.py:extract_rejection_reasons_from_message().
func ExtractRejectionReasonsFromMessage(message *a2atype.Message) map[string]string {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		dp := asDataPart(part)
		if dp == nil {
			continue
		}
		decision, _ := dp.Data[KAgentHitlDecisionTypeKey].(string)
		if decision == string(DecisionBatch) {
			return parseStringMap(dp.Data[KAgentHitlRejectionReasonsKey])
		} else if decision == KAgentHitlDecisionTypeReject {
			if reason, _ := dp.Data["rejection_reason"].(string); reason != "" {
				return map[string]string{"*": reason}
			}
		}
	}
	return nil
}

// ExtractAskUserAnswersFromMessage extracts ask-user answers from a decision message.
// Port of _hitl_utils.py:extract_ask_user_answers_from_message().
func ExtractAskUserAnswersFromMessage(message *a2atype.Message) []map[string]any {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		dp := asDataPart(part)
		if dp == nil {
			continue
		}
		answers := parseAskUserAnswersValue(dp.Data[KAgentAskUserAnswersKey])
		if len(answers) == 0 {
			continue
		}
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
// Port of HitlPartInfo.from_data_part_data().
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
// Port of extract_hitl_info_from_task() but operating on parts directly.
func ExtractHitlInfoFromParts(parts a2atype.ContentParts) []HitlPartInfo {
	var result []HitlPartInfo
	for _, part := range parts {
		dp := asDataPart(part)
		if dp == nil || dp.Metadata == nil {
			continue
		}
		partType, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataTypeKey)
		isLR, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataIsLongRunningKey)
		if partType == A2ADataPartMetadataTypeFunctionCall && isLR == true {
			result = append(result, HitlPartInfoFromDataPartData(dp.Data))
		}
	}
	return result
}

// BuildConfirmationPayload merges the original request_confirmation payload with extra data.
// Port of _agent_executor.py:_build_confirmation_payload().
func BuildConfirmationPayload(originalPayload, extra map[string]any) map[string]any {
	if len(originalPayload) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]any)
	maps.Copy(merged, originalPayload)
	maps.Copy(merged, extra)
	return merged
}

// ExtractPendingConfirmationsFromParts reconstructs pending confirmation state
// from an input_required task status message.
func ExtractPendingConfirmationsFromParts(parts a2atype.ContentParts) map[string]PendingConfirmation {
	pending := make(map[string]PendingConfirmation)
	for _, part := range parts {
		dp := asDataPart(part)
		if dp == nil || dp.Metadata == nil || dp.Data == nil {
			continue
		}

		partType, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataTypeKey)
		isLongRunning, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataIsLongRunningKey)
		if partType != A2ADataPartMetadataTypeFunctionCall || isLongRunning != true {
			continue
		}

		name, _ := dp.Data[PartKeyName].(string)
		if name != toolconfirmation.FunctionCallName {
			continue
		}

		confirmationID, _ := dp.Data[PartKeyID].(string)
		if confirmationID == "" {
			continue
		}

		info := HitlPartInfoFromDataPartData(dp.Data)
		var originalPayload map[string]any
		if args, ok := dp.Data[PartKeyArgs].(map[string]any); ok {
			if tc, ok := args["toolConfirmation"].(map[string]any); ok {
				if payload, ok := tc["payload"].(map[string]any); ok {
					originalPayload = payload
				}
			}
		}

		pending[confirmationID] = PendingConfirmation{
			OriginalID:      info.OriginalFunctionCall.ID,
			OriginalPayload: originalPayload,
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return pending
}

// BuildResumeHITLMessage converts an inbound user HITL decision into the
// adk_request_confirmation FunctionResponse message expected by the Go ADK
// executor for a stored input_required task.
func BuildResumeHITLMessage(storedTask *a2atype.Task, incoming *a2atype.Message) *a2atype.Message {
	decision := ExtractDecisionFromMessage(incoming)
	if decision == "" {
		return nil
	}
	if storedTask == nil || storedTask.Status.State != a2atype.TaskStateInputRequired || storedTask.Status.Message == nil {
		return nil
	}

	pending := ExtractPendingConfirmationsFromParts(storedTask.Status.Message.Parts)
	if len(pending) == 0 {
		return nil
	}

	responseParts := ProcessHitlDecision(pending, decision, incoming)
	if len(responseParts) == 0 {
		return nil
	}

	return a2atype.NewMessage(a2atype.MessageRoleUser, responseParts...)
}

// ProcessHitlDecision processes a HITL decision and returns A2A DataParts
// representing FunctionResponse(s) with ToolConfirmation payloads.
// Port of _agent_executor.py:_process_hitl_decision().
func ProcessHitlDecision(
	pending map[string]PendingConfirmation,
	decision DecisionType,
	message *a2atype.Message,
) []a2atype.Part {
	if len(pending) == 0 {
		return nil
	}

	// Ask-user answers take priority.
	if askUserAnswers := parseAskUserAnswersValue(extractMessageField(message, KAgentAskUserAnswersKey)); len(askUserAnswers) > 0 {
		var parts []a2atype.Part
		for fcID, pc := range pending {
			payload := ParseHitlConfirmationPayload(pc.OriginalPayload)
			payload.Answers = askUserAnswers
			parts = append(parts, buildConfirmationResponsePart(fcID, true, payload.ToMap()))
		}
		return parts
	}

	rejectionReasons := ExtractRejectionReasonsFromMessage(message)

	if decision == DecisionBatch {
		batchDecisions := ExtractBatchDecisionsFromMessage(message)
		if batchDecisions == nil {
			batchDecisions = map[string]DecisionType{}
		}
		var parts []a2atype.Part
		for fcID, pc := range pending {
			payload := ParseHitlConfirmationPayload(pc.OriginalPayload)
			var confirmed bool
			if payload.HasSubagentHitl() {
				allApproved := true
				for _, d := range batchDecisions {
					if d != DecisionApprove {
						allApproved = false
						break
					}
				}
				confirmed = allApproved
				payload.BatchDecisions = batchDecisions
				payload.RejectionReasons = rejectionReasons
			} else {
				toolDecision, exists := batchDecisions[pc.OriginalID]
				if !exists {
					toolDecision = DecisionApprove
				}
				confirmed = toolDecision == DecisionApprove
				payload.RejectionReason = ""
				if !confirmed {
					payload.RejectionReason = rejectionReasons[pc.OriginalID]
				}
			}
			parts = append(parts, buildConfirmationResponsePart(fcID, confirmed, payload.ToMap()))
		}
		return parts
	}

	// Uniform approve/reject.
	confirmed := decision == DecisionApprove
	var parts []a2atype.Part
	for fcID, pc := range pending {
		payload := ParseHitlConfirmationPayload(pc.OriginalPayload)
		if !confirmed {
			payload.RejectionReason = rejectionReasons["*"]
		}
		parts = append(parts, buildConfirmationResponsePart(fcID, confirmed, payload.ToMap()))
	}
	return parts
}

// buildConfirmationResponsePart builds the A2A DataPart for a ToolConfirmation FunctionResponse.
func buildConfirmationResponsePart(fcID string, confirmed bool, payload map[string]any) a2atype.Part {
	tc := toolconfirmation.ToolConfirmation{
		Confirmed: confirmed,
		Payload:   payload,
	}
	serialized, _ := json.Marshal(tc)
	return &a2atype.DataPart{
		Data: map[string]any{
			PartKeyName:     toolconfirmation.FunctionCallName,
			PartKeyID:       fcID,
			PartKeyResponse: map[string]any{"response": string(serialized)},
		},
		Metadata: map[string]any{
			GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
		},
	}
}

func extractMessageField(message *a2atype.Message, key string) any {
	if message == nil {
		return nil
	}
	for _, part := range message.Parts {
		dp := asDataPart(part)
		if dp == nil {
			continue
		}
		if value, ok := dp.Data[key]; ok {
			return value
		}
	}
	return nil
}

func parseDecisionMap(raw any) map[string]DecisionType {
	switch typed := raw.(type) {
	case map[string]DecisionType:
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]DecisionType, len(typed))
		maps.Copy(result, typed)
		return result
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]DecisionType, len(typed))
		for id, decision := range typed {
			switch DecisionType(decision) {
			case DecisionApprove, DecisionReject:
				result[id] = DecisionType(decision)
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]DecisionType, len(typed))
		for id, value := range typed {
			decision, _ := value.(string)
			switch DecisionType(decision) {
			case DecisionApprove, DecisionReject:
				result[id] = DecisionType(decision)
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

func parseStringMap(raw any) map[string]string {
	switch typed := raw.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]string, len(typed))
		for key, value := range typed {
			if value != "" {
				result[key] = value
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		result := make(map[string]string, len(typed))
		for key, value := range typed {
			str, _ := value.(string)
			if str != "" {
				result[key] = str
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

func parseHitlPartsValue(raw any) []HitlPartInfo {
	switch typed := raw.(type) {
	case []HitlPartInfo:
		if len(typed) == 0 {
			return nil
		}
		return append([]HitlPartInfo(nil), typed...)
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
