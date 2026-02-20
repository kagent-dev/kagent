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
	"github.com/kagent-dev/kagent/go-adk/pkg/config"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

const (
	// Default timeout matching Python KAGENT_REMOTE_AGENT_TIMEOUT
	defaultTimeout = 30 * time.Minute

	// MCPInitTimeout is the default timeout for MCP toolset initialization.
	MCPInitTimeout = 2 * time.Minute

	// MCPInitTimeoutMax is the maximum timeout for MCP initialization.
	MCPInitTimeoutMax = 5 * time.Minute
)

// CreateToolsets creates toolsets from all configured HTTP and SSE MCP servers,
// returning the accumulated toolsets. Errors on individual servers are logged
// and skipped.
func CreateToolsets(ctx context.Context, httpTools []config.HttpMcpServerConfig, sseTools []config.SseMcpServerConfig) []tool.Toolset {
	log := logr.FromContextOrDiscard(ctx)
	var toolsets []tool.Toolset

	log.Info("Processing HTTP MCP tools", "httpToolsCount", len(httpTools))
	for i, httpTool := range httpTools {
		url := httpTool.Params.Url
		headers := httpTool.Params.Headers
		if headers == nil {
			headers = make(map[string]string)
		}
		toolFilter := make(map[string]bool, len(httpTool.Tools))
		for _, name := range httpTool.Tools {
			toolFilter[name] = true
		}

		if len(toolFilter) > 0 {
			log.Info("Adding HTTP MCP tool", "index", i+1, "url", url, "toolFilterCount", len(toolFilter), "tools", httpTool.Tools)
		} else {
			log.Info("Adding HTTP MCP tool", "index", i+1, "url", url, "toolFilterCount", "all")
		}

		ts, err := initializeToolSet(ctx, url, headers, "http", toolFilter, httpTool.Params.Timeout, httpTool.Params.SseReadTimeout, httpTool.Params.TlsDisableVerify, httpTool.Params.TlsCaCertPath, httpTool.Params.TlsDisableSystemCas)
		if err != nil {
			log.Error(err, "Failed to fetch tools from HTTP MCP server", "url", url)
			continue
		}
		log.Info("Successfully added HTTP MCP toolset", "url", url)
		toolsets = append(toolsets, ts)
	}

	log.Info("Processing SSE MCP tools", "sseToolsCount", len(sseTools))
	for i, sseTool := range sseTools {
		url := sseTool.Params.Url
		headers := sseTool.Params.Headers
		if headers == nil {
			headers = make(map[string]string)
		}
		toolFilter := make(map[string]bool, len(sseTool.Tools))
		for _, name := range sseTool.Tools {
			toolFilter[name] = true
		}

		if len(toolFilter) > 0 {
			log.Info("Adding SSE MCP tool", "index", i+1, "url", url, "toolFilterCount", len(toolFilter), "tools", sseTool.Tools)
		} else {
			log.Info("Adding SSE MCP tool", "index", i+1, "url", url, "toolFilterCount", "all")
		}

		ts, err := initializeToolSet(ctx, url, headers, "sse", toolFilter, sseTool.Params.Timeout, sseTool.Params.SseReadTimeout, sseTool.Params.TlsDisableVerify, sseTool.Params.TlsCaCertPath, sseTool.Params.TlsDisableSystemCas)
		if err != nil {
			log.Error(err, "Failed to fetch tools from SSE MCP server", "url", url)
			continue
		}
		log.Info("Successfully added SSE MCP toolset", "url", url)
		toolsets = append(toolsets, ts)
	}

	return toolsets
}

// createTransport creates an MCP transport based on server type and configuration.
// Uses the official MCP SDK (github.com/modelcontextprotocol/go-sdk/mcp).
func createTransport(
	ctx context.Context,
	url string,
	headers map[string]string,
	serverType string,
	timeout *float64,
	sseReadTimeout *float64,
	tlsDisableVerify *bool,
	tlsCaCertPath *string,
	tlsDisableSystemCas *bool,
) (mcpsdk.Transport, error) {
	log := logr.FromContextOrDiscard(ctx)

	operationTimeout := defaultTimeout
	if timeout != nil && *timeout > 0 {
		operationTimeout = time.Duration(*timeout) * time.Second
		if operationTimeout < 1*time.Second {
			operationTimeout = 1 * time.Second
		}
	}

	httpTimeout := operationTimeout
	if serverType == "sse" && sseReadTimeout != nil && *sseReadTimeout > 0 {
		configuredSseTimeout := time.Duration(*sseReadTimeout) * time.Second
		if configuredSseTimeout > operationTimeout {
			httpTimeout = configuredSseTimeout
		} else {
			httpTimeout = operationTimeout
		}
		if httpTimeout < 1*time.Second {
			httpTimeout = 1 * time.Second
		}
	}

	baseTransport := &http.Transport{}

	if tlsDisableVerify != nil && *tlsDisableVerify {
		log.Info("WARNING: TLS certificate verification disabled for MCP server - this is insecure and not recommended for production", "url", url)
		baseTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if tlsCaCertPath != nil && *tlsCaCertPath != "" {
		caCert, err := os.ReadFile(*tlsCaCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", *tlsCaCertPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", *tlsCaCertPath)
		}

		tlsConfig := &tls.Config{
			RootCAs: caCertPool,
		}
		if tlsDisableSystemCas != nil && *tlsDisableSystemCas {
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

	var mcpTransport mcpsdk.Transport
	if serverType == "sse" {
		mcpTransport = &mcpsdk.SSEClientTransport{
			Endpoint:   url,
			HTTPClient: httpClient,
		}
	} else {
		mcpTransport = &mcpsdk.StreamableClientTransport{
			Endpoint:   url,
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
func initializeToolSet(
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
) (tool.Toolset, error) {
	mcpTransport, err := createTransport(ctx, url, headers, serverType, timeout, sseReadTimeout, tlsDisableVerify, tlsCaCertPath, tlsDisableSystemCas)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for %s: %w", url, err)
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
		return nil, fmt.Errorf("failed to create MCP toolset for %s: %w", url, err)
	}

	return toolset, nil
}
