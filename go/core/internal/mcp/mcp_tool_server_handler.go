/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agent_translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// --- Input/Output types for tool server tools ---

type ListToolServersInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema:"Optional namespace filter"`
}

type ListToolServersOutput struct {
	Servers []ToolServerSummary `json:"servers"`
}

type ToolServerSummary struct {
	Ref      string `json:"ref"`              // "Kind/namespace/name"
	Kind     string `json:"kind"`             // "RemoteMCPServer", "Service", "MCPServer"
	URL      string `json:"url"`              // resolved endpoint URL
	Protocol string `json:"protocol"`         // "STREAMABLE_HTTP" or "SSE"
	Status   string `json:"status,omitempty"` // "Ready" / "NotReady" (MCPServer only)
}

type ListToolsInput struct {
	Server string `json:"server" jsonschema:"Tool server reference in Kind/namespace/name format (e.g. RemoteMCPServer/default/my-server)"`
}

type ListToolsOutput struct {
	Server string     `json:"server"`
	Tools  []ToolInfo `json:"tools"`
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

type CallToolInput struct {
	Server    string         `json:"server" jsonschema:"Tool server reference in Kind/namespace/name format"`
	Tool      string         `json:"tool" jsonschema:"Tool name to invoke"`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"Tool arguments as JSON object"`
}

type CallToolOutput struct {
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	Content any    `json:"content"`
	IsError bool   `json:"isError"`
}

// --- Ref parsing ---

// parseServerRef parses a "Kind/namespace/name" reference into components.
func parseServerRef(ref string) (kind, namespace, name string, err error) {
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("invalid server reference %q: must be Kind/namespace/name (e.g. RemoteMCPServer/default/my-server)", ref)
	}
	kind, namespace, name = parts[0], parts[1], parts[2]
	switch kind {
	case "RemoteMCPServer", "Service", "MCPServer":
		return kind, namespace, name, nil
	default:
		return "", "", "", fmt.Errorf("unknown server kind %q: must be RemoteMCPServer, Service, or MCPServer", kind)
	}
}

// resolveToRemoteMCPServer fetches the referenced resource and converts it to a RemoteMCPServer
// so all three source types converge to a single type for transport creation.
func (h *MCPHandler) resolveToRemoteMCPServer(ctx context.Context, kind, namespace, name string) (*v1alpha2.RemoteMCPServer, error) {
	key := client.ObjectKey{Namespace: namespace, Name: name}

	switch kind {
	case "RemoteMCPServer":
		server := &v1alpha2.RemoteMCPServer{}
		if err := h.kubeClient.Get(ctx, key, server); err != nil {
			return nil, fmt.Errorf("remoteMCPServer %s/%s not found: %w", namespace, name, err)
		}
		return server, nil

	case "Service":
		svc := &corev1.Service{}
		if err := h.kubeClient.Get(ctx, key, svc); err != nil {
			return nil, fmt.Errorf("service %s/%s not found: %w", namespace, name, err)
		}
		return agent_translator.ConvertServiceToRemoteMCPServer(svc)

	case "MCPServer":
		mcpServer := &v1alpha1.MCPServer{}
		if err := h.kubeClient.Get(ctx, key, mcpServer); err != nil {
			return nil, fmt.Errorf("mcpServer %s/%s not found: %w", namespace, name, err)
		}
		return agent_translator.ConvertMCPServerToRemoteMCPServer(mcpServer)

	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

// --- Session caching ---

// getOrCreateSession returns a cached MCP client session or creates a new one.
func (h *MCPHandler) getOrCreateSession(ctx context.Context, ref string, server *v1alpha2.RemoteMCPServer) (*mcpsdk.ClientSession, error) {
	if cached, ok := h.sessions.Load(ref); ok {
		if session, ok := cached.(*mcpsdk.ClientSession); ok {
			return session, nil
		}
	}

	transport, err := CreateMCPTransport(ctx, h.kubeClient, server)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for %s: %w", ref, err)
	}

	impl := &mcpsdk.Implementation{
		Name:    "kagent-controller",
		Version: version.Version,
	}
	mcpClient := mcpsdk.NewClient(impl, nil)

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s at %s: %w", ref, server.Spec.URL, err)
	}

	h.sessions.Store(ref, session)
	return session, nil
}

// evictSession closes and removes a cached session so the next call creates a fresh one.
func (h *MCPHandler) evictSession(ref string) {
	if cached, ok := h.sessions.LoadAndDelete(ref); ok {
		if session, ok := cached.(*mcpsdk.ClientSession); ok {
			session.Close()
		}
	}
}

// --- Tool handlers ---

// handleListToolServers lists all MCP tool servers across RemoteMCPServer, Service, and MCPServer.
func (h *MCPHandler) handleListToolServers(ctx context.Context, req *mcpsdk.CallToolRequest, input ListToolServersInput) (*mcpsdk.CallToolResult, ListToolServersOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "list_tool_servers")

	listOpts := []client.ListOption{}
	if input.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(input.Namespace))
	}

	var servers []ToolServerSummary

	// 1. RemoteMCPServers
	remoteMCPList := &v1alpha2.RemoteMCPServerList{}
	if err := h.kubeClient.List(ctx, remoteMCPList, listOpts...); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list RemoteMCPServers: %v", err)},
			},
			IsError: true,
		}, ListToolServersOutput{}, nil
	}
	for _, r := range remoteMCPList.Items {
		servers = append(servers, ToolServerSummary{
			Ref:      fmt.Sprintf("RemoteMCPServer/%s/%s", r.Namespace, r.Name),
			Kind:     "RemoteMCPServer",
			URL:      r.Spec.URL,
			Protocol: string(r.Spec.Protocol),
		})
	}

	// 2. Services with kagent.dev/mcp-service=true label
	svcListOpts := append([]client.ListOption{
		client.MatchingLabels{agent_translator.MCPServiceLabel: "true"},
	}, listOpts...)
	svcList := &corev1.ServiceList{}
	if err := h.kubeClient.List(ctx, svcList, svcListOpts...); err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list Services: %v", err)},
			},
			IsError: true,
		}, ListToolServersOutput{}, nil
	}
	for _, svc := range svcList.Items {
		remoteMCP, err := agent_translator.ConvertServiceToRemoteMCPServer(&svc)
		if err != nil {
			log.V(1).Info("Skipping Service with invalid MCP config", "service", svc.Name, "namespace", svc.Namespace, "error", err)
			continue
		}
		servers = append(servers, ToolServerSummary{
			Ref:      fmt.Sprintf("Service/%s/%s", svc.Namespace, svc.Name),
			Kind:     "Service",
			URL:      remoteMCP.Spec.URL,
			Protocol: string(remoteMCP.Spec.Protocol),
		})
	}

	// 3. MCPServers (optional — CRD may not be installed)
	mcpServerList := &v1alpha1.MCPServerList{}
	if err := h.kubeClient.List(ctx, mcpServerList, listOpts...); err != nil {
		// If the CRD isn't installed, treat as empty list
		if !meta.IsNoMatchError(err) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list MCPServers: %v", err)},
				},
				IsError: true,
			}, ListToolServersOutput{}, nil
		}
	} else {
		for _, m := range mcpServerList.Items {
			status := "NotReady"
			for _, condition := range m.Status.Conditions {
				if condition.Type == string(v1alpha1.MCPServerConditionReady) && condition.Status == metav1.ConditionTrue {
					status = "Ready"
					break
				}
			}

			remoteMCP, err := agent_translator.ConvertMCPServerToRemoteMCPServer(&m)
			if err != nil {
				log.V(1).Info("Skipping MCPServer with invalid config", "mcpserver", m.Name, "namespace", m.Namespace, "error", err)
				continue
			}

			servers = append(servers, ToolServerSummary{
				Ref:      fmt.Sprintf("MCPServer/%s/%s", m.Namespace, m.Name),
				Kind:     "MCPServer",
				URL:      remoteMCP.Spec.URL,
				Protocol: string(remoteMCP.Spec.Protocol),
				Status:   status,
			})
		}
	}

	log.Info("Listed tool servers", "count", len(servers))

	output := ListToolServersOutput{Servers: servers}

	var fallbackText strings.Builder
	if len(servers) == 0 {
		fallbackText.WriteString("No tool servers found.")
	} else {
		for i, s := range servers {
			if i > 0 {
				fallbackText.WriteByte('\n')
			}
			fmt.Fprintf(&fallbackText, "%s url=%s protocol=%s", s.Ref, s.URL, s.Protocol)
			if s.Status != "" {
				fmt.Fprintf(&fallbackText, " status=%s", s.Status)
			}
		}
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fallbackText.String()},
		},
	}, output, nil
}

// handleListTools connects to a tool server and returns its available tools.
func (h *MCPHandler) handleListTools(ctx context.Context, req *mcpsdk.CallToolRequest, input ListToolsInput) (*mcpsdk.CallToolResult, ListToolsOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "list_tools", "server", input.Server)

	kind, namespace, name, err := parseServerRef(input.Server)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, ListToolsOutput{}, nil
	}

	remoteMCP, err := h.resolveToRemoteMCPServer(ctx, kind, namespace, name)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, ListToolsOutput{}, nil
	}

	session, err := h.getOrCreateSession(ctx, input.Server, remoteMCP)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to connect to %s: %v", input.Server, err)},
			},
			IsError: true,
		}, ListToolsOutput{}, nil
	}

	result, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		// Connection may be stale; evict and retry once
		h.evictSession(input.Server)
		session, err = h.getOrCreateSession(ctx, input.Server, remoteMCP)
		if err != nil {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to reconnect to %s: %v", input.Server, err)},
				},
				IsError: true,
			}, ListToolsOutput{}, nil
		}
		result, err = session.ListTools(ctx, &mcpsdk.ListToolsParams{})
		if err != nil {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to list tools on %s: %v", input.Server, err)},
				},
				IsError: true,
			}, ListToolsOutput{}, nil
		}
	}

	tools := make([]ToolInfo, 0, len(result.Tools))
	for _, t := range result.Tools {
		tools = append(tools, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	log.Info("Listed tools", "server", input.Server, "count", len(tools))

	output := ListToolsOutput{
		Server: input.Server,
		Tools:  tools,
	}

	var fallbackText strings.Builder
	if len(tools) == 0 {
		fmt.Fprintf(&fallbackText, "No tools found on %s.", input.Server)
	} else {
		fmt.Fprintf(&fallbackText, "Tools on %s:\n", input.Server)
		for _, t := range tools {
			fmt.Fprintf(&fallbackText, "- %s: %s\n", t.Name, t.Description)
		}
	}

	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: fallbackText.String()},
		},
	}, output, nil
}

// handleCallTool invokes a specific tool on a specific tool server.
func (h *MCPHandler) handleCallTool(ctx context.Context, req *mcpsdk.CallToolRequest, input CallToolInput) (*mcpsdk.CallToolResult, CallToolOutput, error) {
	log := ctrllog.FromContext(ctx).WithName("mcp-handler").WithValues("tool", "call_tool", "server", input.Server, "targetTool", input.Tool)

	if input.Tool == "" {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: "tool name is required"},
			},
			IsError: true,
		}, CallToolOutput{}, nil
	}

	kind, namespace, name, err := parseServerRef(input.Server)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, CallToolOutput{}, nil
	}

	remoteMCP, err := h.resolveToRemoteMCPServer(ctx, kind, namespace, name)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, CallToolOutput{}, nil
	}

	session, err := h.getOrCreateSession(ctx, input.Server, remoteMCP)
	if err != nil {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to connect to %s: %v", input.Server, err)},
			},
			IsError: true,
		}, CallToolOutput{}, nil
	}

	result, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      input.Tool,
		Arguments: input.Arguments,
	})
	if err != nil {
		// Connection may be stale; evict and retry once
		h.evictSession(input.Server)
		session, err = h.getOrCreateSession(ctx, input.Server, remoteMCP)
		if err != nil {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to reconnect to %s: %v", input.Server, err)},
				},
				IsError: true,
			}, CallToolOutput{}, nil
		}
		result, err = session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      input.Tool,
			Arguments: input.Arguments,
		})
		if err != nil {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: fmt.Sprintf("Failed to call tool %s on %s: %v", input.Tool, input.Server, err)},
				},
				IsError: true,
			}, CallToolOutput{}, nil
		}
	}

	log.Info("Called tool", "server", input.Server, "targetTool", input.Tool, "isError", result.IsError)

	// Extract text content for fallback
	var fallbackText strings.Builder
	for _, content := range result.Content {
		if textContent, ok := content.(*mcpsdk.TextContent); ok {
			fallbackText.WriteString(textContent.Text)
		}
	}

	output := CallToolOutput{
		Server:  input.Server,
		Tool:    input.Tool,
		Content: result.StructuredContent,
		IsError: result.IsError,
	}

	// If no structured content, use the text content
	if output.Content == nil {
		output.Content = fallbackText.String()
	}

	return result, output, nil
}
