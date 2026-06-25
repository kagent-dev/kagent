package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/constants"
	"github.com/kagent-dev/kagent/go/api/adk"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// DynamicHeaderProvider is a function that returns headers to inject into MCP requests.
// It receives the context and should return a map of headers.
// This is used for dynamic token injection (e.g., STS tokens) per session.
type DynamicHeaderProvider func(ctx context.Context) map[string]string

// TokenPrecedence selects how a static Authorization configured on an MCP
// server relates to a forwarded or STS-exchanged Authorization.
type TokenPrecedence int

const (
	// StaticTokenWins keeps a static Authorization at the highest precedence: it
	// overrides any forwarded or STS-exchanged Authorization. This is the default.
	StaticTokenWins TokenPrecedence = iota

	// ForwardedTokenWins lets a forwarded or STS-exchanged Authorization win over
	// a static Authorization. The displaced static token is sent as the actor
	// token (X-Actor-Token) so a downstream gateway can run an RFC 8693
	// delegation with subject=end user and actor=agent.
	ForwardedTokenWins
)

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
	TokenPrecedence       TokenPrecedence       // how a static Authorization relates to a forwarded/STS Authorization
	HeaderProvider        DynamicHeaderProvider // optional per-request headers derived from invocation context (e.g., STS exchanged access tokens)
	ServerType            string                // "http" or "sse"
	Timeout               *float64
	SseReadTimeout        *float64
	TLSInsecureSkipVerify *bool
	TLSCACertPath         *string
	TLSDisableSystemCAs   *bool
}

// CreateToolsets creates toolsets from all configured HTTP and SSE MCP servers,
// returning the accumulated toolsets. Errors on individual servers are logged
// and skipped.
//
// When propagateToken is true, Authorization is forwarded to every MCP server
// independently of AllowedHeaders, mirroring the Python ADKTokenPropagationPlugin
// behaviour triggered by KAGENT_PROPAGATE_TOKEN.
//
// tokenPrecedence is a runtime-global policy (KAGENT_PROPAGATE_TOKEN_OVERRIDES_STATIC)
// applied uniformly to every server here; see TokenPrecedence and applyStaticHeaders.
//
// Optional headerProvider can be used to inject per-request headers
// derived from invocation context (e.g., STS exchanged access tokens).
func CreateToolsets(
	ctx context.Context,
	httpTools []adk.HttpMcpServerConfig,
	sseTools []adk.SseMcpServerConfig,
	propagateToken bool,
	tokenPrecedence TokenPrecedence,
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
			TokenPrecedence:       tokenPrecedence,
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
			TokenPrecedence:       tokenPrecedence,
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

	if params.TokenPrecedence == ForwardedTokenWins &&
		params.TLSInsecureSkipVerify != nil && *params.TLSInsecureSkipVerify {
		log.Info("WARNING: ForwardedTokenWins sends the static M2M credential as X-Actor-Token, but TLS verification is disabled for this MCP server - the actor token can leak to an unverified endpoint", "url", params.URL)
	}

	var httpTransport http.RoundTripper = baseTransport
	if len(params.Headers) > 0 || len(params.AllowedHeaders) > 0 || params.PropagateToken || params.HeaderProvider != nil {
		httpTransport = &headerRoundTripper{
			base:            baseTransport,
			headers:         params.Headers,
			allowedHeaders:  params.AllowedHeaders,
			propagateToken:  params.PropagateToken,
			tokenPrecedence: params.TokenPrecedence,
			headerProvider:  params.HeaderProvider,
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
// requests. Header sources are applied lowest to highest precedence:
//  1. propagateToken: when true, Authorization is read from the incoming A2A
//     CallContext and forwarded unconditionally (independent of allowedHeaders).
//  2. allowedHeaders: explicit per-header forwarding from the A2A CallContext.
//  3. headerProvider: runtime headers derived from ADK context, such as STS tokens.
//  4. headers: static key/value pairs configured on the MCP server spec.
//
// Static headers (4) have the highest precedence; the one exception is the
// Authorization header under ForwardedTokenWins, resolved in applyStaticHeaders.
type headerRoundTripper struct {
	base            http.RoundTripper
	headers         map[string]string
	allowedHeaders  []string        // header names (case-insensitive) to forward from A2A context
	propagateToken  bool            // when true, Authorization is forwarded independently
	tokenPrecedence TokenPrecedence // resolves static vs forwarded Authorization
	headerProvider  DynamicHeaderProvider
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

	rt.applyStaticHeaders(req)

	return rt.base.RoundTrip(req)
}

// applyStaticHeaders writes the static headers configured on the MCP server spec
// onto req. Non-Authorization headers always overwrite forwarded values. The
// Authorization header honours tokenPrecedence: StaticTokenWins overwrites any
// forwarded token, while ForwardedTokenWins keeps a forwarded/STS token and, when
// it differs from the static one, carries the displaced static token as the actor
// (X-Actor-Token) for a downstream RFC 8693 delegation. With no forwarded token
// the static Authorization is applied and no actor is added; a forwarded token
// equal to the static one is treated as M2M (no actor); an actor token already
// forwarded via allowedHeaders is left untouched.
func (rt *headerRoundTripper) applyStaticHeaders(req *http.Request) {
	// headers is assumed to hold at most one Authorization key; with case-variant
	// duplicates map iteration order decides which wins.
	staticAuthorization := ""
	for key, value := range rt.headers {
		if strings.EqualFold(key, constants.AuthorizationHeader) {
			staticAuthorization = value
			continue
		}
		req.Header.Set(key, value)
	}

	if staticAuthorization == "" {
		return
	}

	if rt.tokenPrecedence == StaticTokenWins {
		req.Header.Set(constants.AuthorizationHeader, staticAuthorization)
		return
	}

	forwardedAuthorization := req.Header.Get(constants.AuthorizationHeader)
	if forwardedAuthorization == "" {
		req.Header.Set(constants.AuthorizationHeader, staticAuthorization)
		return
	}
	if forwardedAuthorization != staticAuthorization && req.Header.Get(constants.ActorTokenHeader) == "" {
		req.Header.Set(constants.ActorTokenHeader, staticAuthorization)
	}
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
