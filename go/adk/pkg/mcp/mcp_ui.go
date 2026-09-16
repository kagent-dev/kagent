package mcp

// MCP Apps (a.k.a. MCP UI) lets an MCP tool declare an interactive HTML view
// that the host renders inline in the chat. A tool opts in via its
// `_meta.ui.resourceUri` field, which points at a `ui://` resource the host
// fetches and renders in a sandboxed iframe; the optional `_meta.ui.visibility`
// field controls whether the tool is exposed to the model, the app, or both.
//
// This file holds the helpers that read those `_meta.ui` fields and classify
// each tool so the agent and UI surface it correctly. The metadata contract is
// defined by the MCP Apps extension, not the core MCP spec:
//
//	Overview: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/extensions/apps/overview.mdx
//	Full spec: https://github.com/modelcontextprotocol/ext-apps/blob/main/specification/2026-01-26/apps.mdx
//
// Keep these helpers here (rather than in registry.go) so the spec mapping stays
// in one place and is easy to track as the extension evolves.

import (
	"context"
	"fmt"
	"sync"

	"github.com/kagent-dev/kagent/go/pkg/logging"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

const (
	// mcpUIExtensionName is the MCP Apps extension identifier negotiated via
	// capabilities.extensions during initialize. Advertising it lets conformant
	// servers that only register UI tools when the client supports them expose
	// those tools to kagent.
	mcpUIExtensionName = "io.modelcontextprotocol/ui"
	// mcpAppHTMLMimeType is the only UI content type defined by the MCP Apps MVP.
	mcpAppHTMLMimeType = "text/html;profile=mcp-app"
)

// mcpUIClientCapabilities advertises MCP Apps support so capability-gated
// servers expose their UI tools.
func mcpUIClientCapabilities() *mcpsdk.ClientCapabilities {
	caps := &mcpsdk.ClientCapabilities{}
	caps.AddExtension(mcpUIExtensionName, map[string]any{"mimeTypes": []string{mcpAppHTMLMimeType}})
	return caps
}

// mcpAppToolset wraps an MCP toolset and records which model-visible tools
// render as MCP App widgets. Classification happens during ListTools inside
// agentVisibleToolFilter because mcpsdk.Tool.Meta is not preserved on the ADK
// tool.Tool values the toolset exposes later.
type mcpAppToolset struct {
	inner tool.Toolset
	// appToolNames is this server's set of model-visible tool names whose
	// results render as interactive MCP App (UI) widgets (the tool declares a
	// `_meta.ui.resourceUri`). Used as a set, so only key presence is meaningful.
	appToolNames map[string]bool
	// serverURL identifies the backend in degradation logs.
	serverURL string

	mu sync.Mutex
	// lastTools is the tool list from the most recent successful ListTools.
	// The MCP toolset lists tools lazily on every invocation, so a backend
	// that goes away mid-conversation would otherwise fail every turn of
	// every agent that references it. Serving the last known list instead
	// makes a dead backend behave like a set of failing tools: the model
	// still sees them and receives a tool error when it calls one.
	lastTools []tool.Tool
	// listed records whether lastTools holds a real result, so an empty
	// list from the server is not confused with "never reached".
	listed bool
}

func (m *mcpAppToolset) Name() string {
	return m.inner.Name()
}

// Tools returns the server's model-visible tools. When the server cannot be
// listed, the failure is logged at error level with the server URL and the
// last successfully listed tools (or none, if the server was never reachable)
// are returned so the agent keeps running. Only a cancelled or expired
// invocation context is still surfaced as an error.
func (m *mcpAppToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := m.inner.Tools(ctx)
	if err == nil {
		m.mu.Lock()
		m.lastTools = append([]tool.Tool(nil), tools...)
		m.listed = true
		m.mu.Unlock()
		return tools, nil
	}
	if ctx.Err() != nil {
		return nil, err
	}

	m.mu.Lock()
	cached := append([]tool.Tool(nil), m.lastTools...)
	listed := m.listed
	m.mu.Unlock()

	log := logging.FromContext(ctx)
	if listed {
		log.ErrorContext(ctx, "MCP server unreachable; continuing with the last known tool list",
			"url", m.serverURL, "toolset", m.inner.Name(), "tools", len(cached), "error", err)
	} else {
		log.ErrorContext(ctx, "MCP server unreachable and its tools were never listed; continuing without them",
			"url", m.serverURL, "toolset", m.inner.Name(), "error", err)
	}
	return cached, nil
}

// MCPAppToolNamesFromToolsets returns the union of MCP App-capable tool names
// recorded on toolsets built by CreateToolsets. The agent attaches the
// model-result compaction callback (see agent.MakeMCPAppModelResultCallback)
// only to these tools.
func MCPAppToolNamesFromToolsets(toolsets []tool.Toolset) map[string]bool {
	out := make(map[string]bool)
	for _, ts := range toolsets {
		aware, ok := ts.(*mcpAppToolset)
		if !ok {
			continue
		}
		for name := range aware.appToolNames {
			out[name] = true
		}
	}
	return out
}

// mcpToolKind classifies how an MCP tool is surfaced, following the MCP Apps
// extension. It is derived from the tool's `_meta.ui` block: presence of a
// `resourceUri` means the tool renders an app, and the `visibility` field
// ("model" / "app") controls who may call it. Modeling this as a single kind
// rather than a set of overlapping booleans keeps the call sites readable and
// leaves room for additional kinds without reworking signatures.
type mcpToolKind int

const (
	// mcpToolKindModel is a regular tool exposed to the model (the LLM/agent)
	// with no interactive UI. Matches `_meta.ui.visibility` of "model" or an
	// absent visibility (which defaults to model-visible).
	mcpToolKindModel mcpToolKind = iota
	// mcpToolKindApp is a model-visible tool that also declares a
	// `_meta.ui.resourceUri`, so its result renders as an interactive MCP App
	// (UI) widget in the chat. The model may call it like any other tool.
	mcpToolKindApp
	// mcpToolKindAppInternal is hidden from the model and only callable from
	// within the rendered MCP App itself (e.g. the widget's own refresh button).
	// It declares `_meta.ui.visibility` of "app" without "model".
	mcpToolKindAppInternal
)

func (k mcpToolKind) String() string {
	switch k {
	case mcpToolKindApp:
		return "app"
	case mcpToolKindAppInternal:
		return "app_internal"
	default:
		return "model"
	}
}

// mcpUIMetadata holds the MCP Apps extension fields read from a tool's
// `_meta.ui` block. See the spec links at the top of this file.
type mcpUIMetadata struct {
	ResourceURI string
	Visibility  []string
}

func parseMCPUIMetadata(meta mcpsdk.Meta) mcpUIMetadata {
	var ui mcpUIMetadata
	if len(meta) == 0 {
		return ui
	}
	if raw, ok := meta["ui"].(map[string]any); ok {
		if uri, _ := raw["resourceUri"].(string); uri != "" {
			ui.ResourceURI = uri
		}
		ui.Visibility = normalizeVisibility(raw["visibility"])
	}
	if ui.ResourceURI == "" {
		if uri, _ := meta["ui/resourceUri"].(string); uri != "" {
			ui.ResourceURI = uri
		}
	}
	return ui
}

// mcpToolKindOf classifies a tool from its MCP metadata. App-internal takes
// precedence: a tool hidden from the model is never surfaced to the model even
// if it also declares a UI resource.
func mcpToolKindOf(meta mcpsdk.Meta) mcpToolKind {
	ui := parseMCPUIMetadata(meta)
	if isAppOnlyVisibility(ui.Visibility) {
		return mcpToolKindAppInternal
	}
	if ui.ResourceURI != "" {
		return mcpToolKindApp
	}
	return mcpToolKindModel
}

// isAppOnlyVisibility reports whether visibility hides a tool from the model:
// it declares "app" but not "model". Absent visibility defaults to
// model-visible.
func isAppOnlyVisibility(visibility []string) bool {
	hasApp := false
	for _, v := range visibility {
		if v == "model" {
			return false
		}
		if v == "app" {
			hasApp = true
		}
	}
	return hasApp
}

// normalizeVisibility coerces the visibility field, which the MCP Apps spec
// allows as either a single string or a list of strings, into a uniform
// []string.
func normalizeVisibility(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// agentVisibleToolFilter lists tools from the MCP server, filters out app-only
// tools and any not in the configured allow-list, and returns a predicate the
// toolset can apply plus the MCP App-capable tool names discovered on this
// server.
//
// Classification must happen here because MCP Apps metadata lives on
// mcpsdk.Tool.Meta, which ADK mcptoolset drops when converting to tool.Tool.
func agentVisibleToolFilter(ctx context.Context, params mcpServerParams, configuredFilter map[string]bool) (tool.Predicate, map[string]bool, error) {
	mcpTransport, err := createTransport(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transport for %s: %w", params.URL, err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-adk"}, &mcpsdk.ClientOptions{Capabilities: mcpUIClientCapabilities()})
	session, err := client.Connect(ctx, mcpTransport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect MCP client for %s: %w", params.URL, err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list MCP tools for %s: %w", params.URL, err)
	}

	allowedTools := make([]string, 0, len(result.Tools))
	appToolNames := make(map[string]bool)
	for _, t := range result.Tools {
		if t == nil || t.Name == "" {
			continue
		}
		if len(configuredFilter) > 0 && !configuredFilter[t.Name] {
			continue
		}
		switch mcpToolKindOf(t.Meta) {
		case mcpToolKindAppInternal:
			// Hidden from the model; only the rendered MCP App calls it.
			continue
		case mcpToolKindApp:
			allowedTools = append(allowedTools, t.Name)
			appToolNames[t.Name] = true
		default: // mcpToolKindModel
			allowedTools = append(allowedTools, t.Name)
		}
	}

	return tool.StringPredicate(allowedTools), appToolNames, nil
}
