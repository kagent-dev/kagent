package session

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
)

func TestInvokeWithAgent(t *testing.T) {
	t.Run("Given an Agent reference, when invoking without a Session, assert that a Session is created and the turn is routed through it", func(t *testing.T) {
		client := newInvokeFixture(t, &invokeSessionService{session: invokeSession()}, &invokeA2AService{})
		var stdout, stderr bytes.Buffer

		err := runInvoke(t.Context(), client.options, &InvokeCfg{
			OutputFormat: "json", Task: "hello", Agent: "k8s-101",
		}, strings.NewReader(""), &stdout, &stderr)

		require.NoError(t, err)

		createRequests := client.sessions.createRequests()
		require.Len(t, createRequests, 1)
		assert.Equal(t, &apiv1.ResourceReference{Namespace: "team-a", Name: "k8s-101"}, createRequests[0].GetAgent())
		require.NoError(t, uuid.Validate(createRequests[0].GetRequestId()), "request ID must be generated for replay")
		assert.Equal(t, []string{testSessionID}, client.sessions.getRequests(), "the A2A client must resolve the created Session")

		assert.Equal(t, "Session: "+testSessionID+"\nTo continue: --session "+testSessionID+"\n", stderr.String())

		output := decodeInvokeJSON(t, stdout.Bytes())
		assert.Contains(t, output, "task", "stdout must keep the A2A response schema")
		assert.NotContains(t, output, "session", "the Session ID must not leak into stdout")
		assert.NotContains(t, output, "sessionId")

		assert.Equal(t, []a2aTurn{{tenant: "team-a/smoke", contextID: testSessionID}}, client.a2a.turns())
	})

	t.Run("Given the Session service rejects creation, when invoking without a Session, assert that no turn is dispatched", func(t *testing.T) {
		client := newInvokeFixture(t, &invokeSessionService{createErr: status.Error(codes.Internal, "boom")}, &invokeA2AService{})
		var stdout, stderr bytes.Buffer

		err := runInvoke(t.Context(), client.options, &InvokeCfg{
			OutputFormat: "json", Task: "hello", Agent: "k8s-101",
		}, strings.NewReader(""), &stdout, &stderr)

		require.ErrorContains(t, err, "create Session")
		assert.Empty(t, stdout.String())
		assert.Empty(t, stderr.String())
		assert.Empty(t, client.sessions.getRequests())
		assert.Empty(t, client.a2a.turns())
	})
}

func decodeInvokeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	require.True(t, json.Valid(data), "stdout must be valid JSON: %s", data)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	return decoded
}

type a2aTurn struct {
	tenant    string
	contextID string
}

type invokeFixture struct {
	sessions *invokeSessionService
	a2a      *invokeA2AService
	options  connection.Options
}

// newInvokeFixture serves the control-plane and gateway APIs in-process so the
// command runs end to end without a cluster.
func newInvokeFixture(t *testing.T, sessions *invokeSessionService, a2a *invokeA2AService) *invokeFixture {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1.RegisterSessionServiceServer(server, sessions)
	a2apb.RegisterA2AServiceServer(server, a2a)
	healthpb.RegisterHealthServer(server, health.NewServer())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, <-done)
		_ = listener.Close()
	})

	options := connection.DefaultOptions()
	options.Namespace = "team-a"
	options.APIURL = "http://api.invalid:80"
	options.GatewayURL = "http://gateway.invalid:80"
	options.Timeout = 5 * time.Second
	options.DialOptions = []grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	}
	return &invokeFixture{sessions: sessions, a2a: a2a, options: options}
}

type invokeSessionService struct {
	apiv1.UnimplementedSessionServiceServer

	mu        sync.Mutex
	create    []*apiv1.CreateSessionRequest
	get       []string
	createErr error
	session   *apiv1.Session
}

func (s *invokeSessionService) CreateSession(_ context.Context, request *apiv1.CreateSessionRequest) (*apiv1.CreateSessionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.create = append(s.create, request)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &apiv1.CreateSessionResponse{Session: s.session}, nil
}

func (s *invokeSessionService) GetSession(_ context.Context, request *apiv1.GetSessionRequest) (*apiv1.GetSessionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.get = append(s.get, request.GetSessionId())
	session := s.session
	if session == nil {
		session = &apiv1.Session{Id: request.GetSessionId()}
	}
	return &apiv1.GetSessionResponse{Session: session}, nil
}

func invokeSession() *apiv1.Session {
	return &apiv1.Session{
		Id:    testSessionID,
		State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team-a", Name: "smoke"}},
	}
}

func (s *invokeSessionService) createRequests() []*apiv1.CreateSessionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*apiv1.CreateSessionRequest(nil), s.create...)
}

func (s *invokeSessionService) getRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.get...)
}

type invokeA2AService struct {
	a2apb.UnimplementedA2AServiceServer

	mu    sync.Mutex
	calls []a2aTurn
}

func (s *invokeA2AService) SendMessage(_ context.Context, request *a2apb.SendMessageRequest) (*a2apb.SendMessageResponse, error) {
	s.mu.Lock()
	s.calls = append(s.calls, a2aTurn{tenant: request.GetTenant(), contextID: request.GetMessage().GetContextId()})
	s.mu.Unlock()
	return pbconv.ToProtoSendMessageResponse(&a2atype.Task{
		ID:        "task-1",
		ContextID: request.GetMessage().GetContextId(),
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
		Artifacts: []*a2atype.Artifact{{ID: "answer", Parts: a2atype.ContentParts{a2atype.NewTextPart("answer")}}},
	})
}

func (s *invokeA2AService) turns() []a2aTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]a2aTurn(nil), s.calls...)
}
