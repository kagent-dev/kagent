package declarative

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"

	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/workflow"
)

// MCPServer is a declared MCP server with its sources resolved.
type MCPServer struct {
	Name    string
	URL     string
	Headers map[string]string
	// Tools is the allowlist; empty allows every tool.
	Tools []string
}

// MCPConnector creates the toolset of a declared MCP server. It must not
// contact the server: tools are listed when an agent or node first needs
// them, so a server that is down fails a run, not the build.
type MCPConnector func(ctx context.Context, server MCPServer) (tool.Toolset, error)

// SetMCPConnector replaces the default streamable HTTP connector, for example
// with one that adds per-request credentials.
func (r *Registry) SetMCPConnector(c MCPConnector) { r.mcpConnector = c }

// ConnectMCP is the default MCPConnector: an ADK MCP toolset over streamable
// HTTP that sends the server's headers and keeps only allowlisted tools.
func ConnectMCP(_ context.Context, server MCPServer) (tool.Toolset, error) {
	transport := &mcpsdk.StreamableClientTransport{Endpoint: server.URL}
	if len(server.Headers) > 0 {
		transport.HTTPClient = &http.Client{Transport: &headerTransport{headers: server.Headers, base: http.DefaultTransport}}
	}
	ts, err := mcptoolset.New(mcptoolset.Config{Transport: transport})
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP toolset %q: %w", server.Name, err)
	}
	if len(server.Tools) > 0 {
		ts = tool.FilterToolset(ts, tool.StringPredicate(server.Tools))
	}
	return &serverToolset{Toolset: ts, name: server.Name}, nil
}

type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// serverToolset reports the document's server name, so traces and errors name
// the server the author declared.
type serverToolset struct {
	tool.Toolset
	name string
}

func (n *serverToolset) Name() string { return n.name }

func (n *serverToolset) Close() error {
	if c, ok := n.Toolset.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// resolveMCPServer resolves the env sources of a declared server.
func resolveMCPServer(s *MCPServerSpec) (MCPServer, error) {
	url, err := sourcedText(s.URL, s.URLFrom)
	if err != nil {
		return MCPServer{}, fmt.Errorf("url_from: %w", err)
	}
	headers := make(map[string]string, len(s.Headers))
	for _, h := range s.Headers {
		v, err := sourcedText(h.Value, h.ValueFrom)
		if err != nil {
			return MCPServer{}, fmt.Errorf("headers[%s]: %w", h.Name, err)
		}
		headers[h.Name] = v
	}
	return MCPServer{Name: s.Name, URL: url, Headers: headers, Tools: slices.Clone(s.Tools)}, nil
}

// connectMCPServers creates the toolsets of the document's servers.
func (b *builder) connectMCPServers(servers []MCPServerSpec) error {
	connect := b.reg.mcpConnector
	if connect == nil {
		connect = ConnectMCP
	}
	for i := range servers {
		s := &servers[i]
		resolved, err := resolveMCPServer(s)
		if err != nil {
			return fmt.Errorf("mcp server %q: %w", s.Name, err)
		}
		ts, err := connect(b.ctx, resolved)
		if err != nil {
			return fmt.Errorf("mcp server %q: %w", s.Name, err)
		}
		if ts == nil {
			return fmt.Errorf("mcp server %q: connector returned no toolset", s.Name)
		}
		b.servers[s.Name] = ts
	}
	return nil
}

// toolset returns a declared server, or a registered toolset of that name.
func (b *builder) toolset(name string) (tool.Toolset, bool) {
	if ts, ok := b.servers[name]; ok {
		return ts, true
	}
	ts, ok := b.reg.toolsets[name]
	return ts, ok
}

// runnableTool is the interface the ADK workflow engine uses to call a tool
// outside a model turn; MCP and function tools implement it.
type runnableTool interface {
	Run(ctx agent.Context, args any) (map[string]any, error)
}

// mcpToolNode calls one tool of a toolset. The toolset lists its tools only
// when the node first runs; a successful lookup is cached, a failed one is
// retried by the next run so a server that was briefly down recovers.
func (b *builder) mcpToolNode(n *NodeSpec, in, out *jsonschema.Schema, cfg workflow.NodeConfig) (workflow.Node, error) {
	ts, ok := b.toolset(n.Server)
	if !ok {
		return nil, fmt.Errorf("server %q is not declared or registered", n.Server)
	}
	var (
		mu     sync.Mutex
		cached runnableTool
	)
	lookup := func(ctx agent.Context) (runnableTool, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached != nil {
			return cached, nil
		}
		tools, err := ts.Tools(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list tools of %q: %w", n.Server, err)
		}
		for _, t := range tools {
			if t.Name() != n.Tool {
				continue
			}
			r, ok := t.(runnableTool)
			if !ok {
				return nil, fmt.Errorf("tool %q of %q (%T) cannot run outside a model turn", n.Tool, n.Server, t)
			}
			cached = r
			return r, nil
		}
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name())
		}
		slices.Sort(names)
		return nil, fmt.Errorf("server %q has no tool %q (has %v)", n.Server, n.Tool, names)
	}
	body := func(ctx agent.Context, input any) (any, error) {
		t, err := lookup(ctx)
		if err != nil {
			return nil, err
		}
		args, err := toolArgs(input)
		if err != nil {
			return nil, fmt.Errorf("tool %q of %q: %w", n.Tool, n.Server, err)
		}
		res, err := t.Run(ctx, args)
		if err != nil {
			return nil, fmt.Errorf("tool %q of %q: %w", n.Tool, n.Server, err)
		}
		return res, nil
	}
	return workflow.NewFunctionNodeWithSchema[any, any](n.ID, body, in, out, cfg)
}

// toolArgs turns a predecessor's output into tool arguments the way the ADK
// tool node does: a map as is, a JSON object string decoded, nothing as {}.
func toolArgs(input any) (map[string]any, error) {
	switch v := input.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err != nil || m == nil {
			return nil, errors.New("input must be an object or a JSON object string")
		}
		return m, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("input is not JSON: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return nil, fmt.Errorf("input must be an object, got %T", input)
	}
	return m, nil
}

// Close releases the connections of the document's MCP servers.
func (s *System) Close() error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(s.servers)) {
		if c, ok := s.servers[name].(io.Closer); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, fmt.Errorf("mcp server %q: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}
