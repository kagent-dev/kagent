package a2a

import (
	"fmt"
	"maps"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// MCPToolCallMetadataExtensionURI identifies kagent's optional A2A extension
// carrying request-scoped MCP tool-call metadata.
const MCPToolCallMetadataExtensionURI = "https://kagent.dev/extensions/mcp-tool-call-metadata/v1"

// MCPToolCallMetadataExtensionID identifies kagent's MCP extension for tool-call
// metadata. A server's settings list the qualified metadata keys it accepts.
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

// MCPToolCallMetadata returns the qualified metadata keys and opaque JSON values
// carried under the A2A extension URI. Missing, null and empty values contribute
// no metadata.
func MCPToolCallMetadata(metadata map[string]any) (map[string]any, error) {
	raw, ok := metadata[MCPToolCallMetadataExtensionURI]
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata %q must be an object keyed by qualified metadata key", MCPToolCallMetadataExtensionURI)
	}
	for key := range values {
		if !IsMCPToolCallMetadataKey(key) {
			return nil, fmt.Errorf("metadata %q: %q is not an allowed tool-call metadata key", MCPToolCallMetadataExtensionURI, key)
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	return maps.Clone(values), nil
}

// IsMCPToolCallMetadataKey reports whether key is allowed by the tool-call metadata
// extension: a qualified MCP metadata key outside MCP's reserved namespaces.
func IsMCPToolCallMetadataKey(key string) bool {
	prefix, name, ok := strings.Cut(key, "/")
	if !ok || !validMetaName(name) {
		return false
	}
	labels := strings.Split(prefix, ".")
	for _, label := range labels {
		if !validMetaLabel(label) {
			return false
		}
	}
	return len(labels) < 2 || labels[1] != "modelcontextprotocol" && labels[1] != "mcp"
}

func validMetaLabel(label string) bool {
	if label == "" || !isASCIILetter(label[0]) {
		return false
	}
	if last := label[len(label)-1]; !isASCIILetter(last) && !isASCIIDigit(last) {
		return false
	}
	for i := 1; i < len(label)-1; i++ {
		if c := label[i]; !isASCIILetter(c) && !isASCIIDigit(c) && c != '-' {
			return false
		}
	}
	return true
}

func validMetaName(name string) bool {
	if name == "" || !isASCIIAlnum(name[0]) || !isASCIIAlnum(name[len(name)-1]) {
		return false
	}
	for i := 1; i < len(name)-1; i++ {
		if c := name[i]; !isASCIIAlnum(c) && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isASCIIDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isASCIIAlnum(c byte) bool  { return isASCIILetter(c) || isASCIIDigit(c) }
