package operations

import (
	"context"
	"encoding/json"
	"io/fs"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

type waitingExecutor struct {
	a2asrv.AgentExecutor
	accepted atomic.Int32
	release  chan struct{}
}

func (e *waitingExecutor) Execute(ctx context.Context, req *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		e.accepted.Add(1)
		if !yield(&a2atype.Task{ID: req.TaskID, ContextID: req.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}, nil) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-e.release:
		}
		yield(&a2atype.TaskStatusUpdateEvent{TaskID: req.TaskID, ContextID: req.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, nil)
	}
}

func TestNotificationAcknowledgedBeforeCompletion(t *testing.T) {
	store := openTestStore(t, database(t), "notification")
	executor := &waitingExecutor{release: make(chan struct{})}
	server := httptest.NewServer(a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor)))
	defer server.Close()
	defer close(executor.release)
	config := testConfig("notification", "http://127.0.0.1:1")
	config.Continuation = &Continuation{URL: server.URL, ContextID: "saved"}
	var calls atomic.Int32
	caller := callFunc(func(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil
	})
	s, err := New(store, config, caller, NewA2ANotifier(*config.Continuation, ""))
	require.NoError(t, err)
	o, err := s.Submit(t.Context(), SubmitRequest{RequestKey: "ticket", Tool: "write", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	_, err = store.decide(t.Context(), o.ID, o.RequestDigest, Approved, "reviewer")
	require.NoError(t, err)
	// The executor cannot finish until this test returns. Acceptance must still
	// be saved promptly; a blocking send would consume this entire deadline.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Tick(ctx))
	saved, err := store.Get(t.Context(), o.ID)
	require.NoError(t, err)
	require.Equal(t, Succeeded, saved.State)
	require.Equal(t, "delivered", saved.Notification)
	require.NoError(t, s.Tick(t.Context()))
	require.EqualValues(t, 1, executor.accepted.Load())
	require.EqualValues(t, 1, calls.Load())
}

func TestMCPPayloadRoundTrip(t *testing.T) {
	dsn := database(t)
	var calls atomic.Int32
	arguments := json.RawMessage(`{"text":"before\u0000after","integer":9007199254740993}`)
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done\x00tail"}}}
	remote := mcp.NewServer(&mcp.Implementation{Name: "target", Version: "1"}, nil)
	remote.AddTool(&mcp.Tool{Name: "write", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		// Compare raw JSON values without float64 conversion or stripping NULs.
		var got map[string]json.RawMessage
		if err := json.Unmarshal(req.Params.Arguments, &got); err != nil {
			return nil, err
		}
		if string(got["integer"]) != "9007199254740993" || string(got["text"]) != `"before\u0000after"` {
			return nil, ErrConflict
		}
		return result, nil
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer upstream.Close()
	store := openTestStore(t, dsn, "payload")
	config := testConfig("payload", upstream.URL)
	s, admin, client := testService(t, store, config, NewMCPCaller(upstream.URL, "", 5*time.Second), nil)
	request := SubmitRequest{RequestKey: "ticket", Tool: "write", Arguments: arguments}
	o := toolOperation(t, client, "request_operation", request)
	require.Equal(t, http.StatusOK, decision(t, admin.URL, approverToken, o, Approved))
	require.NoError(t, s.Tick(t.Context()))
	saved := toolOperation(t, client, "get_operation", map[string]string{"operation_id": o.ID})
	require.Equal(t, Succeeded, saved.State)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, string(encoded), string(saved.Result))
	// Reconstruct the service and store, preserving result bytes and request
	// digest. Retrying the same request must not execute it again.
	require.NoError(t, store.Close())
	reopened := openTestStore(t, dsn, "payload")
	s, err = New(reopened, config, NewMCPCaller(upstream.URL, "", 5*time.Second), nil)
	require.NoError(t, err)
	retried, err := s.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, saved, retried)
	require.NoError(t, s.Tick(t.Context()))
	require.EqualValues(t, 1, calls.Load())
}

func TestRawJSONMigration(t *testing.T) {
	store := openTestStore(t, database(t), "migration")
	// Production migrates before serving requests on a fresh pool. This test
	// changes versions in-process; discard idle connections so prepared result
	// types from the preceding schema do not survive into the next version.
	store.db.SetMaxIdleConns(0)
	files, err := fs.Sub(migrationFiles, "migrations")
	require.NoError(t, err)
	p, err := goose.NewProvider(goose.DialectPostgres, store.db, files, goose.WithTableName("dsh_approval_schema_migrations"))
	require.NoError(t, err)
	_, err = p.DownTo(t.Context(), 1)
	require.NoError(t, err)
	s, err := New(store, testConfig("migration", "http://127.0.0.1:1"), callFunc(func(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "old result"}}}, nil
	}), nil)
	require.NoError(t, err)
	request := SubmitRequest{RequestKey: "old", Tool: "write", Arguments: json.RawMessage(`{"value":7}`)}
	old, err := s.Submit(t.Context(), request)
	require.NoError(t, err)
	_, err = store.decide(t.Context(), old.ID, old.RequestDigest, Approved, "reviewer")
	require.NoError(t, err)
	require.NoError(t, s.Tick(t.Context()))
	old, err = s.Get(t.Context(), old.ID)
	require.NoError(t, err)
	_, err = p.Up(t.Context())
	require.NoError(t, err)
	upgraded, err := s.Submit(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, old, upgraded)
	_, err = p.Down(t.Context())
	require.NoError(t, err, "old-compatible payloads can be rolled back")
	_, err = p.Up(t.Context())
	require.NoError(t, err)
	for _, args := range []string{`{"value":"before\u0000after"}`, `{"value":1e200000}`} {
		o, err := s.Submit(t.Context(), SubmitRequest{RequestKey: args, Tool: "write", Arguments: json.RawMessage(args)})
		require.NoError(t, err)
		require.Equal(t, args, string(o.Arguments))
		// An unsupported downgrade must roll back its schema changes and leave
		// the request intact; later JSON payloads must still be accepted.
		_, err = p.Down(t.Context())
		require.Error(t, err)
		version, err := p.GetDBVersion(t.Context())
		require.NoError(t, err)
		require.EqualValues(t, 2, version)
		got, err := s.Get(t.Context(), o.ID)
		require.NoError(t, err)
		require.Equal(t, o, got)
	}
	for _, invalid := range []string{`[]`, `null`, `not json`} {
		_, err := store.db.ExecContext(t.Context(), `UPDATE dsh_approval_operations SET arguments=$1 WHERE scope=$2`, invalid, store.scope)
		require.Error(t, err, "JSON syntax and object constraint must still be enforced")
	}
}
