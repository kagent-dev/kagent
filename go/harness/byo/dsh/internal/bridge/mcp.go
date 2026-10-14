package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
)

// mcpProxy keeps kagent's HTTP timeout and session-close policy outside ACP,
// whose MCP declarations cannot carry those settings.
type mcpProxy struct {
	server     *http.Server
	done       chan error
	cancel     context.CancelFunc
	sessions   []*mcp.ClientSession
	transports []*http.Transport
	servers    []config.MCPServer
}

func startMCP(ctx context.Context, cfg *config.Config, bindings []config.MCPServer) (_ *mcpProxy, returnErr error) {
	root, cancel := context.WithCancel(context.WithoutCancel(ctx))
	p := &mcpProxy{cancel: cancel}
	defer func() {
		if recovered := recover(); recovered != nil {
			returnErr = fmt.Errorf("failed to initialize MCP proxy: %v", recovered)
		}
		if returnErr != nil {
			returnErr = errors.Join(returnErr, p.close())
		}
	}()
	mux := http.NewServeMux()
	for i, binding := range bindings {
		params := cfg.Agent.HttpTools[i].Params
		timeout := 30 * time.Second
		if params.Timeout != nil {
			timeout = time.Duration(*params.Timeout * float64(time.Second))
		}
		terminate := params.TerminateOnClose == nil || *params.TerminateOnClose
		transport := http.DefaultTransport.(*http.Transport).Clone()
		p.transports = append(p.transports, transport)
		httpClient := &http.Client{Timeout: timeout, Transport: headerTransport{base: transport, headers: binding.Headers, terminate: terminate}}
		connectCtx, connectCancel := context.WithTimeout(ctx, timeout)
		// AddTool can panic on an invalid remote schema; release the discovery
		// deadline even when the startup recovery above handles that panic.
		defer connectCancel()
		session, err := mcp.NewClient(&mcp.Implementation{Name: "kagent-dsh-byo", Version: "1"}, nil).Connect(connectCtx, &mcp.StreamableClientTransport{Endpoint: binding.URL, HTTPClient: httpClient}, nil)
		if err != nil {
			connectCancel()
			return nil, fmt.Errorf("failed to connect MCP binding %d: %w", i, err)
		}
		p.sessions = append(p.sessions, session)
		server := mcp.NewServer(&mcp.Implementation{Name: binding.Name, Version: "1"}, nil)
		cursor := ""
		seenCursors := make(map[string]struct{})
		for {
			listed, err := session.ListTools(connectCtx, &mcp.ListToolsParams{Cursor: cursor})
			if err != nil {
				connectCancel()
				return nil, fmt.Errorf("failed to discover MCP binding %d: %w", i, err)
			}
			for _, tool := range listed.Tools {
				name := tool.Name
				server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					callCtx, callCancel := context.WithTimeout(ctx, timeout)
					defer callCancel()
					return session.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: request.Params.Arguments})
				})
			}
			if listed.NextCursor == "" {
				break
			}
			if _, exists := seenCursors[listed.NextCursor]; exists {
				connectCancel()
				return nil, fmt.Errorf("MCP binding %d repeated a discovery cursor", i)
			}
			cursor = listed.NextCursor
			seenCursors[cursor] = struct{}{}
		}
		connectCancel()
		path := fmt.Sprintf("/mcp/%d", i)
		mux.Handle(path, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
		p.servers = append(p.servers, config.MCPServer{Type: "http", Name: binding.Name, URL: path, Headers: []config.Header{}})
	}
	if len(bindings) == 0 {
		return p, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen for local MCP bridge: %w", err)
	}
	for i := range p.servers {
		p.servers[i].URL = "http://" + listener.Addr().String() + p.servers[i].URL
	}
	p.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return root }}
	p.done = make(chan error, 1)
	go func() { p.done <- p.server.Serve(listener) }()
	return p, nil
}

func (p *mcpProxy) close() error {
	p.cancel()
	var err error
	if p.server != nil {
		err = errors.Join(err, p.server.Close())
		if serveErr := <-p.done; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
	}
	for _, session := range p.sessions {
		err = errors.Join(err, session.Close())
	}
	for _, transport := range p.transports {
		transport.CloseIdleConnections()
	}
	return err
}

type headerTransport struct {
	base      http.RoundTripper
	headers   []config.Header
	terminate bool
}

var _ http.RoundTripper = headerTransport{}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodDelete && !t.terminate {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	request = request.Clone(request.Context())
	for _, header := range t.headers {
		request.Header.Set(header.Name, header.Value)
	}
	return t.base.RoundTrip(request)
}
