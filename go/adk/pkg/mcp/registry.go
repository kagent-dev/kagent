package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"github.com/kagent-dev/kagent/go/api/adk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// DynamicHeaderProvider is a function that returns headers to inject into MCP requests.
// It receives the context and should return a map of headers.
// This is used for dynamic token injection (e.g., STS tokens) per session.
type DynamicHeaderProvider func(ctx context.Context) map[string]string

const (
	// Default timeout matching Python KAGENT_REMOTE_AGENT_TIMEOUT
	defaultTimeout = 30 * time.Minute
)

// allowedRequestHeaders reads the incoming A2A request metadata from ctx and
// returns only the header key/value pairs whose names appear in allowed.
// It reads directly from the A2A CallContext that is already present in the Go
// context, avoiding a redundant copy.
//
// Lookup relies on RequestMeta.Get which already does a case-insensitive O(1)
// lookup (NewRequestMeta lowercases keys at construction). Keys in the result
// preserve the casing from the allowed list so the MCP server sees the header
// names the operator configured. When a header has multiple values only the
// first one is forwarded; additional values are intentionally dropped.
func allowedRequestHeaders(ctx context.Context, allowed []string) map[string]string {
	if len(allowed) == 0 {
		return nil
	}
	callCtx, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return nil
	}
	meta := callCtx.RequestMeta()
	if meta == nil {
		return nil
	}
	result := make(map[string]string)
	for _, name := range allowed {
		if vals, ok := meta.Get(name); ok && len(vals) > 0 && vals[0] != "" {
			result[name] = vals[0]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// mcpServerParams groups connection parameters for an MCP server,
// reducing parameter sprawl across createTransport / initializeToolSet.
type mcpServerParams struct {
	URL                   string
	Headers               map[string]string
	AllowedHeaders        []string              // header names to forward from incoming request
	PropagateToken        bool                  // when true, Authorization is forwarded independently of AllowedHeaders
	HeaderProvider        DynamicHeaderProvider // optional per-request headers derived from invocation context (e.g., STS exchanged access tokens)
	ServerType            string                // "http" or "sse"
	Timeout               *float64
	SseReadTimeout        *float64
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
}

// MCPAppToolNames is the set of MCP tool names whose results render as
// interactive MCP App (UI) widgets in the chat (the tool declares a
// ui.resourceUri and is visible to the agent). It is used as a set, so the bool
// value is always true and only key presence is meaningful. The agent attaches
// the model-result compaction callback (see agent.MakeMCPAppModelResultCallback)
// only to these tools. Collect them from CreateToolsets output via
// MCPAppToolNamesFromToolsets.
type MCPAppToolNames map[string]bool

// mcpAppToolset wraps an MCP toolset and records which agent-visible tools
// render as MCP App widgets. Classification happens during ListTools inside
// agentVisibleToolFilter because mcpsdk.Tool.Meta is not preserved on the ADK
// tool.Tool values the toolset exposes later.
type mcpAppToolset struct {
	inner        tool.Toolset
	appToolNames MCPAppToolNames
}

func (m *mcpAppToolset) Name() string {
	return m.inner.Name()
}

func (m *mcpAppToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	return m.inner.Tools(ctx)
}

// MCPAppToolNamesFromToolsets returns the union of MCP App-capable tool names
// recorded on toolsets built by CreateToolsets.
func MCPAppToolNamesFromToolsets(toolsets []tool.Toolset) MCPAppToolNames {
	out := make(MCPAppToolNames)
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

// CreateToolsets creates toolsets from all configured HTTP and SSE MCP servers.
// MCP App-capable tool names are attached to each returned toolset wrapper and
// can be collected in the agent via MCPAppToolNamesFromToolsets. Errors on
// individual servers are logged and skipped.
//
// When propagateToken is true, Authorization is forwarded to every MCP server
// independently of AllowedHeaders, mirroring the Python ADKTokenPropagationPlugin
// behaviour triggered by KAGENT_PROPAGATE_TOKEN.
//
// Optional headerProvider can be used to inject per-request headers
// derived from invocation context (e.g., STS exchanged access tokens).
func CreateToolsets(
	ctx context.Context,
	httpTools []adk.HttpMcpServerConfig,
	sseTools []adk.SseMcpServerConfig,
	propagateToken bool,
	headerProvider DynamicHeaderProvider,
) []tool.Toolset {
	log := logr.FromContextOrDiscard(ctx)
	var toolsets []tool.Toolset

	log.Info("Processing HTTP MCP tools", "httpToolsCount", len(httpTools))
	for i, httpTool := range httpTools {
		params := mcpServerParams{
			URL:                   httpTool.Params.Url,
			Headers:               httpTool.Params.Headers,
			AllowedHeaders:        httpTool.AllowedHeaders,
			PropagateToken:        propagateToken,
			HeaderProvider:        headerProvider,
			ServerType:            "http",
			Timeout:               httpTool.Params.Timeout,
			SseReadTimeout:        httpTool.Params.SseReadTimeout,
			TLSInsecureSkipVerify: httpTool.Params.TLSInsecureSkipVerify,
			TLSCACertPath:         httpTool.Params.TLSCACertPath,
			TLSDisableSystemCAs:   httpTool.Params.TLSDisableSystemCAs,
		}
		ts, err := addToolset(ctx, log, params, httpTool.Tools, "HTTP", i+1)
		if err != nil {
			continue
		}
		toolsets = append(toolsets, ts)
	}

	log.Info("Processing SSE MCP tools", "sseToolsCount", len(sseTools))
	for i, sseTool := range sseTools {
		params := mcpServerParams{
			URL:                   sseTool.Params.Url,
			Headers:               sseTool.Params.Headers,
			AllowedHeaders:        sseTool.AllowedHeaders,
			PropagateToken:        propagateToken,
			HeaderProvider:        headerProvider,
			ServerType:            "sse",
			Timeout:               sseTool.Params.Timeout,
			SseReadTimeout:        sseTool.Params.SseReadTimeout,
			TLSInsecureSkipVerify: sseTool.Params.TLSInsecureSkipVerify,
			TLSCACertPath:         sseTool.Params.TLSCACertPath,
			TLSDisableSystemCAs:   sseTool.Params.TLSDisableSystemCAs,
		}
		ts, err := addToolset(ctx, log, params, sseTool.Tools, "SSE", i+1)
		if err != nil {
			continue
		}
		toolsets = append(toolsets, ts)
	}

	return toolsets
}

// addToolset logs, initializes, and returns a single MCP toolset.
func addToolset(ctx context.Context, log logr.Logger, params mcpServerParams, tools []string, label string, index int) (tool.Toolset, error) {
	if params.Headers == nil {
		params.Headers = make(map[string]string)
	}

	toolFilter := make(map[string]bool, len(tools))
	for _, name := range tools {
		toolFilter[name] = true
	}

	if len(toolFilter) > 0 {
		log.Info(fmt.Sprintf("Adding %s MCP tool", label), "index", index, "url", params.URL, "toolFilterCount", len(toolFilter), "tools", tools)
	} else {
		log.Info(fmt.Sprintf("Adding %s MCP tool", label), "index", index, "url", params.URL, "toolFilterCount", "all")
	}

	ts, err := initializeToolSet(ctx, params, toolFilter)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to fetch tools from %s MCP server", label), "url", params.URL)
		return nil, err
	}
	log.Info(fmt.Sprintf("Successfully added %s MCP toolset", label), "url", params.URL)
	return ts, nil
}

// createTransport creates an MCP transport based on server type and configuration.
// Uses the official MCP SDK (github.com/modelcontextprotocol/go-sdk/mcp).
func createTransport(ctx context.Context, params mcpServerParams) (mcpsdk.Transport, error) {
	log := logr.FromContextOrDiscard(ctx)

	operationTimeout := defaultTimeout
	if params.Timeout != nil && *params.Timeout > 0 {
		operationTimeout = max(time.Duration(*params.Timeout)*time.Second, 1*time.Second)
	}

	httpTimeout := operationTimeout
	if params.ServerType == "sse" && params.SseReadTimeout != nil && *params.SseReadTimeout > 0 {
		configuredSseTimeout := time.Duration(*params.SseReadTimeout) * time.Second
		if configuredSseTimeout > operationTimeout {
			httpTimeout = configuredSseTimeout
		}
		if httpTimeout < 1*time.Second {
			httpTimeout = 1 * time.Second
		}
	}

	baseTransport := &http.Transport{}

	if params.TLSInsecureSkipVerify != nil && *params.TLSInsecureSkipVerify {
		log.Info("WARNING: TLS certificate verification disabled for MCP server - this is insecure and not recommended for production", "url", params.URL)
		baseTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if params.TLSCACertPath != nil && *params.TLSCACertPath != "" {
		caCert, err := os.ReadFile(*params.TLSCACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", *params.TLSCACertPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", *params.TLSCACertPath)
		}

		tlsConfig := &tls.Config{}
		if params.TLSDisableSystemCAs != nil && *params.TLSDisableSystemCAs {
			tlsConfig.RootCAs = caCertPool
		} else {
			systemCAs, err := x509.SystemCertPool()
			if err != nil {
				tlsConfig.RootCAs = caCertPool
			} else {
				systemCAs.AppendCertsFromPEM(caCert)
				tlsConfig.RootCAs = systemCAs
			}
		}
		baseTransport.TLSClientConfig = tlsConfig
	}

	var httpTransport http.RoundTripper = baseTransport
	if len(params.Headers) > 0 || len(params.AllowedHeaders) > 0 || params.PropagateToken || params.HeaderProvider != nil {
		httpTransport = &headerRoundTripper{
			base:           baseTransport,
			headers:        params.Headers,
			allowedHeaders: params.AllowedHeaders,
			propagateToken: params.PropagateToken,
			headerProvider: params.HeaderProvider,
		}
	}

	httpClient := &http.Client{
		Timeout:   httpTimeout,
		Transport: httpTransport,
	}

	var mcpTransport mcpsdk.Transport
	if params.ServerType == "sse" {
		mcpTransport = &mcpsdk.SSEClientTransport{
			Endpoint:   params.URL,
			HTTPClient: httpClient,
		}
	} else {
		mcpTransport = &mcpsdk.StreamableClientTransport{
			Endpoint:   params.URL,
			HTTPClient: httpClient,
		}
	}

	return mcpTransport, nil
}

// mcpToolKind classifies how an MCP tool is surfaced to the agent and the UI,
// derived from the tool's MCP-UI metadata (the "ui" block). Modeling this as a
// single kind rather than a set of overlapping booleans keeps the call sites
// readable and leaves room for additional kinds (e.g. new tool surfaces) without
// reworking signatures.
type mcpToolKind int

const (
	// mcpToolKindAgent is a regular tool exposed to the agent/model with no
	// interactive UI rendering.
	mcpToolKindAgent mcpToolKind = iota
	// mcpToolKindApp is an agent-visible tool whose result renders as an
	// interactive MCP App (UI) widget in the chat (declares a ui.resourceUri).
	mcpToolKindApp
	// mcpToolKindAppOnly is hidden from the agent and only callable from within
	// the rendered MCP App (visibility declares "app" but not "model").
	mcpToolKindAppOnly
)

func (k mcpToolKind) String() string {
	switch k {
	case mcpToolKindApp:
		return "app"
	case mcpToolKindAppOnly:
		return "app_only"
	default:
		return "agent"
	}
}

// mcpUIMetadata holds the MCP-UI extension fields read from a tool's _meta.
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

// mcpToolKindOf classifies a tool from its MCP metadata. App-only takes
// precedence: a tool hidden from the model is never surfaced to the agent even
// if it also declares a UI resource.
func mcpToolKindOf(meta mcpsdk.Meta) mcpToolKind {
	ui := parseMCPUIMetadata(meta)
	if isAppOnlyVisibility(ui.Visibility) {
		return mcpToolKindAppOnly
	}
	if ui.ResourceURI != "" {
		return mcpToolKindApp
	}
	return mcpToolKindAgent
}

// isAppOnlyVisibility reports whether visibility hides a tool from the agent:
// it declares "app" but not "model". Absent visibility defaults to agent-visible.
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

// normalizeVisibility coerces the visibility field, which the MCP spec allows
// as either a single string or a list of strings, into a uniform []string.
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
// Classification must happen here because MCP-UI metadata lives on
// mcpsdk.Tool.Meta, which ADK mcptoolset drops when converting to tool.Tool.
func agentVisibleToolFilter(ctx context.Context, params mcpServerParams, configuredFilter map[string]bool) (tool.Predicate, MCPAppToolNames, error) {
	mcpTransport, err := createTransport(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transport for %s: %w", params.URL, err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-adk"}, nil)
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
	appToolNames := make(MCPAppToolNames)
	for _, t := range result.Tools {
		if t == nil || t.Name == "" {
			continue
		}
		if len(configuredFilter) > 0 && !configuredFilter[t.Name] {
			continue
		}
		switch mcpToolKindOf(t.Meta) {
		case mcpToolKindAppOnly:
			// Hidden from the agent; only the rendered MCP App calls it.
			continue
		case mcpToolKindApp:
			allowedTools = append(allowedTools, t.Name)
			appToolNames[t.Name] = true
		default: // mcpToolKindAgent
			allowedTools = append(allowedTools, t.Name)
		}
	}

	return tool.StringPredicate(allowedTools), appToolNames, nil
}

// headerRoundTripper wraps an http.RoundTripper to add custom headers to all
// requests. It supports four sources of headers, applied in this order so that
// higher-priority sources win on collision:
//  1. propagateToken: when true, Authorization is read from the incoming A2A
//     CallContext and forwarded unconditionally (independent of allowedHeaders).
//  2. allowedHeaders: explicit per-header forwarding from the A2A CallContext.
//  3. headerProvider: runtime headers derived from ADK context, such as STS tokens.
//  4. headers: static key/value pairs configured on the MCP server spec (highest
//     priority — always wins).
type headerRoundTripper struct {
	base           http.RoundTripper
	headers        map[string]string
	allowedHeaders []string // header names (case-insensitive) to forward from A2A context
	propagateToken bool     // when true, Authorization is forwarded independently
	headerProvider DynamicHeaderProvider
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	// When KAGENT_PROPAGATE_TOKEN is set, forward Authorization from the incoming
	// A2A request independently of allowedHeaders.
	if rt.propagateToken {
		if callCtx, ok := a2asrv.CallContextFrom(req.Context()); ok {
			if meta := callCtx.RequestMeta(); meta != nil {
				if vals, ok := meta.Get(constants.AuthorizationHeader); ok && len(vals) > 0 && vals[0] != "" {
					req.Header.Set(constants.AuthorizationHeader, vals[0])
				}
			}
		}
	}

	// Forward explicitly allowed headers from the incoming A2A request.
	for k, v := range allowedRequestHeaders(req.Context(), rt.allowedHeaders) {
		req.Header.Set(k, v)
	}

	// Dynamic headers (e.g., STS access tokens) override propagated/allowed headers.
	if rt.headerProvider != nil {
		for key, value := range rt.headerProvider(req.Context()) {
			req.Header.Set(key, value)
		}
	}

	// Apply static headers last — they take precedence over all dynamic sources.
	for key, value := range rt.headers {
		req.Header.Set(key, value)
	}

	return rt.base.RoundTrip(req)
}

// initializeToolSet fetches tools from an MCP server using Google ADK's
// mcptoolset and wraps the result with any MCP App-capable tool names found
// during classification.
func initializeToolSet(ctx context.Context, params mcpServerParams, toolFilter map[string]bool) (tool.Toolset, error) {
	mcpTransport, err := createTransport(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for %s: %w", params.URL, err)
	}

	toolPredicate, appToolNames, err := agentVisibleToolFilter(ctx, params, toolFilter)
	if err != nil {
		return nil, err
	}

	cfg := mcptoolset.Config{
		Transport:  mcpTransport,
		ToolFilter: toolPredicate,
	}

	toolset, err := mcptoolset.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP toolset for %s: %w", params.URL, err)
	}

	return &mcpAppToolset{inner: toolset, appToolNames: appToolNames}, nil
}
