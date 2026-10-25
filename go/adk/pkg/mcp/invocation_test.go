package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/adk/pkg/auth"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

type invocationReadonlyContext struct {
	testReadonlyContext
	invocation, session string
}

func (c invocationReadonlyContext) InvocationID() string { return c.invocation }
func (c invocationReadonlyContext) SessionID() string    { return c.session }

func mcpInvocation(t *testing.T, invocation, caller string) (invocationReadonlyContext, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	ctx, _ = a2asrv.NewCallContext(ctx, a2asrv.NewServiceParams(map[string][]string{
		"Authorization": {"Bearer " + caller},
	}))
	return invocationReadonlyContext{
		testReadonlyContext: testReadonlyContext{Context: auth.WithUserID(ctx, caller)},
		invocation:          invocation,
		session:             "shared-session",
	}, cancel
}

type invocationTraffic struct {
	mu                sync.Mutex
	initializations   []string
	lists             int
	deletes           int
	failInitialize    bool
	initializeStarted chan struct{}
	allowInitialize   <-chan struct{}
}

func invocationMCPServer(t *testing.T, traffic *invocationTraffic, useTLS bool) *httptest.Server {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "invocation-test", Version: "1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami"}, func(context.Context, *mcpsdk.CallToolRequest, struct{}) (*mcpsdk.CallToolResult, any, error) {
		return &mcpsdk.CallToolResult{}, nil, nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	endpoint := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message struct {
			Method string `json:"method"`
		}
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err := json.Unmarshal(body, &message); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		traffic.mu.Lock()
		switch message.Method {
		case "initialize":
			traffic.initializations = append(traffic.initializations, r.Header.Get("Authorization"))
		case "tools/list":
			traffic.lists++
		}
		if r.Method == http.MethodDelete {
			traffic.deletes++
		}
		fail := traffic.failInitialize && message.Method == "initialize"
		traffic.mu.Unlock()
		if message.Method == "initialize" && traffic.allowInitialize != nil {
			select {
			case traffic.initializeStarted <- struct{}{}:
			default:
			}
			select {
			case <-traffic.allowInitialize:
			case <-r.Context().Done():
				return
			}
		}
		if fail {
			http.Error(w, "initialization unavailable", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	if useTLS {
		endpoint.StartTLS()
	} else {
		endpoint.Start()
	}
	t.Cleanup(endpoint.Close)
	t.Cleanup(endpoint.CloseClientConnections)
	return endpoint
}

func invocationPassthroughToolset(url string) *invocationToolset {
	return &invocationToolset{params: mcpServerParams{
		URL: url, ServerType: "http",
		PassthroughHeaders: []*configv1.InvocationCredentialHeader{{Name: "Authorization", Prefix: "Bearer "}},
	}}
}

func TestInvocationToolsetReusesConnectionAndClosesAtInvocationEnd(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "classified", true: "passthrough"}[passthrough], func(t *testing.T) {
			traffic := &invocationTraffic{}
			server := invocationMCPServer(t, traffic, false)
			toolset := invocationPassthroughToolset(server.URL)
			if !passthrough {
				toolset.params.PassthroughHeaders = nil
			}
			ctx, cancel := mcpInvocation(t, "invocation-1", "alice")
			for range 3 {
				tools, err := toolset.Tools(ctx)
				require.NoError(t, err)
				require.Len(t, tools, 1)
				require.Equal(t, "whoami", tools[0].Name())
			}
			wantConnections, wantLists := 1, 3
			if !passthrough {
				// Classification uses one short-lived connection, once per invocation.
				wantConnections++
				wantLists++
			}
			traffic.mu.Lock()
			connections, lists := len(traffic.initializations), traffic.lists
			traffic.mu.Unlock()
			require.Equal(t, wantConnections, connections)
			require.Equal(t, wantLists, lists)

			cancel()
			require.Eventually(t, func() bool {
				toolset.mu.Lock()
				defer toolset.mu.Unlock()
				traffic.mu.Lock()
				defer traffic.mu.Unlock()
				return len(toolset.invocations) == 0 && traffic.deletes == wantConnections
			}, 5*time.Second, time.Millisecond)
			_, err := toolset.Tools(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestInvocationToolsetSeparatesCallersAndInvocations(t *testing.T) {
	traffic := &invocationTraffic{}
	server := invocationMCPServer(t, traffic, false)
	toolset := invocationPassthroughToolset(server.URL)
	first, _ := mcpInvocation(t, "shared-id", "alice")
	otherCaller, _ := mcpInvocation(t, "shared-id", "bob")
	otherInvocation, _ := mcpInvocation(t, "next-id", "alice")
	otherSession, _ := mcpInvocation(t, "shared-id", "alice")
	otherSession.session = "other-session"
	for _, ctx := range []invocationReadonlyContext{first, otherCaller, otherInvocation, otherSession, first} {
		tools, err := toolset.Tools(ctx)
		require.NoError(t, err)
		require.Len(t, tools, 1)
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	require.Equal(t, []string{"Bearer alice", "Bearer bob", "Bearer alice", "Bearer alice"}, traffic.initializations)
}

func TestInvocationToolsetConcurrentToolsShareInitialization(t *testing.T) {
	traffic := &invocationTraffic{}
	server := invocationMCPServer(t, traffic, false)
	toolset := invocationPassthroughToolset(server.URL)
	ctx, _ := mcpInvocation(t, "concurrent", "alice")
	var callers sync.WaitGroup
	errors := make(chan error, 8)
	for range cap(errors) {
		callers.Go(func() {
			_, err := toolset.Tools(ctx)
			errors <- err
		})
	}
	callers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	require.Equal(t, []string{"Bearer alice"}, traffic.initializations)
	require.Equal(t, 8, traffic.lists)
}

func TestInvocationToolsetRetriesFailedMCPInitialization(t *testing.T) {
	traffic := &invocationTraffic{failInitialize: true}
	server := invocationMCPServer(t, traffic, false)
	toolset := invocationPassthroughToolset(server.URL)
	ctx, _ := mcpInvocation(t, "retry", "alice")
	_, err := toolset.Tools(ctx)
	require.Error(t, err)
	traffic.mu.Lock()
	traffic.failInitialize = false
	traffic.mu.Unlock()
	tools, err := toolset.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	require.Equal(t, []string{"Bearer alice", "Bearer alice"}, traffic.initializations)
}

func TestInvocationToolsetWaitingCallerCanCancel(t *testing.T) {
	allowInitialize := make(chan struct{})
	traffic := &invocationTraffic{
		initializeStarted: make(chan struct{}, 1), allowInitialize: allowInitialize,
	}
	server := invocationMCPServer(t, traffic, false)
	toolset := &invocationToolset{params: mcpServerParams{URL: server.URL, ServerType: "http"}}
	ctx, _ := mcpInvocation(t, "waiting", "alice")
	completed := make(chan error, 1)
	go func() {
		_, err := toolset.Tools(ctx)
		completed <- err
	}()
	select {
	case <-traffic.initializeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("MCP classification did not initialize")
	}
	waiter := ctx
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	waiter.Context = deadline
	_, err := toolset.Tools(waiter)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(allowInitialize)
	select {
	case err := <-completed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("MCP initialization did not finish after the waiter canceled")
	}
}

func TestInvocationToolsetRetriesFailedTransportInitialization(t *testing.T) {
	traffic := &invocationTraffic{}
	server := invocationMCPServer(t, traffic, true)
	toolset := invocationPassthroughToolset(server.URL)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	toolset.params.TLSCACertPath = &caPath
	ctx, _ := mcpInvocation(t, "retry", "alice")
	_, err := toolset.Tools(ctx)
	require.Error(t, err)
	require.Empty(t, toolset.invocations)
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
	}), 0o600))
	tools, err := toolset.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
}

func TestInvocationToolsetUnidentifiedInvocationRemainsUncached(t *testing.T) {
	traffic := &invocationTraffic{}
	server := invocationMCPServer(t, traffic, false)
	toolset := invocationPassthroughToolset(server.URL)
	ctx, _ := mcpInvocation(t, "", "alice")
	for range 2 {
		tools, err := toolset.Tools(ctx)
		require.NoError(t, err)
		require.Len(t, tools, 1)
	}
	require.Empty(t, toolset.invocations)
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	require.Len(t, traffic.initializations, 2)
}
