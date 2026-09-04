package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/internal/logging"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
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
	meta := callCtx.ServiceParams()
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
	Command               string
	Args                  []string
	Env                   map[string]string
	Dir                   string
	Timeout               *float64
	SseReadTimeout        *float64
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
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
	stdioTools []adk.StdioMcpServerConfig,
	propagateToken bool,
	headerProvider DynamicHeaderProvider,
) []tool.Toolset {
	log := logging.FromContext(ctx)
	var toolsets []tool.Toolset

	log.InfoContext(ctx, "processing stdio MCP tools", "stdio_tools_count", len(stdioTools))
	for i, stdioTool := range stdioTools {
		params := mcpServerParams{
			URL: stdioTool.Command, ServerType: "stdio", Command: stdioTool.Command,
			Args: stdioTool.Args, Env: stdioTool.Env, Dir: stdioTool.Dir,
		}
		ts, err := addToolset(ctx, log, params, nil, "stdio", i+1)
		if err == nil {
			toolsets = append(toolsets, ts)
		}
	}

	log.InfoContext(ctx, "processing HTTP MCP tools", "http_tools_count", len(httpTools))
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

	log.InfoContext(ctx, "processing SSE MCP tools", "sse_tools_count", len(sseTools))
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
func addToolset(ctx context.Context, log *slog.Logger, params mcpServerParams, tools []string, label string, index int) (tool.Toolset, error) {
	if params.Headers == nil {
		params.Headers = make(map[string]string)
	}

	toolFilter := make(map[string]bool, len(tools))
	for _, name := range tools {
		toolFilter[name] = true
	}

	if len(toolFilter) > 0 {
		log.InfoContext(ctx, "adding MCP tool", "transport", label, "index", index, "url", params.URL, "tool_filter_count", len(toolFilter), "tools", tools)
	} else {
		log.InfoContext(ctx, "adding MCP tool", "transport", label, "index", index, "url", params.URL, "tool_filter_count", "all")
	}

	ts, err := initializeToolSet(ctx, params, toolFilter)
	if err != nil {
		log.ErrorContext(ctx, "failed to fetch MCP tools", "transport", label, "error", err, "url", params.URL)
		return nil, err
	}
	log.InfoContext(ctx, "added MCP toolset", "transport", label, "url", params.URL)
	return ts, nil
}

// createTransport creates an MCP transport based on server type and configuration.
// Uses the official MCP SDK (github.com/modelcontextprotocol/go-sdk/mcp).
func createTransport(ctx context.Context, params mcpServerParams) (mcpsdk.Transport, error) {
	log := logging.FromContext(ctx)
	if params.ServerType == "stdio" {
		command := exec.CommandContext(ctx, params.Command, params.Args...)
		command.Dir = params.Dir
		command.Env = os.Environ()
		for key, value := range params.Env {
			command.Env = append(command.Env, key+"="+value)
		}
		return &mcpsdk.CommandTransport{Command: command}, nil
	}

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
		log.WarnContext(ctx, "TLS certificate verification disabled for MCP server", "url", params.URL)
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
			if meta := callCtx.ServiceParams(); meta != nil {
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
		logging.FromContext(ctx).ErrorContext(ctx, "failed to classify MCP tools; falling back to lazy tool discovery", "error", err, "url", params.URL)
		appToolNames = nil
		if len(toolFilter) > 0 {
			allowedTools := make([]string, 0, len(toolFilter))
			for name, enabled := range toolFilter {
				if enabled {
					allowedTools = append(allowedTools, name)
				}
			}
			toolPredicate = tool.StringPredicate(allowedTools)
		} else {
			toolPredicate = nil
		}
	}

	cfg := mcptoolset.Config{
		Transport: mcpTransport,
	}

	toolset, err := mcptoolset.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP toolset for %s: %w", params.URL, err)
	}

	visibleTools := tool.Toolset(toolset)
	if toolPredicate != nil {
		visibleTools = tool.FilterToolset(toolset, toolPredicate)
	}
	return &mcpAppToolset{inner: visibleTools, appToolNames: appToolNames}, nil
}
