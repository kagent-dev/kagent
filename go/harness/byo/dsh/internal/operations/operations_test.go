package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const agentToken = "agent-token-012345678901234567890123456789"
const approverToken = "approver-token-012345678901234567890123456789"

type callFunc func(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error)

func (f callFunc) Call(ctx context.Context, tool string, args json.RawMessage) (*mcp.CallToolResult, error) {
	return f(ctx, tool, args)
}

func (f callFunc) ListTools(context.Context) ([]*mcp.Tool, error) {
	return []*mcp.Tool{{Name: "write", InputSchema: map[string]any{"type": "object"}}}, nil
}

var _ Caller = callFunc(nil)

type notifyFunc func(context.Context, Operation) error

func (f notifyFunc) Notify(ctx context.Context, o Operation) error { return f(ctx, o) }

var _ Notifier = notifyFunc(nil)

func database(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("PostgreSQL integration test")
	}
	c, err := postgres.Run(t.Context(), "postgres:16-alpine", postgres.WithDatabase("operations"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, c)
	dsn, err := c.ConnectionString(t.Context(), "sslmode=disable")
	require.NoError(t, err)
	return dsn
}

func openTestStore(t *testing.T, dsn, scope string) *Store {
	t.Helper()
	s, err := OpenStore(t.Context(), dsn, scope)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s
}

func testConfig(scope, url string) Config {
	return Config{Scope: scope, UpstreamURL: url, TargetRevision: "test-v1", Tools: []string{"write"}, Approver: "reviewer@example.test", ApprovalTTLSeconds: 3600, CallTimeoutSeconds: 5}
}

func testService(t *testing.T, store *Store, config Config, caller Caller, notifier Notifier) (*Service, *httptest.Server, *mcp.ClientSession) {
	t.Helper()
	s, err := New(store, config, caller, notifier)
	require.NoError(t, err)
	h, err := s.Handler(agentToken, approverToken)
	require.NoError(t, err)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: authenticatedClient(agentToken, 5*time.Second), DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return s, server, client
}

func toolOperation(t *testing.T, client *mcp.ClientSession, name string, args any) Operation {
	t.Helper()
	r, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	require.False(t, r.IsError, "%v", r.Content)
	var o Operation
	require.NoError(t, json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &o))
	return o
}

func decision(t *testing.T, url, token string, o Operation, state string) int {
	t.Helper()
	body, err := json.Marshal(struct {
		Digest   string `json:"request_digest"`
		Decision string `json:"decision"`
	}{o.RequestDigest, state})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/operations/"+o.ID+"/decision", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer r.Body.Close()
	_, _ = io.Copy(io.Discard, r.Body)
	return r.StatusCode
}

func TestDurableOperations(t *testing.T) {
	dsn := database(t)
	var calls atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: "target", Version: "1"}, nil)
	mcp.AddTool(remote, &mcp.Tool{Name: "write"}, func(_ context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		calls.Add(1)
		return nil, map[string]any{"saved": args["value"]}, nil
	})
	mcp.AddTool(remote, &mcp.Tool{Name: "forbidden_tool"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		t.Error("unconfigured tool executed")
		return nil, nil, errors.New("forbidden")
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer upstream.Close()
	for _, scenario := range []string{"restart", "reject", "revoke", "concurrent", "response-lost", "commit-lost", "binding-changed", "expiry", "notification-lost", "notification-busy"} {
		t.Run(scenario, func(t *testing.T) {
			scope := uuid.NewString()
			store := openTestStore(t, dsn, scope)
			config := testConfig(scope, upstream.URL)
			before := calls.Load()
			remoteCaller := NewMCPCaller(upstream.URL, "", 5*time.Second)
			var caller Caller = remoteCaller
			if scenario == "response-lost" || scenario == "commit-lost" {
				caller = callFunc(func(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
					r, err := remoteCaller.Call(ctx, name, args)
					if err != nil {
						return nil, err
					}
					if scenario == "response-lost" {
						return nil, errors.New("response lost after side effect")
					}
					require.NoError(t, store.Close())
					return r, nil
				})
			}
			var notifications atomic.Int32
			notifier := notifyFunc(func(_ context.Context, o Operation) error {
				n := notifications.Add(1)
				if scenario == "notification-busy" && n == 1 {
					return errNotAccepted
				}
				require.Contains(t, []string{Succeeded, Rejected, Expired}, o.State)
				if scenario == "notification-lost" {
					return errors.New("accepted then response lost")
				}
				return nil
			})
			s, server, client := testService(t, store, config, caller, notifier)
			listed, err := client.ListTools(t.Context(), nil)
			require.NoError(t, err)
			require.Len(t, listed.Tools, 3)
			catalog, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_operation_tools", Arguments: map[string]any{}})
			require.NoError(t, err)
			require.False(t, catalog.IsError)
			require.Contains(t, catalog.Content[0].(*mcp.TextContent).Text, "write")
			require.NotContains(t, catalog.Content[0].(*mcp.TextContent).Text, "forbidden_tool")
			_, err = s.Submit(t.Context(), SubmitRequest{RequestKey: "forbidden", Tool: "forbidden_tool", Arguments: json.RawMessage(`{}`)})
			require.Error(t, err)
			req := SubmitRequest{RequestKey: "deploy-ticket-7", Tool: "write", Arguments: json.RawMessage(`{"value":7}`)}
			o := toolOperation(t, client, "request_operation", req)
			require.Equal(t, AwaitingApproval, o.State)
			require.Equal(t, o.ID, toolOperation(t, client, "request_operation", req).ID)
			req.Arguments = json.RawMessage(`{"value":8}`)
			r, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "request_operation", Arguments: req})
			require.NoError(t, err)
			require.True(t, r.IsError)
			require.Equal(t, before, calls.Load())
			require.Equal(t, 401, decision(t, server.URL, agentToken, o, Approved))
			wrong := o
			wrong.RequestDigest = "changed"
			require.Equal(t, 409, decision(t, server.URL, approverToken, wrong, Approved))
			state := Approved
			if scenario == "reject" {
				state = Rejected
			}
			require.Equal(t, 200, decision(t, server.URL, approverToken, o, state))
			require.Equal(t, 200, decision(t, server.URL, approverToken, o, state))
			if scenario == "revoke" {
				require.Equal(t, 200, decision(t, server.URL, approverToken, o, Rejected))
			}
			switch scenario {
			case "restart":
				// Reconstruct both DB owner and service after approval, before execution.
				require.NoError(t, store.Close())
				store = openTestStore(t, dsn, scope)
				s, _, client = testService(t, store, config, remoteCaller, notifier)
			case "binding-changed":
				config.TargetRevision = "new-identity"
				s, _, _ = testService(t, store, config, remoteCaller, notifier)
			case "expiry":
				// A separate API submission with a short TTL, no direct SQL seeding.
				o, err = store.submit(t.Context(), Operation{RequestKey: "expired", RequestDigest: "digest", Binding: s.binding, Tool: "write", Arguments: json.RawMessage(`{}`)}, -time.Second)
				require.NoError(t, err)
				require.Equal(t, 409, decision(t, server.URL, approverToken, o, Approved))
			}
			if scenario == "concurrent" {
				var wg sync.WaitGroup
				for range 8 {
					wg.Go(func() { require.NoError(t, s.Tick(t.Context())) })
				}
				wg.Wait()
			} else {
				err = s.Tick(t.Context())
				if scenario == "commit-lost" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}
			if scenario == "commit-lost" {
				store = openTestStore(t, dsn, scope)
			}
			got, err := store.Get(t.Context(), o.ID)
			require.NoError(t, err)
			switch scenario {
			case "binding-changed":
				require.Equal(t, Approved, got.State)
				require.Equal(t, before, calls.Load())
			case "reject", "revoke":
				require.Equal(t, Rejected, got.State)
				require.Equal(t, before, calls.Load())
			case "expiry":
				require.Equal(t, Expired, got.State)
			case "response-lost", "commit-lost":
				require.Equal(t, OutcomeUnknown, got.State)
				require.Nil(t, got.Result)
				recovered, err := New(store, config, remoteCaller, notifier)
				require.NoError(t, err)
				require.NoError(t, recovered.Tick(t.Context()))
				require.Equal(t, before+1, calls.Load())
			default:
				require.Equal(t, Succeeded, got.State)
				require.Contains(t, string(got.Result), "7")
				require.Equal(t, before+1, calls.Load())
				// Lost notification must never execute the business operation again.
				require.NoError(t, s.Tick(t.Context()))
				expectedNotifications := int32(1)
				if scenario == "notification-busy" {
					expectedNotifications = 2
					got, err = store.Get(t.Context(), o.ID)
					require.NoError(t, err)
				}
				require.Equal(t, expectedNotifications, notifications.Load())
				require.Equal(t, before+1, calls.Load())
				if scenario == "notification-lost" {
					require.Equal(t, "delivery_unknown", got.Notification)
				} else {
					require.Equal(t, "delivered", got.Notification)
				}
				read := toolOperation(t, client, "get_operation", map[string]string{"operation_id": o.ID})
				require.Equal(t, Succeeded, read.State)
			}
			other := openTestStore(t, dsn, uuid.NewString())
			_, err = other.Get(t.Context(), o.ID)
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestClaimCrashAndMigration(t *testing.T) {
	dsn := database(t)
	scope := uuid.NewString()
	store := openTestStore(t, dsn, scope)
	config := testConfig(scope, "http://127.0.0.1:1")
	var calls atomic.Int32
	caller := callFunc(func(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return nil, errors.New("unexpected call")
	})
	s, _, _ := testService(t, store, config, caller, nil)
	o, err := s.Submit(t.Context(), SubmitRequest{RequestKey: "key", Tool: "write", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	_, err = store.decide(t.Context(), o.ID, o.RequestDigest, Approved, "reviewer")
	require.NoError(t, err)
	_, err = store.claim(t.Context(), s.binding)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	store = openTestStore(t, dsn, scope)
	s, err = New(store, config, caller, nil)
	require.NoError(t, err)
	require.NoError(t, s.Tick(t.Context()))
	require.Zero(t, calls.Load())
	got, err := s.Get(t.Context(), o.ID)
	require.NoError(t, err)
	require.Equal(t, OutcomeUnknown, got.State)
	// Exercise the independent migration's Down and Up on a disposable database.
	files, err := fs.Sub(migrationFiles, "migrations")
	require.NoError(t, err)
	p, err := goose.NewProvider(goose.DialectPostgres, store.db, files, goose.WithTableName("dsh_approval_schema_migrations"))
	require.NoError(t, err)
	_, err = p.DownTo(t.Context(), 0)
	require.NoError(t, err)
	_, err = p.Up(t.Context())
	require.NoError(t, err)
	_, err = s.Get(t.Context(), o.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestA2ANotification(t *testing.T) {
	var captured struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Message struct {
				ID        string `json:"messageId"`
				ContextID string `json:"contextId"`
				TaskID    string `json:"taskId"`
			} `json:"message"`
		} `json:"params"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer notification-token", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		result, err := json.Marshal(a2atype.StreamResponse{Event: a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("accepted"))})
		require.NoError(t, err)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(captured.ID) + `,"result":` + string(result) + `}`))
	}))
	defer server.Close()
	n := NewA2ANotifier(Continuation{URL: server.URL, ContextID: "saved-context"}, "notification-token")
	require.NoError(t, n.Notify(t.Context(), Operation{ID: "op-7", State: Succeeded}))
	require.Equal(t, "operation-op-7", captured.Params.Message.ID)
	require.Equal(t, "saved-context", captured.Params.Message.ContextID)
	require.Empty(t, captured.Params.Message.TaskID)
}

type errorTransport struct {
	a2aclient.Transport
	err error
}

func (e errorTransport) SendMessage(context.Context, a2aclient.ServiceParams, *a2atype.SendMessageRequest) (a2atype.SendMessageResult, error) {
	return nil, e.err
}

var _ a2aclient.Transport = errorTransport{}

func TestNotificationRetryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"not accepted", a2atype.NewError(a2atype.ErrUnsupportedOperation, "busy").WithErrorInfoMeta(map[string]string{"reason": "KAGENT_SEND_NOT_ACCEPTED"}), true},
		{"generic rejection", a2atype.NewError(a2atype.ErrUnsupportedOperation, "unsupported"), false},
		{"timeout", context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &A2ANotifier{config: Continuation{ContextID: "saved"}, transport: errorTransport{err: tc.err}}
			err := n.Notify(t.Context(), Operation{ID: "operation", State: Succeeded})
			require.Equal(t, tc.retry, errors.Is(err, errNotAccepted))
		})
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true); w.WriteHeader(200) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	_, err := NewMCPCaller(source.URL, "private-credential", time.Second).Call(t.Context(), "write", json.RawMessage(`{}`))
	require.Error(t, err)
	require.False(t, leaked.Load())
}
