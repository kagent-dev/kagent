package claude

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type mcpCompilation struct {
	servers     map[string]claudeconfig.MCPServer
	environment []corev1.EnvVar
	egress      []string
	warnings    []string
}

func (c *Compiler) compileMCP(
	ctx context.Context,
	namespace string,
	tools []translator.ResolvedMCPTool,
) (mcpCompilation, error) {
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

		transport, timeoutMillis, compatibilityWarning, err := claudeMCPTransport(server)
		if err != nil {
			return mcpCompilation{}, err
		}
		if compatibilityWarning != "" {
			result.warnings = append(result.warnings, compatibilityWarning)
		}
		selected, selectionWarning := mcpSelection(tool.Binding.Tools, server, transport)
		if selectionWarning != "" {
			result.warnings = append(result.warnings, selectionWarning)
		}
		hostname, err := mcpOrigin(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, err
		}
		headers, headerEnvironment, err := c.compileMCPHeaders(ctx, namespace, server.Spec.HeadersFrom)
		if err != nil {
			return mcpCompilation{}, fmt.Errorf("compile RemoteMCPServer %q headers: %w", server.Name, err)
		}
		result.servers[name] = claudeconfig.MCPServer{
			Type: transport, URL: server.Spec.URL, Headers: headers,
			RequireApproval: tool.Binding.RequireApproval, TimeoutMillis: timeoutMillis, Tools: selected,
		}
		result.environment = append(result.environment, headerEnvironment...)
		result.egress = append(result.egress, hostname)
	}
	return result, nil
}

// mcpSelection returns the tool selection the harness enforces on a streamable
// HTTP server, whose calls its MCP forwarder carries. Claude Code itself
// cannot select part of a server, so an SSE server exposes the whole server
// unless the selection is all of its current discovered tools.
func mcpSelection(selectedTools []string, server *v1alpha3.RemoteMCPServer, transport string) ([]string, string) {
	if len(selectedTools) == 0 {
		return nil, ""
	}
	selected := append([]string(nil), selectedTools...)
	slices.Sort(selected)
	selected = slices.Compact(selected)
	if transport == "http" {
		return selected, ""
	}
	discovered, current := currentDiscoveredToolNames(server)
	if current && slices.Equal(selected, discovered) {
		return nil, ""
	}
	if !current {
		return nil, fmt.Sprintf(
			"Claude SSE RemoteMCPServer %q cannot verify selected tools %v because no current discovered tool set is available; exposing the whole server",
			server.Name, selected,
		)
	}
	return nil, fmt.Sprintf(
		"Claude SSE RemoteMCPServer %q does not support partial tool selection: selected %v, discovered %v; exposing the whole server",
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

func claudeMCPTransport(server *v1alpha3.RemoteMCPServer) (string, int64, string, error) {
	var ignored []string
	if !server.Spec.TLS.IsEmpty() {
		ignored = append(ignored, "custom TLS configuration")
	}
	var timeoutMillis int64
	if server.Spec.Timeout != nil {
		timeoutMillis = server.Spec.Timeout.Milliseconds()
		if timeoutMillis < claudeconfig.MinMCPTimeoutMillis {
			timeoutMillis = 0
			ignored = append(ignored, "timeout below 1s")
		}
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
		return "sse", timeoutMillis, warning, nil
	case "", v1alpha3.RemoteMCPServerProtocolStreamableHttp:
		return "http", timeoutMillis, warning, nil
	default:
		return "", 0, "", translator.NewValidationError("Claude RemoteMCPServer %q has unsupported protocol %q", server.Name, server.Spec.Protocol)
	}
}

func mcpOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", translator.NewValidationError("Claude RemoteMCPServer URL must be absolute HTTP(S) without credentials or fragment")
	}
	return egress.Origin(parsed), nil
}

func (c *Compiler) compileMCPHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, error) {
	if len(refs) == 0 {
		return nil, nil, nil
	}
	headers := make(map[string]string, len(refs))
	var environment []corev1.EnvVar
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" {
			return nil, nil, translator.NewValidationError("MCP header name is required")
		}
		if _, exists := headers[ref.Name]; exists {
			return nil, nil, translator.NewValidationError("duplicate MCP header %q", ref.Name)
		}
		switch {
		case ref.ValueFrom == nil:
			headers[ref.Name] = ref.Value
		case ref.ValueFrom.Type == v1alpha3.ConfigMapValueSource:
			key := types.NamespacedName{Namespace: namespace, Name: ref.ValueFrom.Name}
			fetched := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(key))
			if fetched == nil {
				return nil, nil, fmt.Errorf("ConfigMap %q not found", ref.ValueFrom.Name)
			}
			configMap := *fetched
			value, exists := configMap.Data[ref.ValueFrom.Key]
			if !exists {
				return nil, nil, fmt.Errorf("ConfigMap %q does not contain key %q", configMap.Name, ref.ValueFrom.Key)
			}
			headers[ref.Name] = value
		case ref.ValueFrom.Type == v1alpha3.SecretValueSource:
			sum := sha256.Sum256([]byte(namespace + "\x00" + ref.ValueFrom.Name + "\x00" + ref.ValueFrom.Key))
			name := claudeconfig.MCPCredentialEnvPrefix + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
			headers[ref.Name] = "${" + name + "}"
			environment = append(environment, secretEnvironment(name, ref.ValueFrom.Name, ref.ValueFrom.Key))
		default:
			return nil, nil, translator.NewValidationError("unsupported MCP header value source %q", ref.ValueFrom.Type)
		}
	}
	return headers, environment, nil
}
