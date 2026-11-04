package a2a

import (
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// NewToolResponse serializes a tool's output and explicit failure state.
func NewToolResponse(output any, failed bool) (*apiv1.ToolResponse, error) {
	value, err := toolActivityValue(output)
	if err != nil {
		return nil, err
	}
	return toolResponse(value, failed), nil
}

func toolResponse(value *structpb.Value, failed bool) *apiv1.ToolResponse {
	if failed {
		return &apiv1.ToolResponse{Outcome: &apiv1.ToolResponse_Error{Error: value}}
	}
	return &apiv1.ToolResponse{Outcome: &apiv1.ToolResponse_Output{Output: value}}
}

// NewMCPToolResponse accepts an MCP result or the content blocks returned by a
// native harness. Rich content is retained without decoding SDK-specific types.
func NewMCPToolResponse(output any, failed bool) (*apiv1.ToolResponse, error) {
	value, err := toolActivityValue(output)
	if err != nil {
		return nil, err
	}
	content := value
	if result := value.GetStructValue(); result != nil {
		failed = failed || result.Fields["isError"].GetBoolValue()
		if structured := result.Fields["structuredContent"]; structured != nil {
			if _, null := structured.Kind.(*structpb.Value_NullValue); !null {
				return toolResponse(structured, failed), nil
			}
		}
		content = result.Fields["content"]
	}
	if blocks := content.GetListValue(); len(blocks.GetValues()) > 0 {
		texts := make([]string, 0, len(blocks.Values))
		for _, block := range blocks.Values {
			item := block.GetStructValue()
			if item == nil || item.Fields["type"].GetStringValue() != "text" {
				return toolResponse(value, failed), nil
			}
			text, ok := item.Fields["text"].GetKind().(*structpb.Value_StringValue)
			if !ok {
				return toolResponse(value, failed), nil
			}
			texts = append(texts, text.StringValue)
		}
		value = structpb.NewStringValue(strings.Join(texts, "\n"))
	}
	return toolResponse(value, failed), nil
}
