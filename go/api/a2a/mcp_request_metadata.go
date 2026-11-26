package a2a

import (
	"fmt"
	"maps"
	"regexp"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPRequestMetadataExtensionURI identifies kagent's optional A2A extension
// carrying the metadata to attach to MCP tool calls for one request.
const MCPRequestMetadataExtensionURI = "https://kagent.dev/extensions/mcp-request-metadata/v1"

const mcpLogLevelMetadataKey = "io.modelcontextprotocol/logLevel"

var mcpMetadataKey = regexp.MustCompile(`^([A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?)*/)?([A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?)?$`)

// MCPRequestMetadataExtension returns the optional Agent Card capability
// implemented by the kagent Go ADK runtime.
func MCPRequestMetadataExtension() a2atype.AgentExtension {
	return a2atype.AgentExtension{
		URI:         MCPRequestMetadataExtensionURI,
		Description: "Request-scoped metadata forwarded to MCP tools/call",
		Required:    false,
	}
}

// MCPRequestMetadata returns a shallow copy of the MCP metadata supplied in the
// A2A envelope. Missing, null and empty values contribute no metadata.
func MCPRequestMetadata(metadata map[string]any) (map[string]any, error) {
	raw, ok := metadata[MCPRequestMetadataExtensionURI]
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata %q must be an object", MCPRequestMetadataExtensionURI)
	}
	if len(values) == 0 {
		return nil, nil
	}
	if err := ValidateMCPRequestMetadata(values); err != nil {
		return nil, err
	}
	return maps.Clone(values), nil
}

// ValidateMCPRequestMetadata checks MCP's top-level _meta key syntax and protects
// protocol and tracing fields owned by the transport. It does not interpret
// application values.
func ValidateMCPRequestMetadata(values map[string]any) error {
	for key := range values {
		if !mcpMetadataKey.MatchString(key) {
			return fmt.Errorf("invalid MCP metadata key %q", key)
		}
		switch key {
		case mcpsdk.MetaKeyProtocolVersion, mcpsdk.MetaKeyClientInfo, mcpsdk.MetaKeyClientCapabilities,
			mcpLogLevelMetadataKey, mcpsdk.MetaKeyServerInfo, mcpsdk.MetaKeySubscriptionID,
			"progressToken", "traceparent", "tracestate", "baggage":
			return fmt.Errorf("MCP metadata key %q is owned by the transport", key)
		}
	}
	return nil
}
