package codex

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
)

type mcpCompilation struct {
	servers  map[string]codexconfig.MCPServer
	egress   []string
	warnings []string
}

func compileMCP(tools []translator.ResolvedMCPTool) (mcpCompilation, error) {
	if len(tools) == 0 {
		return mcpCompilation{}, nil
	}
	result := mcpCompilation{servers: make(map[string]codexconfig.MCPServer, len(tools))}
	identities := map[string]string{}
	for _, tool := range tools {
		server := tool.Server
		if server == nil {
			return mcpCompilation{}, fmt.Errorf("resolved Codex MCP binding has no server")
		}

		name := strings.ReplaceAll(server.Name, ".", "_")
		if previous, ok := identities[name]; ok {
			return mcpCompilation{}, translator.NewValidationError("Codex MCP servers %q and %q map to the same native name %q", previous, server.Name, name)
		}
		identities[name] = server.Name
		if _, ok := result.servers[name]; ok {
			return mcpCompilation{}, translator.NewValidationError("RemoteMCPServer %q is bound more than once", server.Name)
		}
		if server.Spec.Protocol != "" && server.Spec.Protocol != v1alpha3.RemoteMCPServerProtocolStreamableHttp {
			return mcpCompilation{}, translator.NewValidationError("Codex RemoteMCPServer %q requires Streamable HTTP", server.Name)
		}
		if warning := codexMCPCompatibilityWarning(server); warning != "" {
			result.warnings = append(result.warnings, warning)
		}
		host, err := absoluteHTTPOrigin(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, translator.NewValidationError("Codex RemoteMCPServer %q URL %v", server.Name, err)
		}
		selected := append([]string(nil), tool.Binding.Tools...)
		slices.Sort(selected)
		selected = slices.Compact(selected)
		headers, passthrough, err := translator.RuntimeCredentialHeaders(server.Spec.Headers, nil)
		if err != nil {
			return mcpCompilation{}, err
		}
		if len(passthrough) > 0 {
			return mcpCompilation{}, translator.NewValidationError("Codex RemoteMCPServer %q does not support tokenPassthrough", server.Name)
		}
		result.servers[name] = codexconfig.MCPServer{
			URL: server.Spec.URL, Headers: headers, EnabledTools: selected,
			RequireApproval: tool.Binding.RequireApproval,
		}
		result.egress = append(result.egress, host)
	}
	return result, nil
}

func codexMCPCompatibilityWarning(server *v1alpha3.RemoteMCPServer) string {
	var ignored []string
	if !server.Spec.TLS.IsEmpty() {
		ignored = append(ignored, "custom TLS configuration")
	}
	if server.Spec.Timeout != nil && server.Spec.Timeout.Duration != 30*time.Second {
		ignored = append(ignored, "timeout")
	}
	if server.Spec.TerminateOnClose != nil && !*server.Spec.TerminateOnClose {
		ignored = append(ignored, "terminateOnClose")
	}
	if len(ignored) == 0 {
		return ""
	}
	return fmt.Sprintf("Codex RemoteMCPServer %q ignores unsupported fields %s", server.Name, strings.Join(ignored, ", "))
}
