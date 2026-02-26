package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/config"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

const (
	// Default timeout matching Python KAGENT_REMOTE_AGENT_TIMEOUT
	defaultTimeout = 30 * time.Minute
)

// mcpServerParams groups connection parameters for an MCP server,
// reducing parameter sprawl across createTransport / initializeToolSet.
type mcpServerParams struct {
	URL                 string
	Headers             map[string]string
	ServerType          string // "http" or "sse"
	Timeout             *float64
	SseReadTimeout      *float64
	TLSDisableVerify    *bool
	TLSCACertPath       *string
	TLSDisableSystemCAs *bool
}

// CreateToolsets creates toolsets from all configured HTTP and SSE MCP servers,
// returning the accumulated toolsets. Errors on individual servers are logged
// and skipped.
func CreateToolsets(ctx context.Context, httpTools []config.HttpMcpServerConfig, sseTools []config.SseMcpServerConfig) []tool.Toolset {
	log := logr.FromContextOrDiscard(ctx)
	var toolsets []tool.Toolset

	log.Info("Processing HTTP MCP tools", "httpToolsCount", len(httpTools))
	for i, httpTool := range httpTools {
		params := mcpServerParams{
			URL:                 httpTool.Params.Url,
			Headers:             httpTool.Params.Headers,
			ServerType:          "http",
			Timeout:             httpTool.Params.Timeout,
			SseReadTimeout:      httpTool.Params.SseReadTimeout,
			TLSDisableVerify:    httpTool.Params.TlsDisableVerify,
			TLSCACertPath:       httpTool.Params.TlsCaCertPath,
			TLSDisableSystemCAs: httpTool.Params.TlsDisableSystemCas,
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
			URL:                 sseTool.Params.Url,
			Headers:             sseTool.Params.Headers,
			ServerType:          "sse",
			Timeout:             sseTool.Params.Timeout,
			SseReadTimeout:      sseTool.Params.SseReadTimeout,
			TLSDisableVerify:    sseTool.Params.TlsDisableVerify,
			TLSCACertPath:       sseTool.Params.TlsCaCertPath,
			TLSDisableSystemCAs: sseTool.Params.TlsDisableSystemCas,
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

	if params.TLSDisableVerify != nil && *params.TLSDisableVerify {
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
