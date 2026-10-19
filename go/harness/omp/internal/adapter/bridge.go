package adapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kagent-dev/kagent/go/harness/omp/internal/config"
	"github.com/kagent-dev/kagent/go/harness/workspace"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const bridgeConnectTimeout = 30 * time.Second

// startBridges serves each server that names its tools on loopback with only
// those tools, and returns the URL omp reaches each one at. omp exposes every
// tool a server lists and has no allowlist of its own, so the bridge is the
// one place a binding's tool selection holds. The bridge calls the server
// with the binding's headers, so a Secret-backed header still leaves the
// Actor as the placeholder the egress gateway replaces; on one of the turn's
// actor-JWT hosts, a server without an Authorization of its own is called with
// the placeholder the gateway replaces with the Actor's JWT.
func startBridges(ctx context.Context, servers []config.MCPServer, actorJWT *turnActorJWTHosts) (map[string]string, error) {
	mux := http.NewServeMux()
	urls := map[string]string{}
	var bridges []*toolBridge
	for _, server := range servers {
		if len(server.Tools) == 0 {
			continue
		}
		bridge := newToolBridge(ctx, server, actorJWT)
		bridges = append(bridges, bridge)
		path := "/" + server.Name
		mux.Handle(path, mcp.NewStreamableHTTPHandler(bridge.serverFor, nil))
		urls[server.Name] = path
	}
	if len(urls) == 0 {
		return urls, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for the MCP tool bridge: %w", err)
	}
	for name, path := range urls {
		urls[name] = "http://" + listener.Addr().String() + path
	}
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logging.FromContext(ctx).ErrorContext(ctx, "mcp tool bridge stopped", "error", err)
		}
	}()
	// The bridges live as long as the adapter's context: the process.
	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
		for _, bridge := range bridges {
			bridge.close()
		}
	}()
	return urls, nil
}

// toolBridge holds one connection to its server, opened when omp first
// connects and opened again when the server has closed it.
type toolBridge struct {
	ctx      context.Context
	upstream config.MCPServer
	client   *mcp.Client
	http     *http.Client

	mu      sync.Mutex
	session *mcp.ClientSession
	server  *mcp.Server
}

func newToolBridge(ctx context.Context, upstream config.MCPServer, actorJWT *turnActorJWTHosts) *toolBridge {
	return &toolBridge{
		ctx: ctx, upstream: upstream,
		client: mcp.NewClient(&mcp.Implementation{Name: "kagent-omp", Version: "1"}, nil),
		http: &http.Client{Transport: headerTransport{
			url: upstream.URL, headers: upstream.Headers, actorJWT: actorJWT, base: http.DefaultTransport,
		}},
	}
}

// serverFor answers omp's sessions with the selected tools of the server,
// read once from its tools/list. Without a connection there is no server: omp
// reports the server unavailable and the next turn tries again.
func (b *toolBridge) serverFor(*http.Request) *mcp.Server {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.server != nil {
		return b.server
	}
	session, err := b.connectLocked()
	if err != nil {
		logging.FromContext(b.ctx).ErrorContext(b.ctx, "mcp tool bridge cannot reach its server", "server", b.upstream.Name, "error", err)
		return nil
	}
	selected := make(map[string]bool, len(b.upstream.Tools))
	for _, tool := range b.upstream.Tools {
		selected[tool] = true
	}
	server := mcp.NewServer(&mcp.Implementation{Name: b.upstream.Name, Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(b.ctx, bridgeConnectTimeout)
	defer cancel()
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			logging.FromContext(b.ctx).ErrorContext(b.ctx, "mcp tool bridge cannot list tools", "server", b.upstream.Name, "error", err)
			return nil
		}
		if selected[tool.Name] {
			server.AddTool(tool, b.call)
		}
	}
	b.server = server
	return server
}

// call forwards one call of a selected tool. Only selected tools are on the
// bridge's server, so no other call reaches this handler.
func (b *toolBridge) call(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if b.upstream.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(b.upstream.TimeoutSeconds*float64(time.Second)))
		defer cancel()
	}
	params := &mcp.CallToolParams{Name: request.Params.Name, Arguments: request.Params.Arguments}
	session, err := b.connection()
	if err != nil {
		return nil, err
	}
	result, err := session.CallTool(ctx, params)
	if errors.Is(err, mcp.ErrConnectionClosed) {
		if session, err = b.reconnect(session); err != nil {
			return nil, err
		}
		result, err = session.CallTool(ctx, params)
	}
	return result, err
}

func (b *toolBridge) connection() (*mcp.ClientSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		return b.session, nil
	}
	return b.connectLocked()
}

// reconnect replaces a closed connection unless another call already has.
func (b *toolBridge) reconnect(closed *mcp.ClientSession) (*mcp.ClientSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != closed {
		return b.session, nil
	}
	b.session = nil
	return b.connectLocked()
}

func (b *toolBridge) connectLocked() (*mcp.ClientSession, error) {
	if b.session != nil {
		return b.session, nil
	}
	ctx, cancel := context.WithTimeout(b.ctx, bridgeConnectTimeout)
	defer cancel()
	session, err := b.client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: b.upstream.URL, HTTPClient: b.http}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MCP server %q: %w", b.upstream.Name, err)
	}
	b.session = session
	return session, nil
}

func (b *toolBridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		_ = b.session.Close()
		b.session = nil
	}
}

// headerTransport sets a binding's headers on every request to its server,
// and the placeholder Authorization while the server takes the current
// turn's Actor JWT.
type headerTransport struct {
	url      string
	headers  map[string]string
	actorJWT *turnActorJWTHosts
	base     http.RoundTripper
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for name, value := range workspace.ActorJWTHeaders(t.url, t.headers, t.actorJWT.get()) {
		request.Header.Set(name, value)
	}
	return t.base.RoundTrip(request)
}

// turnActorJWTHosts holds the actor-JWT hosts of the turn omp runs. The
// bridges start with the adapter, and the hosts, the Session's, reach it only
// with each turn's call.
type turnActorJWTHosts struct {
	hosts atomic.Pointer[[]string]
}

func (h *turnActorJWTHosts) set(hosts []string) { h.hosts.Store(&hosts) }

func (h *turnActorJWTHosts) get() []string {
	if hosts := h.hosts.Load(); hosts != nil {
		return *hosts
	}
	return nil
}
