package driver

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/kagent-dev/kagent/go/harness/codex/config"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// ToolCaller executes the exact saved MCP operation after human approval.
type ToolCaller interface {
	Call(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error)
}

// toolNotExecutedError proves that no tools/call request was sent. Other call
// errors remain uncertain because a remote side effect may have occurred.
type toolNotExecutedError struct{ err error }

func (e *toolNotExecutedError) Error() string { return e.err.Error() }
func (e *toolNotExecutedError) Unwrap() error { return e.err }

type MCPCaller struct {
	servers     map[string]config.MCPServer
	environment map[string]string
}

func NewMCPCaller(servers map[string]config.MCPServer, environment []string) *MCPCaller {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	return &MCPCaller{servers: servers, environment: values}
}

func (c *MCPCaller) Call(ctx context.Context, server, tool string, args map[string]any) (result *mcp.CallToolResult, returnErr error) {
	called := false
	defer func() {
		if !called && returnErr != nil {
			returnErr = &toolNotExecutedError{err: returnErr}
		}
	}()
	cfg, ok := c.servers[server]
	if !ok || !cfg.RequireApproval || len(cfg.EnabledTools) > 0 && !slices.Contains(cfg.EnabledTools, tool) {
		return nil, fmt.Errorf("deferred MCP tool %s.%s is not configured for approval", server, tool)
	}
	headers := make(http.Header, len(cfg.Headers))
	for name, value := range cfg.Headers {
		if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
			variable := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
			var present bool
			value, present = c.environment[variable]
			if !present || value == "" {
				return nil, fmt.Errorf("deferred MCP header environment %q is unavailable", variable)
			}
		}
		headers.Set(name, value)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "kagent-codex", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: cfg.URL,
		HTTPClient: &http.Client{Transport: otelhttp.NewTransport(&mcpHeaderTransport{headers: headers}),
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect deferred MCP server: %w", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			// Session termination is separate from the known tool outcome. In
			// particular, a failed DELETE must not discard a received result.
			logging.FromContext(ctx).ErrorContext(ctx, "failed to close deferred mcp session", "server", server, "error", err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	called = true
	return session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
}

type mcpHeaderTransport struct {
	headers http.Header
}

func (t *mcpHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for name, values := range t.headers {
		request.Header[name] = slices.Clone(values)
	}
	return http.DefaultTransport.RoundTrip(request)
}

var _ ToolCaller = (*MCPCaller)(nil)
var _ http.RoundTripper = (*mcpHeaderTransport)(nil)
