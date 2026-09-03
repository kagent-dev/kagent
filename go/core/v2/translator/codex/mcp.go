package codex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	codexconfig "github.com/kagent-dev/kagent/go/harness/codex/config"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type mcpCompilation struct {
	servers     map[string]codexconfig.MCPServer
	environment []corev1.EnvVar
	egress      []string
	warnings    []string
}

func (c *Compiler) compileMCP(ctx context.Context, namespace string, tools []v2translator.ResolvedMCPTool) (mcpCompilation, error) {
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
			return mcpCompilation{}, v2translator.NewValidationError("Codex MCP servers %q and %q map to the same native name %q", previous, server.Name, name)
		}
		identities[name] = server.Name
		if _, ok := result.servers[name]; ok {
			return mcpCompilation{}, v2translator.NewValidationError("RemoteMCPServer %q is bound more than once", server.Name)
		}
		if server.Spec.Protocol != "" && server.Spec.Protocol != v1alpha3.RemoteMCPServerProtocolStreamableHttp {
			return mcpCompilation{}, v2translator.NewValidationError("Codex RemoteMCPServer %q requires Streamable HTTP", server.Name)
		}
		if warning := codexMCPCompatibilityWarning(server); warning != "" {
			result.warnings = append(result.warnings, warning)
		}
		host, err := absoluteHTTPHostname(server.Spec.URL)
		if err != nil {
			return mcpCompilation{}, v2translator.NewValidationError("Codex RemoteMCPServer %q URL %v", server.Name, err)
		}
		headers, environment, err := c.compileMCPHeaders(ctx, namespace, server.Spec.HeadersFrom)
		if err != nil {
			return mcpCompilation{}, fmt.Errorf("compile RemoteMCPServer %q headers: %w", server.Name, err)
		}
		selected := append([]string(nil), tool.Binding.Tools...)
		slices.Sort(selected)
		selected = slices.Compact(selected)
		result.servers[name] = codexconfig.MCPServer{URL: server.Spec.URL, Headers: headers, EnabledTools: selected}
		result.environment = append(result.environment, environment...)
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
		if _, ok := headers[ref.Name]; ok {
			return nil, nil, v2translator.NewValidationError("duplicate MCP header %q", ref.Name)
		}
		switch {
		case ref.ValueFrom == nil:
			headers[ref.Name] = ref.Value
		case ref.ValueFrom.Type == v1alpha3.ConfigMapValueSource:
			configMap := krt.FetchOne(c.ctx, c.collections.ConfigMaps, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: ref.ValueFrom.Name}))
			if configMap == nil {
				return nil, nil, fmt.Errorf("ConfigMap %q not found", ref.ValueFrom.Name)
			}
			value, ok := (*configMap).Data[ref.ValueFrom.Key]
			if !ok {
				return nil, nil, fmt.Errorf("ConfigMap %q does not contain key %q", ref.ValueFrom.Name, ref.ValueFrom.Key)
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
