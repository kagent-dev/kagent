package anthropicworker

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/env/guest/process"
	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kagent-dev/kagent/go/api/client"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/migrations"
)

func TestWorkerShutdownHandsOffRunningCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	dsn := dbtest.StartT(context.Background(), t)
	db := dbtest.PoolConfig(dsn)
	const schema = "anthropic_test"
	source := MigrationSource(schema)
	require.NoError(t, migrations.RunUp(t.Context(), db, []migrations.Source{source}))
	db.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), db)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	workspace := t.TempDir()
	cfg := process.DefaultConfig(t.TempDir())
	cfg.Workspace = workspace
	tracker, err := process.NewTracker(cfg)
	require.NoError(t, err)
	t.Cleanup(tracker.Close)
	guest := &handoffGuest{Service: process.NewService(tracker), observed: make(chan struct{}, 1),
		suspended: make(chan struct{}, 1), expiring: make(chan struct{}, 1), templates: make(chan *apiv1.ResourceReference, 2)}
	kagent := testKagent(t, guest)
	upstream := &handoffProvider{posted: make(chan string, 1), events: make(chan string, 1), heartbeatRecovered: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(upstream.serve))
	t.Cleanup(server.Close)
	provider := anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()), option.WithMaxRetries(0))
	env := Environment{ID: "env_test", Key: "environment-key", Template: &apiv1.ResourceReference{
		Namespace: "default", Name: "bash",
	}}
	newReplica := func() *Runner {
		r, err := New(pool, provider, kagent, []Environment{env}, make([]byte, 32), time.Second, 1)
		require.NoError(t, err)
		return r
	}
	run := func(r *Runner) (context.CancelFunc, <-chan struct{}) {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Error("worker did not shut down")
			}
		})
		go func() {
			defer close(done)
			r.Run(ctx)
		}()
		return cancel, done
	}
	stop, firstStopped := run(newReplica())
	receive(t, guest.observed)
	template := receive(t, guest.templates)
	assert.Equal(t, env.Template.GetName(), template.GetName(), "the environment selects the sandbox template")
	assert.Equal(t, env.Template.GetNamespace(), template.GetNamespace())
	receive(t, upstream.heartbeatRecovered)
	assert.EqualValues(t, 1, upstream.streams.Load(), "a transient heartbeat error must not reopen SSE")

	stop()
	receive(t, firstStopped)

	assert.Zero(t, guest.signals.Load(), "shutting down the first worker cannot signal the sandbox process")
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "release"), nil, 0o600))
	stopReplacement, replacementStopped := run(newReplica())
	body := receive(t, upstream.posted)
	assert.JSONEq(t, `{"events":[{"type":"user.tool_result","tool_use_id":"tool_pending","is_error":false,"content":[{"type":"text","text":"survived its worker"}]}]}`, body)
	assert.EqualValues(t, 1, upstream.acks.Load(), "the second worker must reuse the acknowledged assignment")
	assert.EqualValues(t, 2, upstream.streams.Load(), "each worker opens its own SSE stream")
	assert.EqualValues(t, 1, guest.starts.Load(), "the replacement must collect, not restart")

	if !t.Run("Given an idle suspended sandbox, when SSE delivers a tool call, assert that it completes before the recovery scan", func(t *testing.T) {
		receive(t, guest.suspended)
		upstream.events <- `{"type":"agent.tool_use","id":"tool_after_idle","name":"bash","input":{"command":"test -f release && printf 'still here'"}}`
		body := receive(t, upstream.posted)
		assert.JSONEq(t, `{"events":[{"type":"user.tool_result","tool_use_id":"tool_after_idle","is_error":false,"content":[{"type":"text","text":"still here"}]}]}`, body)
		assert.EqualValues(t, 1, guest.creates.Load(), "an idle sandbox must be resumed, not recreated")
		assert.EqualValues(t, 1, guest.resumes.Load())
		assert.EqualValues(t, 2, upstream.streams.Load(), "suspending compute must not close the relay stream")
	}) {
		t.FailNow()
	}

	upstream.events <- `{"type":"agent.tool_use","id":"tool_expiring","name":"bash","input":{"command":"while [ ! -f expired ]; do sleep 0.05; done","timeout_ms":15000}}`
	receive(t, guest.expiring)
	guest.expired.Store(true)
	body = receive(t, upstream.posted)
	assert.JSONEq(t, `{"events":[{"type":"user.tool_result","tool_use_id":"tool_expiring","is_error":true,"content":[{"type":"text","text":"The command's result is no longer available."}]}]}`, body)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "expired"), nil, 0o600))

	upstream.events <- `{"type":"agent.tool_use","id":"tool_replacement","name":"bash","input":{"command":"printf 'replacement sandbox'"}}`
	body = receive(t, upstream.posted)
	assert.JSONEq(t, `{"events":[{"type":"user.tool_result","tool_use_id":"tool_replacement","is_error":false,"content":[{"type":"text","text":"replacement sandbox"}]}]}`, body)
	assert.EqualValues(t, 2, guest.creates.Load(), "new work gets a replacement for expired compute")
	assert.EqualValues(t, 2, upstream.streams.Load())

	upstream.events <- `{"type":"session.status_terminated","id":"end"}`
	require.Eventually(t, func() bool {
		var done bool
		err := pool.QueryRow(t.Context(), `SELECT credential IS NULL AND finished_at IS NOT NULL FROM sandbox_work WHERE id = 'work_test'`).Scan(&done)
		return done && err == nil
	}, 5*time.Second, 50*time.Millisecond)
	assert.EqualValues(t, 1, guest.deletes.Load(), "only session termination cleans up the sandbox")
	assert.EqualValues(t, 0, upstream.unexpected.Load())
	stopReplacement()
	receive(t, replacementStopped)
	require.NoError(t, migrations.WithProvider(t.Context(), db, source, func(p *goose.Provider) error {
		_, err := p.Down(t.Context())
		return err
	}))
}

func TestRejectedHeartbeatWaitsForRedispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	db := dbtest.PoolConfig(dbtest.StartT(t.Context(), t))
	const schema = "heartbeat_test"
	require.NoError(t, migrations.RunUp(t.Context(), db, []migrations.Source{MigrationSource(schema)}))
	db.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), db)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	for _, status := range []int{http.StatusUnauthorized, http.StatusPreconditionFailed} {
		t.Run(fmt.Sprintf("Given a saved assignment, when heartbeat returns %d, assert that only a fresh matching offer resumes it", status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				assert.True(t, strings.HasSuffix(req.URL.Path, "/heartbeat"))
				assert.Equal(t, "Bearer old-token", req.Header.Get("Authorization"))
				assert.Equal(t, "previous", req.URL.Query().Get("expected_last_heartbeat"))
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)
			env := Environment{ID: fmt.Sprintf("env%d", status), Key: "environment-key", Template: &apiv1.ResourceReference{
				Namespace: "default", Name: "bash",
			}}
			provider := anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithBaseURL(server.URL),
				option.WithHTTPClient(server.Client()), option.WithMaxRetries(0))
			r, err := New(pool, provider, nil, []Environment{env}, make([]byte, 32), time.Second, 1)
			require.NoError(t, err)
			ctx := t.Context()
			offer := assignment{id: env.ID, environment: env.ID, sessionID: "session"}
			environments := []string{env.ID}
			require.NoError(t, r.acceptOffer(ctx, offer, "old-token"))
			w, err := r.store.claim(ctx, environments)
			require.NoError(t, err)
			require.NoError(t, r.store.ack(ctx, w))
			require.NoError(t, r.store.heartbeat(ctx, w, "previous"))
			require.NoError(t, r.store.saveSandbox(ctx, &w, "existing-sandbox"))
			require.NoError(t, r.store.addCommand(ctx, w, command{id: "tool", name: "bash", input: []byte(`{}`)}))
			require.NoError(t, r.store.transition(ctx, w, command{id: "tool", phase: "queued"}, "running", new("process"), nil, false))
			require.NoError(t, r.store.release(ctx, w, 0))

			runCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				r.work(runCtx, environments)
			}()
			t.Cleanup(func() {
				cancel()
				receive(t, done)
			})
			require.Eventually(t, func() bool {
				_, err := r.store.parkedCredential(ctx, offer)
				return err == nil
			}, 5*time.Second, 50*time.Millisecond)
			cancel()
			receive(t, done)
			require.EqualValues(t, 1, calls.Load())

			require.NoError(t, r.acceptOffer(ctx, offer, "old-token"))
			wrongSession := offer
			wrongSession.sessionID = "another-session"
			require.NoError(t, r.acceptOffer(ctx, wrongSession, "new-token"))
			_, err = r.store.claim(ctx, environments)
			require.ErrorIs(t, err, pgx.ErrNoRows)

			require.NoError(t, r.acceptOffer(ctx, offer, "new-token"))
			resumed, err := r.store.claim(ctx, environments)
			require.NoError(t, err)
			credential, err := r.cipher.Open(nil, nil, resumed.credential, []byte(offer.id))
			require.NoError(t, err)
			assert.Equal(t, "new-token", string(credential))
			assert.False(t, resumed.acknowledged)
			assert.Empty(t, resumed.heartbeat)
			assert.Equal(t, "existing-sandbox", resumed.sandboxID)
			pending, err := r.store.nextCommand(ctx, resumed)
			require.NoError(t, err)
			assert.Equal(t, "process", pending.processID)
		})
	}
}

func TestHeartbeatExpiryFailsUnfinishedCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	db := dbtest.PoolConfig(dbtest.StartT(t.Context(), t))
	const schema = "heartbeat_expiry"
	require.NoError(t, migrations.RunUp(t.Context(), db, []migrations.Source{MigrationSource(schema)}))
	db.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(t.Context(), db)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	cfg := process.DefaultConfig(t.TempDir())
	cfg.Workspace = t.TempDir()
	tracker, err := process.NewTracker(cfg)
	require.NoError(t, err)
	t.Cleanup(tracker.Close)
	guest := &handoffGuest{Service: process.NewService(tracker), observed: make(chan struct{}, 1)}
	upstream := &handoffProvider{
		posted: make(chan string, 2),
		events: make(chan string, 1),
		heartbeat: func(w http.ResponseWriter, attempt int32) {
			if attempt == 1 {
				_, _ = io.WriteString(w, `{"last_heartbeat":"continuation","ttl_seconds":2,"lease_extended":true,"state":"active"}`)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	}
	server := httptest.NewServer(http.HandlerFunc(upstream.serve))
	t.Cleanup(server.Close)
	provider := anthropic.NewClient(option.WithoutEnvironmentDefaults(), option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()), option.WithMaxRetries(0))
	env := Environment{ID: "env_test", Key: "environment-key",
		Template: &apiv1.ResourceReference{Namespace: "default", Name: "bash"}}
	r, err := New(pool, provider, testKagent(t, guest), []Environment{env}, make([]byte, 32), time.Minute, 1)
	require.NoError(t, err)
	require.NoError(t, r.acceptOffer(t.Context(), assignment{
		id: "work_test", environment: env.ID, sessionID: "session_test",
	}, "session-token"))
	w, err := r.store.claim(t.Context(), []string{env.ID})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	done := make(chan error, 1)
	t.Cleanup(func() {
		cancel()
		for range done {
		}
	})
	go func() {
		defer close(done)
		done <- r.serve(ctx, w, env)
	}()
	receive(t, guest.observed)
	upstream.events <- `{"type":"agent.tool_use","id":"tool_queued","name":"bash","input":{"command":"printf queued"}}`
	for _, id := range []string{"tool_pending", "tool_queued"} {
		assert.JSONEq(t, fmt.Sprintf(`{"events":[{"type":"user.tool_result","tool_use_id":%q,"is_error":true,"content":[{"type":"text","text":"Command did not finish before the sandbox worker's heartbeat lease expired."}]}]}`, id), receive(t, upstream.posted))
	}
	assert.EqualValues(t, 1, guest.starts.Load(), "the queued command must not start")
	assert.ErrorIs(t, receive(t, done), errHeartbeatExpired)
	_, err = r.store.nextCommand(t.Context(), w)
	assert.ErrorIs(t, err, pgx.ErrNoRows, "delivered results cannot remain pending")
}

func TestSessionJournalHonorsToolConfirmation(t *testing.T) {
	j := sessionJournal{seen: make(map[string]bool), pending: make(map[string]pendingTool)}
	t.Run("Given an ask-gated command, when the user confirms it, assert that it becomes runnable", func(t *testing.T) {
		changed, _, _, err := j.accept(sessionEvent{id: "tool", kind: "agent.tool_use", permission: "ask"})
		require.NoError(t, err)
		require.Len(t, changed, 1)
		assert.Equal(t, "ask", changed[0].event.permission)
		changed, _, _, err = j.accept(sessionEvent{id: "confirmation", kind: "user.tool_confirmation", toolID: "tool", verdict: "allow"})
		require.NoError(t, err)
		require.Len(t, changed, 1)
		assert.Equal(t, "allow", changed[0].event.permission)
	})
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for expected activity")
		var zero T
		return zero
	}
}

type handoffProvider struct {
	acks, streams, heartbeats, unexpected atomic.Int32
	posted                                chan string
	events                                chan string
	heartbeatRecovered                    chan struct{}
	heartbeat                             func(http.ResponseWriter, int32)
}

func (p *handoffProvider) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/environments/env_test/work/poll":
		if p.acks.Load() != 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		secret := base64.RawURLEncoding.EncodeToString([]byte(`{"sessions_token":"session-token"}`))
		_, _ = io.WriteString(w, `{"id":"work_test","environment_id":"env_test","data":{"id":"session_test","type":"session"},"secret":"`+secret+`"}`)
	case "/v1/environments/env_test/work/work_test/ack":
		p.acks.Add(1)
		_, _ = io.WriteString(w, `{"id":"work_test","state":"starting"}`)
	case "/v1/environments/env_test/work/work_test/heartbeat":
		expected := "NO_HEARTBEAT"
		attempt := p.heartbeats.Add(1)
		if attempt > 1 {
			expected = "continuation"
		}
		if r.Header.Get("Authorization") != "Bearer session-token" || r.URL.Query().Get("expected_last_heartbeat") != expected {
			p.unexpected.Add(1)
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if p.heartbeat != nil {
			p.heartbeat(w, attempt)
			return
		}
		if attempt == 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if attempt == 3 {
			close(p.heartbeatRecovered)
		}
		_, _ = io.WriteString(w, `{"last_heartbeat":"continuation","ttl_seconds":6,"lease_extended":true,"state":"active"}`)
	case "/v1/sessions/session_test/events":
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{}`)
			p.posted <- string(body)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"type":"agent.tool_use","id":"tool_pending","name":"bash","input":{"command":"while [ ! -f release ]; do sleep 0.05; done; printf 'survived its worker'","timeout_ms":15000}}],"has_more":false}`)
	case "/v1/sessions/session_test/events/stream":
		p.streams.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case event := <-p.events:
				if _, err := io.WriteString(w, "event: event\ndata: "+event+"\n\n"); err != nil {
					return
				}
				if err := http.NewResponseController(w).Flush(); err != nil {
					return
				}
			}
		}
	default:
		p.unexpected.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}
}

type handoffGuest struct {
	*process.Service
	apiv1.UnimplementedSandboxServiceServer
	templates                                  chan *apiv1.ResourceReference
	observed, suspended, expiring              chan struct{}
	starts, signals, creates, resumes, deletes atomic.Int32
	idle, expired                              atomic.Bool
}

func (f *handoffGuest) CreateSandbox(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.CreateSandboxResponse, error) {
	select {
	case f.templates <- req.GetSandboxTemplate():
	default:
	}
	f.creates.Add(1)
	f.expired.Store(false)
	f.idle.Store(false)
	return &apiv1.CreateSandboxResponse{Sandbox: f.sandbox()}, nil
}

func (f *handoffGuest) GetSandbox(context.Context, *apiv1.GetSandboxRequest) (*apiv1.GetSandboxResponse, error) {
	return &apiv1.GetSandboxResponse{Sandbox: f.sandbox()}, nil
}

func (f *handoffGuest) SuspendSandbox(context.Context, *apiv1.SuspendSandboxRequest) (*apiv1.SuspendSandboxResponse, error) {
	f.idle.Store(true)
	select {
	case f.suspended <- struct{}{}:
	default:
	}
	return &apiv1.SuspendSandboxResponse{Sandbox: f.sandbox()}, nil
}

func (f *handoffGuest) ResumeSandbox(context.Context, *apiv1.ResumeSandboxRequest) (*apiv1.ResumeSandboxResponse, error) {
	f.resumes.Add(1)
	f.idle.Store(false)
	return &apiv1.ResumeSandboxResponse{Sandbox: f.sandbox()}, nil
}

func (f *handoffGuest) DeleteSandbox(context.Context, *apiv1.DeleteSandboxRequest) (*apiv1.DeleteSandboxResponse, error) {
	f.deletes.Add(1)
	return &apiv1.DeleteSandboxResponse{Sandbox: f.sandbox()}, nil
}

func (f *handoffGuest) sandbox() *apiv1.Sandbox {
	state := apiv1.RuntimeState_RUNTIME_STATE_READY
	if f.idle.Load() {
		state = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
	}
	expires := time.Now().Add(time.Hour)
	if f.expired.Load() {
		expires = time.Now().Add(-time.Hour)
	}
	return &apiv1.Sandbox{Id: fmt.Sprintf("sandbox-%d", f.creates.Load()), State: state,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
		ExpiresAt: timestamppb.New(expires)}
}

func (f *handoffGuest) StartProcess(ctx context.Context, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
	f.starts.Add(1)
	return f.Service.StartProcess(ctx, req)
}

func (f *handoffGuest) GetProcess(ctx context.Context, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	if f.expired.Load() {
		return nil, status.Error(codes.FailedPrecondition, "sandbox is expired")
	}
	proc, err := f.Service.GetProcess(ctx, req)
	if proc.GetState() == guestpb.ProcessState_PROCESS_STATE_RUNNING {
		observed := f.observed
		if cmd := proc.GetCommand(); len(cmd) >= 3 && strings.Contains(cmd[2], "[ ! -f expired ]") {
			observed = f.expiring
		}
		select {
		case observed <- struct{}{}:
		default:
		}
	}
	return proc, err
}

func (f *handoffGuest) SignalProcess(ctx context.Context, req *guestpb.SignalProcessRequest) (*guestpb.Process, error) {
	f.signals.Add(1)
	return f.Service.SignalProcess(ctx, req)
}

func testKagent(t *testing.T, guest *handoffGuest) *client.SandboxClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	apiv1.RegisterSandboxServiceServer(server, guest)
	guestpb.RegisterProcessServiceServer(server, guest)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	kagent, err := client.NewAPI("http://kagent", client.WithGRPCTarget("passthrough:///test"),
		client.WithGRPCDialOptions(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		})))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, kagent.Close()) })
	return kagent.Sandbox
}
