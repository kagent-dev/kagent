package claude

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	claudeconfig "github.com/kagent-dev/kagent/go/harness/claude/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const mcpCredentialPrefix = "KAGENT_CLAUDE_MCP_CREDENTIAL_"

type mcpCompilation struct {
	servers     map[string]claudeconfig.MCPServer
	environment []corev1.EnvVar
	egress      []string
	warnings    []string
}

func (c *Compiler) compileMCP(
	ctx context.Context,
	namespace string,
	tools []v2translator.ResolvedMCPTool,
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
			return mcpCompilation{}, v2translator.NewValidationError("Claude MCP servers %q and %q map to the same native name %q", previous, server.Name, name)
		}
		identities[name] = server.Name
		if _, exists := result.servers[name]; exists {
			return mcpCompilation{}, v2translator.NewValidationError("RemoteMCPServer %q is bound more than once", server.Name)
		}

		if warning := mcpSelectionWarning(tool.Binding.Tools, server); warning != "" {
			result.warnings = append(result.warnings, warning)
		}
		transport, err := claudeMCPTransport(server)
		if err != nil {
			return mcpCompilation{}, err
		}
		hostname, err := mcpHostname(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, err
		}
		headers, headerEnvironment, err := c.compileMCPHeaders(ctx, namespace, server.Spec.HeadersFrom)
		if err != nil {
			return mcpCompilation{}, fmt.Errorf("compile RemoteMCPServer %q headers: %w", server.Name, err)
		}
		result.servers[name] = claudeconfig.MCPServer{Type: transport, URL: server.Spec.URL, Headers: headers}
		result.environment = append(result.environment, headerEnvironment...)
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

func claudeMCPTransport(server *v1alpha3.RemoteMCPServer) (string, error) {
	if !server.Spec.TLS.IsEmpty() {
		return "", v2translator.NewValidationError("Claude RemoteMCPServer %q does not support custom TLS configuration", server.Name)
	}
	if server.Spec.Timeout != nil && server.Spec.Timeout.Duration != 30*time.Second {
		return "", v2translator.NewValidationError("Claude RemoteMCPServer %q supports only the default 30s timeout", server.Name)
	}
	if server.Spec.TerminateOnClose != nil && !*server.Spec.TerminateOnClose {
		return "", v2translator.NewValidationError("Claude RemoteMCPServer %q requires terminateOnClose", server.Name)
	}
	switch server.Spec.Protocol {
	case v1alpha3.RemoteMCPServerProtocolSse:
		return "sse", nil
	case "", v1alpha3.RemoteMCPServerProtocolStreamableHttp:
		return "http", nil
	default:
		return "", v2translator.NewValidationError("Claude RemoteMCPServer %q has unsupported protocol %q", server.Name, server.Spec.Protocol)
	}
}

func mcpHostname(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", v2translator.NewValidationError("Claude RemoteMCPServer URL must be absolute HTTP(S) without credentials or fragment")
	}
	return parsed.Hostname(), nil
}

func (c *Compiler) compileMCPHeaders(ctx context.Context, namespace string, refs []v1alpha3.ValueRef) (map[string]string, []corev1.EnvVar, error) {
	if len(refs) == 0 {
		return nil, nil, nil
	}
	headers := make(map[string]string, len(refs))
	var environment []corev1.EnvVar
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" {
			return nil, nil, v2translator.NewValidationError("MCP header name is required")
		}
		if _, exists := headers[ref.Name]; exists {
			return nil, nil, v2translator.NewValidationError("duplicate MCP header %q", ref.Name)
		}
		switch {
		case ref.ValueFrom == nil:
			headers[ref.Name] = ref.Value
		case ref.ValueFrom.Type == v1alpha3.ConfigMapValueSource:
			configMap := &corev1.ConfigMap{}
			key := types.NamespacedName{Namespace: namespace, Name: ref.ValueFrom.Name}
			if err := c.kube.Get(ctx, key, configMap); err != nil {
				return nil, nil, err
			}
			value, exists := configMap.Data[ref.ValueFrom.Key]
			if !exists {
				return nil, nil, fmt.Errorf("ConfigMap %q does not contain key %q", configMap.Name, ref.ValueFrom.Key)
			}
			headers[ref.Name] = value
		case ref.ValueFrom.Type == v1alpha3.SecretValueSource:
			sum := sha256.Sum256([]byte(namespace + "\x00" + ref.ValueFrom.Name + "\x00" + ref.ValueFrom.Key))
			name := mcpCredentialPrefix + strings.ToUpper(fmt.Sprintf("%x", sum[:8]))
			headers[ref.Name] = "${" + name + "}"
			environment = append(environment, secretEnvironment(name, ref.ValueFrom.Name, ref.ValueFrom.Key))
		default:
			return nil, nil, v2translator.NewValidationError("unsupported MCP header value source %q", ref.ValueFrom.Type)
		}
	}
	return headers, environment, nil
}
