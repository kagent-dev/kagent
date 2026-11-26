package a2a

import (
	"fmt"
	"maps"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// MCPToolCallMetadataExtensionURI identifies kagent's optional A2A extension
// carrying request-scoped MCP tool-call metadata.
const MCPToolCallMetadataExtensionURI = "https://kagent.dev/extensions/mcp-tool-call-metadata/v1"

// MCPToolCallMetadataExtensionID identifies kagent's MCP extension for tool-call
// metadata. A server declares support with an empty settings object.
const MCPToolCallMetadataExtensionID = "dev.kagent/tool-call-metadata"

// MCPToolCallMetadataExtension returns the optional Agent Card capability
// implemented by the kagent Go ADK runtime.
func MCPToolCallMetadataExtension() a2atype.AgentExtension {
	return a2atype.AgentExtension{
		URI:         MCPToolCallMetadataExtensionURI,
		Description: "Request-scoped tool-call metadata for MCP servers that opt in",
		Required:    false,
	}
}

// MCPToolCallMetadata returns the opaque JSON object
// carried under the A2A extension URI. Missing, null and empty values contribute
// no metadata.
func MCPToolCallMetadata(metadata map[string]any) (map[string]any, error) {
	raw, ok := metadata[MCPToolCallMetadataExtensionURI]
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata %q must be an object", MCPToolCallMetadataExtensionURI)
	}
	if len(values) == 0 {
		return nil, nil
	}
	return maps.Clone(values), nil
}
