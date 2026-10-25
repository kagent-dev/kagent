package claude

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
)

type mcpCompilation struct {
	servers  map[string]claudeconfig.MCPServer
	egress   []string
	warnings []string
}

func compileMCP(tools []translator.ResolvedMCPTool) (mcpCompilation, error) {
	if len(tools) == 0 {
		return mcpCompilation{}, nil
	}
	result := mcpCompilation{servers: make(map[string]claudeconfig.MCPServer, len(tools))}
	identities := make(map[string]string, len(tools))
	for _, tool := range tools {
		server := tool.Server
		if server == nil {
			return mcpCompilation{}, fmt.Errorf("resolved Claude MCP binding has no server")
		}

		name := strings.ReplaceAll(server.Name, ".", "_")
		if previous, exists := identities[name]; exists {
			return mcpCompilation{}, translator.NewValidationError("Claude MCP servers %q and %q map to the same native name %q", previous, server.Name, name)
		}
		identities[name] = server.Name
		if _, exists := result.servers[name]; exists {
			return mcpCompilation{}, translator.NewValidationError("RemoteMCPServer %q is bound more than once", server.Name)
		}

		if warning := mcpSelectionWarning(tool.Binding.Tools, server); warning != "" {
			result.warnings = append(result.warnings, warning)
		}
		transport, compatibilityWarning, err := claudeMCPTransport(server)
		if err != nil {
			return mcpCompilation{}, err
		}
		if compatibilityWarning != "" {
			result.warnings = append(result.warnings, compatibilityWarning)
		}
		hostname, err := mcpOrigin(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, err
		}
		headers, passthrough, err := translator.RuntimeCredentialHeaders(server.Spec.Headers, nil)
		if err != nil {
			return mcpCompilation{}, err
		}
		if passthrough {
			return mcpCompilation{}, translator.NewValidationError("Claude RemoteMCPServer %q does not support tokenPassthrough", server.Name)
		}
		result.servers[name] = claudeconfig.MCPServer{
			Type: transport, URL: server.Spec.URL, Headers: headers,
			RequireApproval: tool.Binding.RequireApproval,
		}
		result.egress = append(result.egress, hostname)
	}
	return result, nil
}

func mcpSelectionWarning(selectedTools []string, server *v1alpha3.RemoteMCPServer) string {
	if len(selectedTools) == 0 {
		return ""
	}
	selected := append([]string(nil), selectedTools...)
	slices.Sort(selected)
	selected = slices.Compact(selected)
	discovered, current := currentDiscoveredToolNames(server)
	if current && slices.Equal(selected, discovered) {
		return ""
	}
	if !current {
		return fmt.Sprintf(
			"Claude RemoteMCPServer %q cannot verify selected tools %v because no current discovered tool set is available; exposing the whole server",
			server.Name, selected,
		)
	}
	return fmt.Sprintf(
		"Claude RemoteMCPServer %q does not support partial tool selection: selected %v, discovered %v; exposing the whole server",
		server.Name, selected, discovered,
	)
}

func currentDiscoveredToolNames(server *v1alpha3.RemoteMCPServer) ([]string, bool) {
	if server.Status.ObservedGeneration != server.Generation || len(server.Status.DiscoveredTools) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(server.Status.DiscoveredTools))
	for _, tool := range server.Status.DiscoveredTools {
		if tool == nil || strings.TrimSpace(tool.Name) == "" {
			return nil, false
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if len(slices.Compact(append([]string(nil), names...))) != len(names) {
		return nil, false
	}
	return names, true
}

func claudeMCPTransport(server *v1alpha3.RemoteMCPServer) (string, string, error) {
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
	var warning string
	if len(ignored) != 0 {
		warning = fmt.Sprintf("Claude RemoteMCPServer %q ignores unsupported fields %s", server.Name, strings.Join(ignored, ", "))
	}
	switch server.Spec.Protocol {
	case v1alpha3.RemoteMCPServerProtocolSse:
		return "sse", warning, nil
	case "", v1alpha3.RemoteMCPServerProtocolStreamableHttp:
		return "http", warning, nil
	default:
		return "", "", translator.NewValidationError("Claude RemoteMCPServer %q has unsupported protocol %q", server.Name, server.Spec.Protocol)
	}
}

func mcpOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", translator.NewValidationError("Claude RemoteMCPServer URL must be absolute HTTP(S) without credentials or fragment")
	}
	return egresspolicy.Origin(parsed), nil
}
