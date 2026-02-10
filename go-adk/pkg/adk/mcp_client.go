package adk

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"iter"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/adk/models"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"
)

const (
	// Default timeout matching Python KAGENT_REMOTE_AGENT_TIMEOUT
	defaultTimeout = 30 * time.Minute
)

// MCPToolRegistry stores tools from MCP servers and provides execution
// This implementation uses Google ADK's mcptoolset to match Python ADK behavior
type MCPToolRegistry struct {
	toolsets map[string]tool.Toolset // keyed by server URL, stores Google ADK toolsets
	tools    map[string]*MCPToolInfo // keyed by tool name, for backward compatibility
	logger   logr.Logger
}

// MCPToolInfo stores information about an MCP tool
type MCPToolInfo struct {
	Name                string
	Description         string
	InputSchema         map[string]interface{} // JSON schema
	ServerURL           string
	ServerType          string // "http" or "sse"
	Headers             map[string]string
	Timeout             *float64 // Timeout in seconds for HTTP requests
	SseReadTimeout      *float64 // SSE read timeout in seconds
	TlsDisableVerify    *bool    // If true, skip TLS certificate verification
	TlsCaCertPath       *string  // Path to CA certificate file
	TlsDisableSystemCas *bool    // If true, don't use system CA certificates
}

// NewMCPToolRegistry creates a new MCP tool registry using Google ADK's mcptoolset
func NewMCPToolRegistry(logger logr.Logger) *MCPToolRegistry {
	return &MCPToolRegistry{
		toolsets: make(map[string]tool.Toolset),
		tools:    make(map[string]*MCPToolInfo),
		logger:   logger,
	}
}

// createTransport creates an MCP transport based on server type and configuration
// Uses the official MCP SDK (github.com/modelcontextprotocol/go-sdk/mcp)
func (r *MCPToolRegistry) createTransport(
	url string,
	headers map[string]string,
	serverType string,
	timeout *float64,
	sseReadTimeout *float64,
	tlsDisableVerify *bool,
	tlsCaCertPath *string,
	tlsDisableSystemCas *bool,
) (mcp.Transport, error) {
	// Calculate operation timeout
	operationTimeout := defaultTimeout
	if timeout != nil && *timeout > 0 {
		operationTimeout = time.Duration(*timeout) * time.Second
		// Ensure minimum timeout of 1 second
		if operationTimeout < 1*time.Second {
			operationTimeout = 1 * time.Second
		}
	}

	// Create HTTP client with proper timeout
	httpTimeout := operationTimeout
	if serverType == "sse" && sseReadTimeout != nil && *sseReadTimeout > 0 {
		configuredSseTimeout := time.Duration(*sseReadTimeout) * time.Second
		// Use maximum of configured sseReadTimeout and operationTimeout
		if configuredSseTimeout > operationTimeout {
			httpTimeout = configuredSseTimeout
		} else {
			httpTimeout = operationTimeout
		}
		// Ensure minimum timeout of 1 second
		if httpTimeout < 1*time.Second {
			httpTimeout = 1 * time.Second
		}
	}

	// Create HTTP client with custom transport to support headers and TLS
	baseTransport := &http.Transport{}

	// Configure TLS for self-signed certificates
	if tlsDisableVerify != nil && *tlsDisableVerify {
		// Skip TLS certificate verification (for self-signed certificates)
		// WARNING: This is insecure and should not be used in production
		if r.logger.GetSink() != nil {
			r.logger.Info("WARNING: TLS certificate verification disabled for MCP server - this is insecure and not recommended for production", "url", url)
		}
		baseTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if tlsCaCertPath != nil && *tlsCaCertPath != "" {
		// Load custom CA certificate
		caCert, err := os.ReadFile(*tlsCaCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", *tlsCaCertPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", *tlsCaCertPath)
		}

		// Configure TLS with custom CA
		tlsConfig := &tls.Config{
			RootCAs: caCertPool,
		}
		if tlsDisableSystemCas != nil && *tlsDisableSystemCas {
			// Don't use system CA certificates, only use the provided CA
			tlsConfig.RootCAs = caCertPool
		} else {
			// Use both system CAs and custom CA
			systemCAs, err := x509.SystemCertPool()
			if err != nil {
				// Fallback to custom CA only if system pool unavailable
				tlsConfig.RootCAs = caCertPool
			} else {
				systemCAs.AppendCertsFromPEM(caCert)
				tlsConfig.RootCAs = systemCAs
			}
		}
		baseTransport.TLSClientConfig = tlsConfig
	}

	// Create a RoundTripper that adds headers to all requests
	var httpTransport http.RoundTripper = baseTransport
	if len(headers) > 0 {
		httpTransport = &headerRoundTripper{
			base:    baseTransport,
			headers: headers,
		}
	}

	httpClient := &http.Client{
		Timeout:   httpTimeout,
		Transport: httpTransport,
	}

	// Create MCP transport based on server type using official MCP SDK
	var mcpTransport mcp.Transport

	if serverType == "sse" {
		// For SSE, use SSEClientTransport
		mcpTransport = &mcp.SSEClientTransport{
			Endpoint:   url,
			HTTPClient: httpClient,
		}
	} else {
		// For StreamableHTTP, use StreamableClientTransport
		mcpTransport = &mcp.StreamableClientTransport{
			Endpoint:   url,
			HTTPClient: httpClient,
		}
	}

	return mcpTransport, nil
}

// headerRoundTripper wraps an http.RoundTripper to add custom headers to all requests
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	req = req.Clone(req.Context())

	// Add custom headers
	for key, value := range rt.headers {
		req.Header.Set(key, value)
	}

	return rt.base.RoundTrip(req)
}

// fetchToolsFromServer fetches tools from an MCP server using Google ADK's mcptoolset
func (r *MCPToolRegistry) fetchToolsFromServer(
	ctx context.Context,
	url string,
	headers map[string]string,
	serverType string,
	toolFilter map[string]bool,
	timeout *float64,
	sseReadTimeout *float64,
	tlsDisableVerify *bool,
	tlsCaCertPath *string,
	tlsDisableSystemCas *bool,
) error {
	// Create transport
	mcpTransport, err := r.createTransport(url, headers, serverType, timeout, sseReadTimeout, tlsDisableVerify, tlsCaCertPath, tlsDisableSystemCas)
	if err != nil {
		return fmt.Errorf("failed to create transport for %s: %w", url, err)
	}

	// Create tool filter predicate
	var toolPredicate tool.Predicate
	if len(toolFilter) > 0 {
		allowedTools := make([]string, 0, len(toolFilter))
		for toolName := range toolFilter {
			allowedTools = append(allowedTools, toolName)
		}
		toolPredicate = tool.StringPredicate(allowedTools)
	}

	// Create Google ADK mcptoolset configuration
	cfg := mcptoolset.Config{
		Transport:  mcpTransport,
		ToolFilter: toolPredicate,
	}

	// Create toolset using Google ADK
	toolset, err := mcptoolset.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create MCP toolset for %s: %w", url, err)
	}

	// Store toolset
	r.toolsets[url] = toolset

	// Eagerly fetch and log tool schemas that Google ADK's toolset provides (for debugging parameter name mismatches)
	// This shows what schemas the LLM will actually see
	// Calculate timeout for tool fetching
	initTimeout := core.MCPInitTimeout
	if timeout != nil && *timeout > 0 {
		configuredTimeout := time.Duration(*timeout) * time.Second
		if configuredTimeout > initTimeout {
			initTimeout = configuredTimeout
		}
		// Cap at max timeout for initialization to prevent hanging too long
		if initTimeout > core.MCPInitTimeoutMax {
			initTimeout = core.MCPInitTimeoutMax
		}
	}
	// For SSE, also consider sseReadTimeout
	if serverType == "sse" && sseReadTimeout != nil && *sseReadTimeout > 0 {
		configuredSseTimeout := time.Duration(*sseReadTimeout) * time.Second
		if configuredSseTimeout > initTimeout {
			initTimeout = configuredSseTimeout
		}
		if initTimeout > core.MCPInitTimeoutMax {
			initTimeout = core.MCPInitTimeoutMax
		}
	}

	// Extract tools from toolset for backward compatibility and logging
	// Use a timeout context to ensure tools are fetched within the initialization timeout
	if r.logger.GetSink() != nil {
		r.logger.Info("Eagerly fetching tools from MCP toolset for logging", "url", url, "timeout", initTimeout)
	}
	fetchCtx, fetchCancel := context.WithTimeout(ctx, initTimeout)
	defer fetchCancel()

	readonlyCtx := &readonlyContextImpl{Context: fetchCtx}
	tools, err := toolset.Tools(readonlyCtx)
	if err != nil {
		if r.logger.GetSink() != nil {
			r.logger.Error(err, "Failed to fetch tools from toolset", "url", url, "timeout", initTimeout)
		}
		return fmt.Errorf("failed to get tools from toolset for %s: %w", url, err)
	}

	if r.logger.GetSink() != nil {
		if len(tools) == 0 {
			toolFilterCount := len(toolFilter)
			r.logger.Info("Toolset returned no tools", "url", url, "toolFilterCount", toolFilterCount)
		} else {
			r.logger.Info("Successfully fetched tools from toolset", "url", url, "toolCount", len(tools))
		}
	}

	// Store tool info for backward compatibility and log detailed schemas
	// Also fetch schemas directly from MCP client if toolset doesn't provide them
	var mcpToolsList []*mcp.Tool
	needsDirectFetch := false

	// Check if any tool is missing schema
	for _, t := range tools {
		inputSchema := make(map[string]interface{})
		if schemaTool, ok := t.(interface{ InputSchema() map[string]interface{} }); ok {
			inputSchema = schemaTool.InputSchema()
		}
		// Check if schema is empty or missing properties
		if len(inputSchema) == 0 || inputSchema["properties"] == nil {
			needsDirectFetch = true
			break
		}
	}

	// If schemas are missing, fetch directly from MCP client
	if needsDirectFetch {
		if r.logger.GetSink() != nil {
			r.logger.Info("Toolset schemas incomplete, fetching directly from MCP client", "url", url)
		}
		// Create MCP client to fetch schemas directly
		impl := &mcp.Implementation{
			Name:    "go-adk",
			Version: "1.0.0",
		}
		mcpClient := mcp.NewClient(impl, nil)

		// Connect to MCP server
		conn, err := mcpClient.Connect(fetchCtx, mcpTransport, nil)
		if err == nil {
			defer conn.Close()
			// List tools to get full schemas
			listToolsParams := &mcp.ListToolsParams{}
			result, err := conn.ListTools(fetchCtx, listToolsParams)
			if err == nil && result != nil {
				mcpToolsList = result.Tools
				if r.logger.GetSink() != nil {
					r.logger.Info("Successfully fetched tools from MCP client", "toolCount", len(mcpToolsList))
				}
			} else if err != nil && r.logger.GetSink() != nil {
				r.logger.Error(err, "Failed to list tools from MCP client", "url", url)
			}
		} else if r.logger.GetSink() != nil {
			r.logger.Error(err, "Failed to connect to MCP client for schema fetch", "url", url)
		}
	}

	for _, t := range tools {
		// Get tool name and description
		toolName := t.Name()
		toolDesc := ""
		if descTool, ok := t.(interface{ Description() string }); ok {
			toolDesc = descTool.Description()
		}

		// Get input schema if available from toolset
		inputSchema := make(map[string]interface{})
		if schemaTool, ok := t.(interface{ InputSchema() map[string]interface{} }); ok {
			inputSchema = schemaTool.InputSchema()
		}

		// If schema is empty or missing properties, fetch from MCP client directly
		if (len(inputSchema) == 0 || inputSchema["properties"] == nil) && len(mcpToolsList) > 0 {
			if r.logger.GetSink() != nil {
				r.logger.Info("Fetching schema directly from MCP client", "toolName", toolName, "url", url)
			}
			// Find matching tool in the MCP tools list
			for _, mcpTool := range mcpToolsList {
				if mcpTool.Name == toolName {
					// Convert MCP tool schema to our format
					if mcpTool.InputSchema != nil {
						// Marshal and unmarshal to convert to map[string]interface{}
						if schemaBytes, err := json.Marshal(mcpTool.InputSchema); err == nil {
							if err := json.Unmarshal(schemaBytes, &inputSchema); err == nil {
								if r.logger.GetSink() != nil {
									r.logger.Info("Successfully fetched schema from MCP client", "toolName", toolName)
								}
							}
						}
					}
					// Also update description if available from MCP
					if mcpTool.Description != "" {
						toolDesc = mcpTool.Description
					}
					break
				}
			}
		}

		// Extract parameter names from schema for logging
		paramNames := []string{}
		if properties, ok := inputSchema["properties"].(map[string]interface{}); ok {
			for paramName := range properties {
				paramNames = append(paramNames, paramName)
			}
		}

		// Log detailed schema information
		if r.logger.GetSink() != nil {
			schemaJSON := ""
			if len(inputSchema) > 0 {
				if schemaBytes, err := json.Marshal(inputSchema); err == nil {
					schemaJSON = string(schemaBytes)
					// Truncate if too long
					if len(schemaJSON) > core.SchemaJSONMaxLength {
						schemaJSON = schemaJSON[:core.SchemaJSONMaxLength] + "... (truncated)"
					}
				}
			}

			r.logger.V(1).Info("Google ADK toolset tool schema",
				"url", url,
				"toolName", toolName,
				"description", toolDesc,
				"parameterNames", paramNames,
				"schema", schemaJSON)
		}

		// Store tool info
		r.tools[toolName] = &MCPToolInfo{
			Name:                toolName,
			Description:         toolDesc,
			InputSchema:         inputSchema,
			ServerURL:           url,
			ServerType:          serverType,
			Headers:             headers,
			Timeout:             timeout,
			SseReadTimeout:      sseReadTimeout,
			TlsDisableVerify:    tlsDisableVerify,
			TlsCaCertPath:       tlsCaCertPath,
			TlsDisableSystemCas: tlsDisableSystemCas,
		}

		if r.logger.GetSink() != nil {
			r.logger.Info("Registered MCP tool", "toolName", toolName, "serverURL", url, "serverType", serverType)
		}
	}

	return nil
}

// readonlyContextImpl implements agent.ReadonlyContext for tool discovery
type readonlyContextImpl struct {
	context.Context
}

func (r *readonlyContextImpl) SessionID() string           { return "" }
func (r *readonlyContextImpl) UserID() string              { return "" }
func (r *readonlyContextImpl) AgentName() string           { return "" }
func (r *readonlyContextImpl) AppName() string             { return "" }
func (r *readonlyContextImpl) InvocationID() string        { return "" }
func (r *readonlyContextImpl) Branch() string              { return "" }
func (r *readonlyContextImpl) UserContent() *genai.Content { return nil }
func (r *readonlyContextImpl) ReadonlyState() session.ReadonlyState {
	// Return a minimal implementation of ReadonlyState
	return &readonlyStateImpl{}
}

// FetchToolsFromHttpServer fetches tools from an HTTP MCP server
func (r *MCPToolRegistry) FetchToolsFromHttpServer(ctx context.Context, config core.HttpMcpServerConfig) error {
	url := config.Params.Url
	headers := config.Params.Headers
	if headers == nil {
		headers = make(map[string]string)
	}

	toolFilter := buildToolFilter(config.Tools)
	return r.fetchToolsFromServer(ctx, url, headers, "http", toolFilter, config.Params.Timeout, config.Params.SseReadTimeout, config.Params.TlsDisableVerify, config.Params.TlsCaCertPath, config.Params.TlsDisableSystemCas)
}

// FetchToolsFromSseServer fetches tools from an SSE MCP server
func (r *MCPToolRegistry) FetchToolsFromSseServer(ctx context.Context, config core.SseMcpServerConfig) error {
	url := config.Params.Url
	headers := config.Params.Headers
	if headers == nil {
		headers = make(map[string]string)
	}

	toolFilter := buildToolFilter(config.Tools)
	return r.fetchToolsFromServer(ctx, url, headers, "sse", toolFilter, config.Params.Timeout, config.Params.SseReadTimeout, config.Params.TlsDisableVerify, config.Params.TlsCaCertPath, config.Params.TlsDisableSystemCas)
}

// buildToolFilter creates a map of allowed tool names from a slice
func buildToolFilter(tools []string) map[string]bool {
	toolFilter := make(map[string]bool, len(tools))
	for _, toolName := range tools {
		toolFilter[toolName] = true
	}
	return toolFilter
}

// GetToolsAsFunctionDeclarations converts registered tools to function declarations for LLM
func (r *MCPToolRegistry) GetToolsAsFunctionDeclarations() []models.FunctionDeclaration {
	declarations := make([]models.FunctionDeclaration, 0, len(r.tools))
	for _, tool := range r.tools {
		declarations = append(declarations, models.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}
	return declarations
}

// readonlyStateImpl implements session.ReadonlyState
type readonlyStateImpl struct{}

func (r *readonlyStateImpl) Get(key string) (any, error) {
	return nil, fmt.Errorf("key not found: %s", key)
}

func (r *readonlyStateImpl) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		// No state to iterate
	}
}

// GetToolCount returns the number of registered tools
func (r *MCPToolRegistry) GetToolCount() int {
	return len(r.tools)
}

// GetToolsets returns all toolsets from the registry
// This is used to pass toolsets to Google ADK agents
func (r *MCPToolRegistry) GetToolsets() []tool.Toolset {
	toolsets := make([]tool.Toolset, 0, len(r.toolsets))
	for _, toolset := range r.toolsets {
		toolsets = append(toolsets, toolset)
	}
	return toolsets
}
