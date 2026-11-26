package a2a

import (
	"fmt"
	"regexp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpLogLevelMetadataKey = "io.modelcontextprotocol/logLevel"

var mcpMetadataKey = regexp.MustCompile(`^([A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?)*/)?([A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?)?$`)

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
