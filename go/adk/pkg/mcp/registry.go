package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"
)

const (
	// Default timeout matching Python KAGENT_REMOTE_AGENT_TIMEOUT
	defaultTimeout = 30 * time.Minute
)

// mcpServerParams groups connection parameters for an MCP server,
// reducing parameter sprawl across createTransport / initializeToolSet.
type mcpServerParams struct {
	URL                   string
	Headers               map[string]string
	ServerType            string // "http" or "sse"
	Timeout               *float64
	SseReadTimeout        *float64
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
}

// CreateToolsets creates toolsets from all configured HTTP and SSE MCP servers,
// returning the accumulated toolsets. Errors on individual servers are logged
// and skipped.
func CreateToolsets(ctx context.Context, httpTools []adk.HttpMcpServerConfig, sseTools []adk.SseMcpServerConfig) []tool.Toolset {
	log := logr.FromContextOrDiscard(ctx)
	var toolsets []tool.Toolset

	log.Info("Processing HTTP MCP tools", "httpToolsCount", len(httpTools))
	for i, httpTool := range httpTools {
		params := mcpServerParams{
			URL:                   httpTool.Params.Url,
			Headers:               httpTool.Params.Headers,
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
	if len(params.Headers) > 0 {
		httpTransport = &headerRoundTripper{
			base:    baseTransport,
			headers: params.Headers,
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

// headerRoundTripper wraps an http.RoundTripper to add custom headers to all requests.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for key, value := range rt.headers {
		req.Header.Set(key, value)
	}
	return rt.base.RoundTrip(req)
}

// mcpSession wraps an MCP client session for direct tool calls.
type mcpSession struct {
	client    *mcpsdk.Client
	transport mcpsdk.Transport
	session   *mcpsdk.ClientSession
}

func newMCPSession(transport mcpsdk.Transport) *mcpSession {
	return &mcpSession{
		client:    mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-temporal", Version: "0.1.0"}, nil),
		transport: transport,
	}
}

func (s *mcpSession) connect(ctx context.Context) error {
	session, err := s.client.Connect(ctx, s.transport, nil)
	if err != nil {
		return fmt.Errorf("failed to connect MCP session: %w", err)
	}
	s.session = session
	return nil
}

func (s *mcpSession) listTools(ctx context.Context) ([]*mcpsdk.Tool, error) {
	var tools []*mcpsdk.Tool
	cursor := ""
	for {
		resp, err := s.session.ListTools(ctx, &mcpsdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		tools = append(tools, resp.Tools...)
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	return tools, nil
}

func (s *mcpSession) callTool(ctx context.Context, name string, args any) (*mcpsdk.CallToolResult, error) {
	return s.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
}

// mcpToolRouter maps tool names to their MCP client connections for direct
// tool execution outside the ADK agent pipeline (e.g., Temporal activities).
type mcpToolRouter struct {
	// toolSessions maps tool name -> MCP session that serves it.
	toolSessions map[string]*mcpSession
}

// ToolExecutorResult holds both the executor function and the discovered tool
// declarations for the LLM. This avoids connecting to MCP servers twice.
type ToolExecutorResult struct {
	// Executor routes tool calls to the correct MCP server.
	Executor func(ctx context.Context, toolName string, args []byte) ([]byte, error)
	// ToolDeclarations are genai.FunctionDeclaration entries for the LLM.
	ToolDeclarations []*genai.FunctionDeclaration
}

// CreateToolExecutor creates a ToolExecutor function that routes tool calls to
// the correct MCP server. It connects to all configured MCP servers, discovers
// their tools, builds a routing table, and also returns tool declarations for the LLM.
func CreateToolExecutor(ctx context.Context, httpTools []adk.HttpMcpServerConfig, sseTools []adk.SseMcpServerConfig) (*ToolExecutorResult, error) {
	log := logr.FromContextOrDiscard(ctx)

	router := &mcpToolRouter{
		toolSessions: make(map[string]*mcpSession),
	}

	var decls []*genai.FunctionDeclaration

	// Connect to HTTP MCP servers and discover tools.
	for i, httpTool := range httpTools {
		params := mcpServerParams{
			URL:                   httpTool.Params.Url,
			Headers:               httpTool.Params.Headers,
			ServerType:            "http",
			Timeout:               httpTool.Params.Timeout,
			SseReadTimeout:        httpTool.Params.SseReadTimeout,
			TLSInsecureSkipVerify: httpTool.Params.TLSInsecureSkipVerify,
			TLSCACertPath:         httpTool.Params.TLSCACertPath,
			TLSDisableSystemCAs:   httpTool.Params.TLSDisableSystemCAs,
		}
		serverDecls, err := router.addServer(ctx, log, params, httpTool.Tools, "HTTP", i+1)
		if err != nil {
			log.Error(err, "Failed to add HTTP MCP server for tool executor", "url", params.URL)
			// Continue — partial tool support is better than none.
		}
		decls = append(decls, serverDecls...)
	}

	// Connect to SSE MCP servers and discover tools.
	for i, sseTool := range sseTools {
		params := mcpServerParams{
			URL:                   sseTool.Params.Url,
			Headers:               sseTool.Params.Headers,
			ServerType:            "sse",
			Timeout:               sseTool.Params.Timeout,
			SseReadTimeout:        sseTool.Params.SseReadTimeout,
			TLSInsecureSkipVerify: sseTool.Params.TLSInsecureSkipVerify,
			TLSCACertPath:         sseTool.Params.TLSCACertPath,
			TLSDisableSystemCAs:   sseTool.Params.TLSDisableSystemCAs,
		}
		serverDecls, err := router.addServer(ctx, log, params, sseTool.Tools, "SSE", i+1)
		if err != nil {
			log.Error(err, "Failed to add SSE MCP server for tool executor", "url", params.URL)
		}
		decls = append(decls, serverDecls...)
	}

	if len(router.toolSessions) == 0 {
		log.Info("No MCP tools discovered for tool executor")
		return &ToolExecutorResult{}, nil
	}

	toolNames := make([]string, 0, len(router.toolSessions))
	for name := range router.toolSessions {
		toolNames = append(toolNames, name)
	}
	log.Info("Tool executor ready", "toolCount", len(router.toolSessions), "tools", toolNames)

	return &ToolExecutorResult{
		Executor:         router.execute,
		ToolDeclarations: decls,
	}, nil
}

// addServer connects to an MCP server, discovers its tools, registers them,
// and returns genai.FunctionDeclaration entries for the discovered tools.
func (r *mcpToolRouter) addServer(ctx context.Context, log logr.Logger, params mcpServerParams, toolFilter []string, label string, index int) ([]*genai.FunctionDeclaration, error) {
	transport, err := createTransport(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for %s server %d (%s): %w", label, index, params.URL, err)
	}

	sess := newMCPSession(transport)
	if err := sess.connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to %s server %d (%s): %w", label, index, params.URL, err)
	}

	// Discover tools from this server.
	tools, err := sess.listTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools from %s server %d (%s): %w", label, index, params.URL, err)
	}

	// Build filter set if configured.
	filterSet := make(map[string]bool, len(toolFilter))
	for _, name := range toolFilter {
		filterSet[name] = true
	}

	var decls []*genai.FunctionDeclaration
	registered := 0
	for _, t := range tools {
		if len(filterSet) > 0 && !filterSet[t.Name] {
			continue
		}
		r.toolSessions[t.Name] = sess
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name,
			Description:          t.Description,
			ParametersJsonSchema: t.InputSchema,
		})
		registered++
	}

	log.Info(fmt.Sprintf("Registered %s MCP tools for executor", label),
		"index", index, "url", params.URL, "registered", registered, "total", len(tools))
	return decls, nil
}

// execute implements the ToolExecutor signature.
func (r *mcpToolRouter) execute(ctx context.Context, toolName string, args []byte) ([]byte, error) {
	sess, ok := r.toolSessions[toolName]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q: not registered with any MCP server", toolName)
	}

	// Parse args from JSON to map for CallTool.
	var arguments any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool args for %q: %w", toolName, err)
		}
	}

	result, err := sess.callTool(ctx, toolName, arguments)
	if err != nil {
		return nil, fmt.Errorf("MCP tool %q execution failed: %w", toolName, err)
	}

	if result.IsError {
		var details strings.Builder
		for _, c := range result.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				details.WriteString(tc.Text)
			}
		}
		return nil, fmt.Errorf("MCP tool %q returned error: %s", toolName, details.String())
	}

	// Build text result from content parts.
	if result.StructuredContent != nil {
		return json.Marshal(result.StructuredContent)
	}

	var text strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}

	return json.Marshal(map[string]string{"output": text.String()})
}

// initializeToolSet fetches tools from an MCP server using Google ADK's mcptoolset.
// Returns the created toolset on success.
func initializeToolSet(ctx context.Context, params mcpServerParams, toolFilter map[string]bool) (tool.Toolset, error) {
	mcpTransport, err := createTransport(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for %s: %w", params.URL, err)
	}

	var toolPredicate tool.Predicate
	if len(toolFilter) > 0 {
		allowedTools := make([]string, 0, len(toolFilter))
		for toolName := range toolFilter {
			allowedTools = append(allowedTools, toolName)
		}
		toolPredicate = tool.StringPredicate(allowedTools)
	}

	cfg := mcptoolset.Config{
		Transport:  mcpTransport,
		ToolFilter: toolPredicate,
	}

	toolset, err := mcptoolset.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP toolset for %s: %w", params.URL, err)
	}

	return toolset, nil
}
