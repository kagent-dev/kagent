package a2a

import (
	"fmt"
	"maps"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// MCPExtensionsExtensionURI identifies the versioned kagent A2A extension that
// carries per-request values for MCP extensions. The values are read only when
// the client activated the extension.
const MCPExtensionsExtensionURI = "https://kagent.dev/extensions/mcp-extensions/v1"

// MCPExtensionsExtension returns the optional Agent Card capability
// implemented by the kagent Go ADK runtime.
func MCPExtensionsExtension() a2atype.AgentExtension {
	return a2atype.AgentExtension{
		URI:         MCPExtensionsExtensionURI,
		Description: "Per-request values for the MCP extensions the agent's MCP servers declare",
		Required:    false,
	}
}

// MCPExtensionValues returns the values a request carries under the extension:
// a JSON object keyed by MCP extension id. It returns nil when the request
// carries none, and an error when the entry is not an object or a key is not a
// valid MCP extension id.
func MCPExtensionValues(metadata map[string]any) (map[string]any, error) {
	raw, ok := metadata[MCPExtensionsExtensionURI]
	if !ok || raw == nil {
		return nil, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("metadata %q must be an object keyed by MCP extension id", MCPExtensionsExtensionURI)
	}
	for id := range values {
		if !IsMCPExtensionID(id) {
			return nil, fmt.Errorf("metadata %q: %q is not an MCP extension id", MCPExtensionsExtensionURI, id)
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	return maps.Clone(values), nil
}

// IsMCPExtensionID reports whether id is an MCP extension identifier
// ("{vendor-prefix}/{extension-name}", MCP SEP-2133): a `_meta` key whose
// prefix is mandatory and not reserved by MCP. A prefix whose second label is
// "modelcontextprotocol" or "mcp" is reserved.
func IsMCPExtensionID(id string) bool {
	prefix, name, ok := strings.Cut(id, "/")
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
