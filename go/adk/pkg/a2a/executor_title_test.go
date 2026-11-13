package a2a

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

type nameWrite struct{ sessionID, name string }

type recordingSessionServer struct {
	apiv1.UnimplementedSessionServiceServer
	mu     sync.Mutex
	writes []nameWrite
}

func (s *recordingSessionServer) SetSessionNameIfEmpty(_ context.Context, req *apiv1.SetSessionNameIfEmptyRequest) (*apiv1.SetSessionNameIfEmptyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, nameWrite{req.GetSessionId(), req.GetName()})
	return &apiv1.SetSessionNameIfEmptyResponse{}, nil
}

func (s *recordingSessionServer) snapshot() []nameWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]nameWrite(nil), s.writes...)
}

type fakeTitler struct {
	mu       sync.Mutex
	messages []string
	title    string
	err      error
	// block makes Title wait for the caller's context to end.
	block bool
}

func (f *fakeTitler) Title(ctx context.Context, userMessage string) (string, error) {
	f.mu.Lock()
	f.messages = append(f.messages, userMessage)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.title, f.err
}

func (f *fakeTitler) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

type titleFixture struct {
	t        *testing.T
	server   *recordingSessionServer
	client   *controllerclient.Client
	titler   *fakeTitler
	executor *KAgentExecutor
	// writesAtTerminal is what the controller had received when the terminal event reached the consumer.
	writesAtTerminal []nameWrite
	terminalSeen     bool
}

func newTitleFixture(t *testing.T) *titleFixture {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := &recordingSessionServer{}
	grpcServer := grpc.NewServer()
	apiv1.RegisterSessionServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	client, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://bufnet:80",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return &titleFixture{t: t, server: server, client: client, titler: &fakeTitler{title: "Fix login bug"}}
}

func (f *titleFixture) withTerminalState(state a2atype.TaskState) *titleFixture {
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "context-1"}
	builtin := &recordingExecutor{events: []a2atype.Event{
		a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil),
		a2atype.NewStatusUpdateEvent(reqCtx, state, nil),
	}}
	f.executor = &KAgentExecutor{
		builtin:          builtin,
		sessionService:   adksession.InMemoryService(),
		appName:          "test-app",
		logger:           slog.New(slog.DiscardHandler),
		controllerClient: f.client,
		titler:           f.titler,
		titleTimeout:     50 * time.Millisecond,
	}
	return f
}

func (f *titleFixture) whenTurnRuns(message string) *titleFixture {
	f.t.Helper()
	f.terminalSeen = false
	reqCtx := &a2asrv.ExecutorContext{
		TaskID: "task-1", ContextID: "context-1",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(message)),
	}
	for event, err := range f.executor.Execute(f.t.Context(), reqCtx) {
		require.NoError(f.t, err)
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State != a2atype.TaskStateWorking {
			f.terminalSeen = true
			f.writesAtTerminal = f.server.snapshot()
		}
	}
	require.True(f.t, f.terminalSeen, "turn must deliver its terminal event")
	return f
}

func TestFirstTurnTitle(t *testing.T) {
	t.Run("Given a new session, when the first turn completes, assert that the title is written before the terminal event is delivered", func(t *testing.T) {
		f := newTitleFixture(t).withTerminalState(a2atype.TaskStateCompleted).whenTurnRuns("my login page is broken")
		assert.Equal(t, []string{"my login page is broken"}, f.titler.calls())
		assert.Equal(t, []nameWrite{{"context-1", "Fix login bug"}}, f.writesAtTerminal)
	})

	t.Run("Given a session that already had a turn, when a second turn completes, assert that no title is requested or written", func(t *testing.T) {
		f := newTitleFixture(t).withTerminalState(a2atype.TaskStateCompleted).whenTurnRuns("first")
		before := len(f.server.snapshot())
		f.whenTurnRuns("second")
		assert.Equal(t, []string{"first"}, f.titler.calls())
		assert.Len(t, f.server.snapshot(), before)
	})

	t.Run("Given a titler that never answers, when the first turn completes, assert that the terminal event is delivered and nothing is written", func(t *testing.T) {
		f := newTitleFixture(t)
		f.titler.block = true
		f.withTerminalState(a2atype.TaskStateCompleted).whenTurnRuns("hello")
		assert.Len(t, f.titler.calls(), 1)
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a titler that fails, when the first turn completes, assert that the turn completes and nothing is written", func(t *testing.T) {
		f := newTitleFixture(t)
		f.titler.err = errors.New("model unavailable")
		f.withTerminalState(a2atype.TaskStateCompleted).whenTurnRuns("hello")
		assert.Len(t, f.titler.calls(), 1)
		assert.Empty(t, f.server.snapshot())
	})

	for _, state := range []a2atype.TaskState{a2atype.TaskStateFailed, a2atype.TaskStateInputRequired} {
		t.Run("Given a new session, when the first turn ends "+string(state)+", assert that no title is written", func(t *testing.T) {
			f := newTitleFixture(t).withTerminalState(state).whenTurnRuns("hello")
			assert.Empty(t, f.server.snapshot())
		})
	}

	t.Run("Given no titler, when the first turn completes, assert that nothing is written", func(t *testing.T) {
		f := newTitleFixture(t).withTerminalState(a2atype.TaskStateCompleted)
		f.executor.titler = nil
		f.whenTurnRuns("hello")
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a titler and no controller client, when the first turn completes, assert that the titler is not called", func(t *testing.T) {
		f := newTitleFixture(t).withTerminalState(a2atype.TaskStateCompleted)
		f.executor.controllerClient = nil
		f.whenTurnRuns("hello")
		assert.Empty(t, f.titler.calls())
	})
}

func TestCleanTitle(t *testing.T) {
	long := ""
	for len(long) < 300 {
		long += "word "
	}
	scenarios := []struct {
		name, raw, want string
	}{
		{"surrounding quotes", `"Fix the login bug"`, "Fix the login bug"},
		{"trailing punctuation", "Fix the login bug.", "Fix the login bug"},
		{"padding and blank lines", "\n  Fix  the login bug  \n", "Fix the login bug"},
		{"only the first line", "Fix the login bug\nThis title covers...", "Fix the login bug"},
		{"empty output", " \n ", ""},
	}
	for _, s := range scenarios {
		t.Run("Given model output with "+s.name+", when it is cleaned, assert that a plain title remains", func(t *testing.T) {
			assert.Equal(t, s.want, cleanTitle(s.raw))
		})
	}
	t.Run("Given model output over 200 characters, when it is cleaned, assert that the title is capped at 200", func(t *testing.T) {
		assert.LessOrEqual(t, len([]rune(cleanTitle(long))), titleMaxLength)
		assert.NotEmpty(t, cleanTitle(long))
	})
}
