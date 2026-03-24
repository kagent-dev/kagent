package a2a

import (
	"context"
	"encoding/json"
	"maps"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/adk/server/adka2a"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// Inbound: A2A → GenAI
// ---------------------------------------------------------------------------

// messageToGenAIContent converts an A2A message to *genai.Content using kagent
// a2aPartConverter logic: handle kagent_type and adk_type DataParts explicitly,
// drop unrecognised DataParts (e.g. HITL decision parts).
func messageToGenAIContent(ctx context.Context, msg *a2atype.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, nil
	}
	parts := make([]*genai.Part, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		genaiPart, err := a2aPartConverter(ctx, msg, part)
		if err != nil {
			return nil, err
		}
		if genaiPart == nil {
			continue
		}
		parts = append(parts, genaiPart)
	}
	var role genai.Role = genai.RoleUser
	if msg.Role == a2atype.MessageRoleAgent {
		role = genai.RoleModel
	}
	return genai.NewContentFromParts(parts, role), nil
}

// a2aPartConverter converts inbound A2A parts to GenAI parts.
//
// DataParts with kagent_type metadata are converted explicitly (function_call /
// function_response).  DataParts with no recognised metadata — including HITL
// decision parts like {decision_type: "approve"} — are dropped (return nil).
func a2aPartConverter(_ context.Context, _ a2atype.Event, part a2atype.Part) (*genai.Part, error) {
	dp := asDataPart(part)
	if dp == nil {
		// Text and file parts: delegate to ADK default.
		return adka2a.ToGenAIPart(part)
	}

	// DataPart with kagent_type metadata: convert explicitly.
	if dp.Metadata != nil {
		if _, has := dp.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)]; has {
			return convertDataPartToGenAI(dp, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
		}
	}

	// DataPart with adk_type metadata (produced by the ADK itself): delegate.
	if dp.Metadata != nil {
		if _, has := dp.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey)]; has {
			return adka2a.ToGenAIPart(part)
		}
	}

	// DataPart with no recognised type metadata (e.g. {decision_type: "approve"}).
	// Drop it — returning nil excludes it from the GenAI content, matching Python.
	return nil, nil
}

// convertDataPartToGenAI converts a DataPart with a type metadata key
// (either adk_type or kagent_type) back to GenAI for inbound message processing.
func convertDataPartToGenAI(p *a2atype.DataPart, typeKey string) (*genai.Part, error) {
	if p == nil {
		return nil, nil
	}
	partType, _ := p.Metadata[typeKey].(string)
	switch partType {
	case A2ADataPartMetadataTypeFunctionCall:
		name, _ := p.Data[PartKeyName].(string)
		funcArgs, _ := p.Data[PartKeyArgs].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionCall(name, funcArgs)
			if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionCall.ID = id
			}
			return genaiPart, nil
		}
	case A2ADataPartMetadataTypeFunctionResponse:
		name, _ := p.Data[PartKeyName].(string)
		response, _ := p.Data[PartKeyResponse].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionResponse(name, response)
			if id, ok := p.Data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionResponse.ID = id
			}
			return genaiPart, nil
		}
	}
	return adka2a.ToGenAIPart(p)
}

// ---------------------------------------------------------------------------
// Outbound: GenAI → A2A stamping / metadata
// ---------------------------------------------------------------------------

// stampSubagentSessionID adds kagent_subagent_session_id to the metadata of a
// function_call DataPart if the tool name is in subagentSessionIDs.
// Port of event_converter.py:_process_subagent_session_id().
func stampSubagentSessionID(part a2atype.Part, subagentSessionIDs map[string]string) {
	dp := asDataPart(part)
	if dp == nil || dp.Metadata == nil {
		return
	}
	partType, _ := ReadMetadataValue(dp.Metadata, A2ADataPartMetadataTypeKey)
	if partType != A2ADataPartMetadataTypeFunctionCall {
		return
	}
	toolName, _ := dp.Data[PartKeyName].(string)
	if toolName == "" {
		return
	}
	if sessionID, ok := subagentSessionIDs[toolName]; ok && sessionID != "" {
		dp.Metadata[GetKAgentMetadataKey("subagent_session_id")] = sessionID
	}
}

// ---------------------------------------------------------------------------
// Metadata helpers
// ---------------------------------------------------------------------------

// toA2AMetadataMap converts v to map[string]any via JSON so values placed in A2A
// Metadata are gob-friendly (no concrete genai types stored in interface{} slots).
func toA2AMetadataMap(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// buildEventMeta merges the base metadata with per-event fields such as
// invocation_id, author, branch, usage_metadata, etc.
func buildEventMeta(baseMeta map[string]any, adkEvent *adksession.Event) map[string]any {
	result := maps.Clone(baseMeta)
	if adkEvent == nil {
		return result
	}
	for k, v := range map[string]string{
		"invocation_id": adkEvent.InvocationID,
		"author":        adkEvent.Author,
		"branch":        adkEvent.Branch,
	} {
		if v != "" {
			result[adka2a.ToA2AMetaKey(k)] = v
		}
	}
	if adkEvent.UsageMetadata != nil {
		// Store usage as map[string]any (JSON round-trip), matching adka2a.addMeta. Raw
		// *genai.GenerateContentResponseUsageMetadata in map values breaks gob encoding
		// when the a2a stack persists task/event metadata.
		if um, err := toA2AMetadataMap(adkEvent.UsageMetadata); err == nil && um != nil {
			result[adka2a.ToA2AMetaKey("usage_metadata")] = um
		}
	}
	if adkEvent.ErrorCode != "" {
		result[adka2a.ToA2AMetaKey("error_code")] = adkEvent.ErrorCode
	}
	return result
}
