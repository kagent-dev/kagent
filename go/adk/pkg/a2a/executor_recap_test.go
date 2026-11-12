package a2a

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

type recapWrite struct {
	sessionID, text string
	turnCount       int32
}

type recapSessionServer struct {
	apiv1.UnimplementedSessionServiceServer
	mu     sync.Mutex
	writes []recapWrite
}

func (s *recapSessionServer) SetSessionRecap(_ context.Context, req *apiv1.SetSessionRecapRequest) (*apiv1.SetSessionRecapResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, recapWrite{req.GetSessionId(), req.GetText(), req.GetTurnCount()})
	return &apiv1.SetSessionRecapResponse{}, nil
}

func (s *recapSessionServer) snapshot() []recapWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recapWrite(nil), s.writes...)
}

type fakeRecapper struct {
	mu          sync.Mutex
	transcripts []string
	text        string
	err         error
	started     chan struct{}
	// release, when set, holds Recap until it is closed.
	release chan struct{}
	// hang holds Recap until its context ends.
	hang      bool
	cancelled bool
}

func newFakeRecapper(text string) *fakeRecapper {
	return &fakeRecapper{text: text, started: make(chan struct{}, 10)}
}

func (f *fakeRecapper) Recap(ctx context.Context, transcript string) (string, error) {
	f.mu.Lock()
	f.transcripts = append(f.transcripts, transcript)
	f.mu.Unlock()
	f.started <- struct{}{}
	if f.hang {
		<-ctx.Done()
		f.mu.Lock()
		f.cancelled = true
		f.mu.Unlock()
		return "", ctx.Err()
	}
	if f.release != nil {
		<-f.release
	}
	return f.text, f.err
}

func (f *fakeRecapper) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.transcripts...)
}

func (f *fakeRecapper) sawCancel() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelled
}

type recapFixture struct {
	t        *testing.T
	server   *recapSessionServer
	recapper *fakeRecapper
	builtin  *recordingExecutor
	executor *KAgentExecutor
	// writesAtTerminal is what the controller had received when the last terminal event reached the consumer.
	writesAtTerminal []recapWrite
	// events are the events the last turn delivered, in order.
	events []a2atype.Event
}

func newRecapFixture(t *testing.T) *recapFixture {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := &recapSessionServer{}
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

	f := &recapFixture{t: t, server: server, recapper: newFakeRecapper("Debugged the login page")}
	f.builtin = &recordingExecutor{}
	f.executor = &KAgentExecutor{
		builtin:          f.builtin,
		sessionService:   adksession.InMemoryService(),
		appName:          "test-app",
		logger:           slog.New(slog.DiscardHandler),
		controllerClient: client,
		recapper:         f.recapper,
		recapWait:        time.Minute,
	}
	// Each turn adds the user message and an agent reply to the ADK session, as the runner does.
	f.builtin.onExecute = f.appendTurn
	return f.givenTurnEndsIn(a2atype.TaskStateCompleted)
}

func (f *recapFixture) givenTurnEndsIn(state a2atype.TaskState) *recapFixture {
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "context-1"}
	f.builtin.events = []a2atype.Event{
		a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil),
		a2atype.NewStatusUpdateEvent(reqCtx, state, nil),
	}
	return f
}

func (f *recapFixture) appendTurn(ctx context.Context) {
	resp, err := f.executor.sessionService.Get(ctx, &adksession.GetRequest{
		AppName: "test-app", UserID: "A2A_USER_context-1", SessionID: "context-1",
	})
	require.NoError(f.t, err)
	user := userText(f.builtin.message)
	require.NoError(f.t, f.executor.sessionService.AppendEvent(ctx, resp.Session, textEvent("user", user)))
	require.NoError(f.t, f.executor.sessionService.AppendEvent(ctx, resp.Session, textEvent("agent", "reply to "+user)))
}

func textEvent(author, text string) *adksession.Event {
	role := genai.RoleModel
	if author == "user" {
		role = genai.RoleUser
	}
	return &adksession.Event{
		Author:      author,
		LLMResponse: adkmodel.LLMResponse{Content: genai.NewContentFromText(text, genai.Role(role))},
	}
}

func (f *recapFixture) whenTurnsRun(messages ...string) *recapFixture {
	f.t.Helper()
	for _, message := range messages {
		reqCtx := &a2asrv.ExecutorContext{
			TaskID: "task-1", ContextID: "context-1",
			Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(message)),
		}
		terminalSeen := false
		f.events = nil
		for event, err := range f.executor.Execute(f.t.Context(), reqCtx) {
			require.NoError(f.t, err)
			f.events = append(f.events, event)
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State != a2atype.TaskStateWorking {
				terminalSeen = true
				f.writesAtTerminal = f.server.snapshot()
			}
		}
		require.True(f.t, terminalSeen, "turn must deliver its terminal event")
	}
	return f
}

func (f *recapFixture) whenCleanupRuns() *recapFixture {
	f.t.Helper()
	f.executor.Cleanup(f.t.Context(), &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "context-1"}, nil, nil)
	return f
}

// awaitRecapStarted fails the test rather than hanging when no recap call starts.
func (f *recapFixture) awaitRecapStarted() {
	f.t.Helper()
	select {
	case <-f.recapper.started:
	case <-time.After(2 * time.Second):
		f.t.Fatal("recap was never requested")
	}
}

func TestRecapCleanupWaitStaysUnderDispatchWindow(t *testing.T) {
	t.Run("Given the gateway retries dispatch for 10s, assert that cleanup waits less than that", func(t *testing.T) {
		assert.Less(t, recapCleanupWait, 10*time.Second)
	})
}

func TestChatRecap(t *testing.T) {
	t.Run("Given a chat on its third turn, when the turn completes and cleanup runs, assert that the recap is written with the turns it covers", func(t *testing.T) {
		f := newRecapFixture(t).whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		require.Len(t, f.recapper.calls(), 1)
		assert.Contains(t, f.recapper.calls()[0], "User: q3")
		assert.Contains(t, f.recapper.calls()[0], "Agent: reply to q3")
		assert.Equal(t, []recapWrite{{"context-1", "Debugged the login page", 3}}, f.server.snapshot())
	})

	t.Run("Given a chat on its first two turns, when each turn completes and cleanup runs, assert that no recap is requested", func(t *testing.T) {
		f := newRecapFixture(t).whenTurnsRun("q1").whenCleanupRuns().whenTurnsRun("q2").whenCleanupRuns()
		assert.Empty(t, f.recapper.calls())
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a slow recapper, when the third turn completes, assert that the terminal event arrives before the recap and cleanup writes it", func(t *testing.T) {
		f := newRecapFixture(t)
		f.recapper.release = make(chan struct{})
		f.whenTurnsRun("q1", "q2", "q3")
		assert.Empty(t, f.writesAtTerminal)
		assert.Empty(t, f.server.snapshot())
		close(f.recapper.release)
		f.whenCleanupRuns()
		assert.Equal(t, []recapWrite{{"context-1", "Debugged the login page", 3}}, f.server.snapshot())
	})

	t.Run("Given a recapper slower than the cleanup cap, when cleanup runs, assert that it returns without writing", func(t *testing.T) {
		f := newRecapFixture(t)
		f.executor.recapWait = 20 * time.Millisecond
		f.recapper.hang = true
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		assert.Len(t, f.recapper.calls(), 1)
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a recap longer than the limit, when cleanup runs, assert that the stored recap is cut to the limit", func(t *testing.T) {
		f := newRecapFixture(t)
		f.recapper.text = strings.Repeat("é", recapMaxLength+50)
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		writes := f.server.snapshot()
		require.Len(t, writes, 1)
		assert.Equal(t, strings.Repeat("é", recapMaxLength), writes[0].text)
	})

	t.Run("Given a recapper that fails, when cleanup runs, assert that it returns without writing", func(t *testing.T) {
		f := newRecapFixture(t)
		f.recapper.err = errors.New("model unavailable")
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		assert.Len(t, f.recapper.calls(), 1)
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a recapper that returns nothing, when cleanup runs, assert that it returns without writing", func(t *testing.T) {
		f := newRecapFixture(t)
		f.recapper.text = "  \n"
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		assert.Len(t, f.recapper.calls(), 1)
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a recap in flight, when a new request starts, assert that the recap is cancelled and nothing is written", func(t *testing.T) {
		f := newRecapFixture(t)
		f.recapper.hang = true
		f.whenTurnsRun("q1", "q2", "q3")
		f.awaitRecapStarted()
		f.givenTurnEndsIn(a2atype.TaskStateInputRequired).whenTurnsRun("q4").whenCleanupRuns()
		assert.True(t, f.recapper.sawCancel())
		assert.Empty(t, f.server.snapshot())
	})

	for _, state := range []a2atype.TaskState{a2atype.TaskStateFailed, a2atype.TaskStateInputRequired} {
		t.Run("Given a chat with two turns, when the third turn ends "+string(state)+", assert that no recap is requested or written", func(t *testing.T) {
			f := newRecapFixture(t).whenTurnsRun("q1", "q2").givenTurnEndsIn(state).whenTurnsRun("q3").whenCleanupRuns()
			assert.Empty(t, f.recapper.calls())
			assert.Empty(t, f.server.snapshot())
		})
	}

	t.Run("Given no recapper, when the third turn completes, assert that nothing is written", func(t *testing.T) {
		f := newRecapFixture(t)
		f.executor.recapper = nil
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		assert.Empty(t, f.server.snapshot())
	})

	t.Run("Given a recapper and no controller client, when the third turn completes, assert that the recapper is not called", func(t *testing.T) {
		f := newRecapFixture(t)
		f.executor.controllerClient = nil
		f.whenTurnsRun("q1", "q2", "q3").whenCleanupRuns()
		assert.Empty(t, f.recapper.calls())
	})
}

func (f *recapFixture) recappingMarkers() (positions []int) {
	for i, event := range f.events {
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Metadata[apia2a.RecappingMetadataKey] == true {
			assert.Equal(f.t, a2atype.TaskStateWorking, update.Status.State)
			assert.Equal(f.t, "task-1", string(update.TaskID))
			assert.Equal(f.t, "context-1", update.ContextID)
			positions = append(positions, i)
		}
	}
	return positions
}

func TestRecappingMarker(t *testing.T) {
	t.Run("Given a chat on its third turn, when the turn completes, assert that one non-terminal marker precedes the completed event", func(t *testing.T) {
		f := newRecapFixture(t).whenTurnsRun("q1", "q2", "q3")
		require.Len(t, f.events, 3)
		assert.Equal(t, []int{1}, f.recappingMarkers())
		last, ok := f.events[2].(*a2atype.TaskStatusUpdateEvent)
		require.True(t, ok)
		assert.Equal(t, a2atype.TaskStateCompleted, last.Status.State)
		assert.Nil(t, last.Metadata[apia2a.RecappingMetadataKey])
	})

	t.Run("Given a chat on its first turn, when the turn completes, assert that no marker is sent", func(t *testing.T) {
		f := newRecapFixture(t).whenTurnsRun("q1")
		assert.Len(t, f.events, 2)
		assert.Empty(t, f.recappingMarkers())
	})

	t.Run("Given a chat with two turns, when the third turn fails, assert that no marker is sent", func(t *testing.T) {
		f := newRecapFixture(t).whenTurnsRun("q1", "q2").givenTurnEndsIn(a2atype.TaskStateFailed).whenTurnsRun("q3")
		assert.Empty(t, f.recappingMarkers())
	})
}

func TestCountUserTurns(t *testing.T) {
	toolResult := &adksession.Event{Author: "user", LLMResponse: adkmodel.LLMResponse{Content: &genai.Content{
		Role: genai.RoleUser, Parts: []*genai.Part{genai.NewPartFromFunctionResponse("search", map[string]any{"ok": true})},
	}}}
	t.Run("Given user text, agent text and a tool result, when turns are counted, assert that only the user text counts", func(t *testing.T) {
		events := []*adksession.Event{
			textEvent("user", "q1"), textEvent("agent", "a1"), toolResult,
			textEvent("user", "q2"), textEvent("agent", "a2"),
		}
		assert.Equal(t, 2, countUserTurns(events))
	})
	t.Run("Given no events, when turns are counted, assert that the count is zero", func(t *testing.T) {
		assert.Zero(t, countUserTurns(nil))
	})
}

func TestBuildTranscript(t *testing.T) {
	t.Run("Given user and agent text, when the transcript is built, assert that each turn is labelled in order", func(t *testing.T) {
		events := []*adksession.Event{textEvent("user", "q1"), textEvent("agent", "a1"), textEvent("user", "q2")}
		transcript := buildTranscript(events, 1000)
		assert.Equal(t, 0, strings.Index(transcript, "User: q1"))
		assert.Less(t, strings.Index(transcript, "User: q1"), strings.Index(transcript, "Agent: a1"))
		assert.Less(t, strings.Index(transcript, "Agent: a1"), strings.Index(transcript, "User: q2"))
	})
	t.Run("Given a chat longer than the cap, when the transcript is built, assert that it is capped and keeps the latest turn", func(t *testing.T) {
		events := []*adksession.Event{
			textEvent("user", strings.Repeat("old ", 100)), textEvent("agent", "the latest answer"),
		}
		transcript := buildTranscript(events, 50)
		assert.LessOrEqual(t, len([]rune(transcript)), 50)
		assert.Contains(t, transcript, "the latest answer")
	})
}
