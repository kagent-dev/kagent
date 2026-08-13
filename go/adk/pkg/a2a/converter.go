package a2a

import (
	"context"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// isEmptyDataPart returns true if the part is a DataPart with nil or empty Data.
// The ADK processor emits such parts as cleanup signals for streaming partial
// artifacts and as a fallback for unrecognized GenAI part types.
func isEmptyDataPart(part *a2atype.Part) bool {
	dp := asDataPart(part)
	return dp != nil && len(dp) == 0
}

// a2aPartConverter converts inbound A2A parts to GenAI parts.
func a2aPartConverter(_ context.Context, _ a2atype.Event, part *a2atype.Part) (*genai.Part, error) {
	dp := asDataPart(part)
	if dp == nil {
		// Text and file parts: delegate to ADK default.
		return adka2a.ToGenAIPart(part)
	}

	// DataPart with kagent_type metadata: convert explicitly.
	if part != nil && part.Metadata != nil {
		if _, has := part.Metadata[GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)]; has {
			return convertDataPartToGenAI(dp, part.Metadata, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
		}
	}

	// DataPart with adk_type metadata (produced by the ADK itself): delegate.
	if part != nil && part.Metadata != nil {
		if _, has := part.Metadata[adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey)]; has {
			return adka2a.ToGenAIPart(part)
		}
	}

	// DataPart with no recognised type metadata (e.g. {decision_type: "approve"}).
	// Drop it — returning nil excludes it from the GenAI content, matching Python.
	return nil, nil
}

// genAIPartConverter lets the upstream executor own artifact construction
// while preserving kagent's part filtering and long-running-tool metadata.
func genAIPartConverter(_ context.Context, event *adksession.Event, part *genai.Part) (*a2atype.Part, error) {
	converted, err := adka2a.ToA2APart(part, event.LongRunningToolIDs)
	if err != nil {
		return nil, err
	}
	if isEmptyDataPart(converted) {
		return nil, nil
	}
	return converted, nil
}

// convertDataPartToGenAI converts a DataPart with a type metadata key
// (either adk_type or kagent_type) back to GenAI for inbound message processing.
func convertDataPartToGenAI(data map[string]any, metadata map[string]any, typeKey string) (*genai.Part, error) {
	if data == nil {
		return nil, nil
	}
	partType, _ := metadata[typeKey].(string)
	switch partType {
	case A2ADataPartMetadataTypeFunctionCall:
		name, _ := data[PartKeyName].(string)
		funcArgs, _ := data[PartKeyArgs].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionCall(name, funcArgs)
			if id, ok := data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionCall.ID = id
			}
			return genaiPart, nil
		}
	case A2ADataPartMetadataTypeFunctionResponse:
		name, _ := data[PartKeyName].(string)
		response, _ := data[PartKeyResponse].(map[string]any)
		if name != "" {
			genaiPart := genai.NewPartFromFunctionResponse(name, response)
			if id, ok := data[PartKeyID].(string); ok && id != "" {
				genaiPart.FunctionResponse.ID = id
			}
			return genaiPart, nil
		}
	}
	return adka2a.ToGenAIPart(a2atype.NewDataPart(data))
}
