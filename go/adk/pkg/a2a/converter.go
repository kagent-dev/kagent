package a2a

import (
	"context"
	"fmt"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/fileextract"
	kagenta2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
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
		converted, err := adka2a.ToGenAIPart(part)
		if err != nil {
			return nil, err
		}
		keepFileName(converted)
		return converted, nil
	}

	// DataPart using kagent's public metadata contract: convert explicitly.
	if part != nil && part.Metadata != nil {
		if _, has := part.Metadata[kagenta2a.PartTypeMetadataKey]; has {
			return convertDataPartToGenAI(dp, part.Metadata, kagenta2a.PartTypeMetadataKey)
		}
	}

	// DataPart with no recognised type metadata (e.g. {decision_type: "approve"}).
	// Drop it — returning nil excludes it from the GenAI content, matching Python.
	return nil, nil
}

// keepFileName copies a non-image file's name into PartMetadata, which survives ADK blanking DisplayName.
// Vertex rejects PartMetadata, but Gemini never gets these parts: the file wrapper turns them into text.
func keepFileName(p *genai.Part) {
	if p == nil || p.InlineData == nil || p.InlineData.DisplayName == "" || strings.HasPrefix(p.InlineData.MIMEType, "image/") {
		return
	}
	if p.PartMetadata == nil {
		p.PartMetadata = map[string]any{}
	}
	p.PartMetadata[fileextract.FilenameMetadataKey] = p.InlineData.DisplayName
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
	if response := part.FunctionResponse; response != nil {
		result, err := adkToolResponse(response.Response)
		if err != nil {
			return nil, err
		}
		canonical, err := kagenta2a.NewToolResultPart(response.ID, response.Name, result)
		if err != nil {
			return nil, err
		}
		canonical.Metadata = converted.Metadata
		return canonical, nil
	}
	return converted, nil
}

// convertDataPartToGenAI converts a typed DataPart back to GenAI.
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
		if typeKey == kagenta2a.PartTypeMetadataKey {
			part := a2atype.NewDataPart(data)
			part.Metadata = metadata
			activity, ok := kagenta2a.ParseToolActivity(part)
			if !ok || activity.Response == nil {
				return nil, fmt.Errorf("invalid canonical tool response")
			}
			switch result := activity.Response.Outcome.(type) {
			case *apiv1.ToolResponse_Output:
				value := result.Output.AsInterface()
				response, ok = value.(map[string]any)
				if !ok {
					response = map[string]any{"output": value}
				}
			case *apiv1.ToolResponse_Error:
				response = map[string]any{"error": result.Error.AsInterface()}
			case *apiv1.ToolResponse_Pending:
				value := result.Pending.AsInterface()
				response, ok = value.(map[string]any)
				if !ok {
					response = map[string]any{"status": "pending", "value": value}
				}
			default:
				return nil, fmt.Errorf("canonical tool response requires an outcome")
			}
		}
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
