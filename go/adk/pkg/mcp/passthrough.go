package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolutils"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// passthroughToolset keeps only configuration, never a client session or caller
// credential. Each operation owns an ordinary ADK toolset and its connections.
type passthroughToolset struct {
	params mcpServerParams
	filter map[string]bool
}

func (*passthroughToolset) Name() string { return "mcp_tool_set" }

// passthroughTransport observes connections without wrapping them: the SDK uses
// private connection hooks to update session identity and protocol negotiation.
// ADK connects synchronously, including retries, within the owning operation.
type passthroughTransport struct {
	params      mcpServerParams
	connections []mcpsdk.Connection
}

func (p *passthroughTransport) Connect(ctx context.Context) (mcpsdk.Connection, error) {
	// SDK transports are single-use; ADK may reconnect through this wrapper.
	transport, err := createTransport(ctx, p.params)
	if err != nil {
		return nil, err
	}
	conn, err := transport.Connect(ctx)
	if conn != nil {
		p.connections = append(p.connections, conn)
	}
	return conn, err
}

func (p *passthroughTransport) close(ctx context.Context) {
	for _, conn := range p.connections {
		// Cancellation alone does not stop the SDK's detached background work.
		// Close is idempotent, including after failed initialization or retries.
		if err := conn.Close(); err != nil {
			logging.FromContext(ctx).WarnContext(ctx, "failed to close passthrough MCP connection")
		}
	}
	p.connections = nil
}

func (p *passthroughToolset) tools(ctx adkagent.ReadonlyContext, transport *passthroughTransport) ([]tool.Tool, error) {
	if _, ok := models.PassthroughToken(ctx, true); !ok {
		return nil, fmt.Errorf("MCP bearer token passthrough requires an authenticated invocation bearer token")
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent", Version: "1"},
		&mcpsdk.ClientOptions{Capabilities: mcpUIClientCapabilities()})
	// ADK drops MCP metadata during conversion. Filter each discovery page at
	// the SDK boundary, before delegating declarations and results to ADK.
	var listedSession mcpsdk.Session
	client.AddSendingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/call" && req.GetSession() != listedSession {
				// A native ADK call can reconnect after discovery. Recheck access
				// on the replacement session before allowing the retried call.
				allowed := false
				for item, err := range req.GetSession().(*mcpsdk.ClientSession).Tools(ctx, nil) {
					if err != nil {
						return nil, err
					}
					if item.Name == req.GetParams().(*mcpsdk.CallToolParams).Name {
						allowed = true
					}
				}
				if !allowed {
					return nil, fmt.Errorf("passthrough MCP tool is no longer available")
				}
			}
			result, err := next(ctx, method, req)
			if err == nil && method == "tools/list" {
				list := result.(*mcpsdk.ListToolsResult)
				list.Tools = slices.DeleteFunc(list.Tools, func(item *mcpsdk.Tool) bool {
					return item == nil || len(p.filter) > 0 && !p.filter[item.Name] ||
						mcpToolKindOf(item.Meta) == mcpToolKindAppInternal
				})
				listedSession = req.GetSession()
			}
			return result, err
		}
	})
	native, err := mcptoolset.New(mcptoolset.Config{Client: client, Transport: transport})
	if err != nil {
		return nil, passthroughError(err)
	}
	tools, err := native.Tools(ctx)
	if err != nil {
		return nil, passthroughError(err)
	}
	return tools, nil
}

func (p *passthroughToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	transport := &passthroughTransport{params: p.params}
	defer transport.close(ctx)
	tools, err := p.tools(ctx, transport)
	if err != nil {
		return nil, err
	}
	for i, t := range tools {
		// Keep only ADK's declaration, not its tool/client and closed session
		// (which can still retain the discovery caller's context).
		tools[i] = &passthroughTool{set: p, declaration: t.(functionTool).Declaration()}
	}
	return tools, nil
}

type passthroughTool struct {
	set         *passthroughToolset
	declaration *genai.FunctionDeclaration
}

func (p *passthroughTool) Name() string                            { return p.declaration.Name }
func (p *passthroughTool) Description() string                     { return p.declaration.Description }
func (*passthroughTool) IsLongRunning() bool                       { return false }
func (p *passthroughTool) Declaration() *genai.FunctionDeclaration { return p.declaration }
func (p *passthroughTool) ProcessRequest(_ adkagent.Context, req *model.LLMRequest) error {
	return toolutils.PackTool(req, p)
}

func (p *passthroughTool) Run(ctx adkagent.Context, args any) (map[string]any, error) {
	transport := &passthroughTransport{params: p.set.params}
	defer transport.close(ctx)
	tools, err := p.set.tools(ctx, transport)
	if err != nil {
		return nil, err
	}
	for _, t := range tools {
		if t.Name() == p.Name() {
			result, err := t.(functionTool).Run(ctx, args)
			if err != nil {
				return nil, passthroughError(err)
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("passthrough MCP tool is no longer available")
}

// Upstream errors may contain server content or credentials. Preserve only
// framework classifications, never the original error or its wrapping text.
func passthroughError(err error) error {
	for _, safe := range []error{
		context.Canceled, context.DeadlineExceeded,
		tool.ErrConfirmationRequired, tool.ErrConfirmationRejected,
	} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return fmt.Errorf("passthrough MCP operation failed")
}

var _ mcpsdk.Transport = (*passthroughTransport)(nil)
var _ tool.Toolset = (*passthroughToolset)(nil)
var _ functionTool = (*passthroughTool)(nil)
