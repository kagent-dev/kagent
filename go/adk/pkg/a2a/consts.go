package a2a

import "google.golang.org/adk/server/adka2a"

const (
	StateKeySessionName = "session_name"
	StateKeySource      = "source"
)

// A2A DataPart metadata keys and type values.
const (
	A2ADataPartMetadataTypeKey              = "type"
	A2ADataPartMetadataIsLongRunningKey     = "is_long_running"
	A2ADataPartMetadataTypeFunctionCall     = "function_call"
	A2ADataPartMetadataTypeFunctionResponse = "function_response"
)

// DataPart map keys for GenAI-style function call / response content.
const (
	PartKeyName     = "name"
	PartKeyArgs     = "args"
	PartKeyResponse = "response"
	PartKeyID       = "id"
)

// HITL batch/rejection/ask-user constants.
const (
	KAgentHitlDecisionTypeBatch   = "batch"
	KAgentHitlDecisionsKey        = "decisions"
	KAgentHitlRejectionReasonsKey = "rejection_reasons"
	KAgentAskUserAnswersKey       = "ask_user_answers"
)

// ReadMetadataValue checks adk_<key> first, then kagent_<key>.
// Returns the value and true if found, or (nil, false).
func ReadMetadataValue(metadata map[string]any, key string) (any, bool) {
	if metadata == nil {
		return nil, false
	}
	adkKey := adka2a.ToA2AMetaKey(key)
	if v, ok := metadata[adkKey]; ok {
		return v, true
	}
	kagentKey := KAgentMetadataKeyPrefix + key
	if v, ok := metadata[kagentKey]; ok {
		return v, true
	}
	return nil, false
}
