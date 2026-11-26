package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

const (
	requestMetadataSessionKey     = "com.example/session"
	requestMetadataConfigKey      = "com.example/config"
	metadataLegacyProtocolVersion = "2025-11-25"
	metadataModernProtocolVersion = "2026-07-28"
)

func TestRequestMetadataForwardsArbitraryMCPMetadata(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		t.Run(fmt.Sprintf("Given stateless=%t, when a tool is called, assert that the metadata is forwarded without an extra carrier", stateless), func(t *testing.T) {
			serverURL, received := metadataServer(t, stateless, nil)
			call := metadataTool(t, serverURL)
			values := map[string]any{
				requestMetadataSessionKey: "a1",
				requestMetadataConfigKey:  map[string]any{"format": "csv", "limit": 10.0, "any nested/key": true},
				"format":                  "csv",
			}
			require.NoError(t, call(t.Context(), values))
			record := <-received
			wantVersion := metadataLegacyProtocolVersion
			if stateless {
				wantVersion = metadataModernProtocolVersion
			}
			require.Equal(t, wantVersion, record.version)
			for key, value := range values {
				require.Equal(t, value, record.meta[key], key)
			}
			require.NotNil(t, record.capabilities)
			require.Contains(t, record.capabilities.Extensions, mcpUIExtensionName)
			require.Len(t, record.capabilities.Extensions, 1, "forwarding metadata does not declare extension support")
			require.NotContains(t, record.capabilities.Extensions, requestMetadataSessionKey)
			require.Len(t, values, 3)
		})
	}
}

func TestRequestMetadataDoesNotRequireAnMCPCapability(t *testing.T) {
	for name, extensions := range map[string]map[string]any{
		"no extension":        nil,
		"unrelated extension": {requestMetadataSessionKey: map[string]any{}},
	} {
		t.Run("Given "+name+", when a tool is called, assert that metadata is forwarded without application capability checks", func(t *testing.T) {
			serverURL, received := metadataServer(t, true, extensions)
			call := metadataTool(t, serverURL)
			require.NoError(t, call(t.Context(), map[string]any{requestMetadataSessionKey: "private"}))
			require.Equal(t, "private", (<-received).meta[requestMetadataSessionKey])
		})
	}
}

func TestRequestMetadataIsScopedToEachCall(t *testing.T) {
	serverURL, received := metadataServer(t, true, nil)
	call := metadataTool(t, serverURL)
	for _, value := range []string{"first", "second", ""} {
		var values map[string]any
		if value != "" {
			values = map[string]any{requestMetadataSessionKey: value}
		}
		require.NoError(t, call(t.Context(), values))
		record := <-received
		if value == "" {
			require.NotContains(t, record.meta, requestMetadataSessionKey)
		} else {
			require.Equal(t, value, record.meta[requestMetadataSessionKey])
		}
	}
}

func TestConcurrentToolCallsKeepTheirOwnMetadata(t *testing.T) {
	serverURL, received := metadataServer(t, true, nil)
	call := metadataTool(t, serverURL)
	const calls = 12
	var workers sync.WaitGroup
	errors := make(chan error, calls)
	for index := range calls {
		workers.Go(func() {
			errors <- call(t.Context(), map[string]any{requestMetadataSessionKey: fmt.Sprintf("call-%d", index)})
		})
	}
	workers.Wait()
	seen := make(map[string]bool, calls)
	for range calls {
		require.NoError(t, <-errors)
		value, ok := (<-received).meta[requestMetadataSessionKey].(string)
		require.True(t, ok)
		require.False(t, seen[value], "duplicate metadata %q", value)
		seen[value] = true
	}
	for index := range calls {
		require.True(t, seen[fmt.Sprintf("call-%d", index)])
	}
}

func TestRequestMetadataPreservesExistingCallMetadata(t *testing.T) {
	serverURL, received := metadataServer(t, true, nil)
	client := newMCPAppClient(nil, &mcpAppDiscovery{listedAppNames: map[string]bool{}, appToolNames: map[string]bool{}})
	session, err := client.Connect(t.Context(), &mcpsdk.StreamableClientTransport{Endpoint: serverURL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, session.Close()) })
	metadata := mcpsdk.Meta{
		requestMetadataSessionKey: "existing",
		"org.other/setting":       true,
	}
	_, err = session.CallTool(WithRequestMetadata(t.Context(), map[string]any{requestMetadataSessionKey: "replacement"}), &mcpsdk.CallToolParams{
		Name: "echo", Meta: metadata,
	})
	require.NoError(t, err)
	record := <-received
	require.Equal(t, "existing", record.meta[requestMetadataSessionKey])
	require.Equal(t, true, record.meta["org.other/setting"])
	require.Equal(t, metadataModernProtocolVersion, record.meta[mcpsdk.MetaKeyProtocolVersion])
}

func TestRequestMetadataDoesNotReachDiscovery(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		t.Run(fmt.Sprintf("Given stateless=%t, when a request discovers and calls a tool, assert that only the tool call carries its metadata", stateless), func(t *testing.T) {
			type record struct {
				method string
				meta   mcpsdk.Meta
			}
			received := make(chan record, 20)
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "metadata", Version: "1"}, nil)
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo"}, func(context.Context, *mcpsdk.CallToolRequest, struct{}) (*mcpsdk.CallToolResult, any, error) {
				return &mcpsdk.CallToolResult{}, nil, nil
			})
			server.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
				return func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
					var metadata mcpsdk.Meta
					if params := request.GetParams(); params != nil && !reflect.ValueOf(params).IsNil() {
						metadata = params.GetMeta()
					}
					received <- record{method: method, meta: metadata}
					return next(ctx, method, request)
				}
			})
			httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless}))
			t.Cleanup(httpServer.Close)
			ctx := WithRequestMetadata(t.Context(), map[string]any{requestMetadataSessionKey: "request-only"})
			client := newMCPAppClient(nil, &mcpAppDiscovery{listedAppNames: map[string]bool{}, appToolNames: map[string]bool{}})
			session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, session.Close()) })
			_, err = session.ListTools(ctx, nil)
			require.NoError(t, err)
			_, err = session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "echo"})
			require.NoError(t, err)
			seen := map[string]bool{}
			for len(received) > 0 {
				observation := <-received
				seen[observation.method] = true
				if observation.method == "tools/call" {
					require.Equal(t, "request-only", observation.meta[requestMetadataSessionKey])
				} else {
					require.NotContains(t, observation.meta, requestMetadataSessionKey, observation.method)
				}
			}
			require.True(t, seen["tools/list"])
			require.True(t, seen["tools/call"])
		})
	}
}

func TestRequestMetadataCannotOverrideClientProtocolFields(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		for _, key := range []string{mcpsdk.MetaKeyProtocolVersion, mcpsdk.MetaKeyClientInfo, mcpsdk.MetaKeyClientCapabilities, "progressToken"} {
			t.Run(fmt.Sprintf("Given stateless=%t, when metadata supplies %s, assert that the call is refused before dispatch", stateless, key), func(t *testing.T) {
				serverURL, received := metadataServer(t, stateless, nil)
				call := metadataTool(t, serverURL)
				require.Error(t, call(t.Context(), map[string]any{key: "caller-controlled"}))
				require.Empty(t, received)
			})
		}
	}
}

type metadataRecord struct {
	meta         mcpsdk.Meta
	version      string
	capabilities *mcpsdk.ClientCapabilities
}

func metadataServer(t *testing.T, stateless bool, extensions map[string]any) (string, <-chan metadataRecord) {
	t.Helper()
	received := make(chan metadataRecord, 20)
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "metadata", Version: "1"}, &mcpsdk.ServerOptions{
		Capabilities: &mcpsdk.ServerCapabilities{Extensions: extensions},
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		received <- metadataRecord{meta: request.Params.GetMeta(), version: request.ProtocolVersion(), capabilities: request.ClientCapabilities()}
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "ok"}}}, nil, nil
	})
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless}))
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	return httpServer.URL, received
}

func metadataTool(t *testing.T, serverURL string) func(context.Context, map[string]any) error {
	t.Helper()
	toolset, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcpsdk.StreamableClientTransport{Endpoint: serverURL},
		Client:    newMCPAppClient(nil, &mcpAppDiscovery{listedAppNames: map[string]bool{}, appToolNames: map[string]bool{}}),
	})
	require.NoError(t, err)
	closer, ok := toolset.(io.Closer)
	require.True(t, ok)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	tools, err := toolset.Tools(metadataCallContext{ctx: t.Context()})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	runnable, ok := tools[0].(interface {
		Run(adkagent.Context, any) (map[string]any, error)
	})
	require.True(t, ok)
	return func(ctx context.Context, values map[string]any) error {
		_, err := runnable.Run(metadataCallContext{ctx: WithRequestMetadata(ctx, values)}, map[string]any{})
		return err
	}
}

type metadataCallContext struct {
	adkagent.Context
	ctx context.Context
}

func (c metadataCallContext) Deadline() (time.Time, bool)                          { return c.ctx.Deadline() }
func (c metadataCallContext) Done() <-chan struct{}                                { return c.ctx.Done() }
func (c metadataCallContext) Err() error                                           { return c.ctx.Err() }
func (c metadataCallContext) Value(key any) any                                    { return c.ctx.Value(key) }
func (c metadataCallContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
