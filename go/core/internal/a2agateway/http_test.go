package a2agateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type httpTestAuthenticator struct {
	auth.AuthProvider
}

var _ auth.AuthProvider = httpTestAuthenticator{}

func (httpTestAuthenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	if headers.Get("Authorization") != "Bearer valid" {
		return nil, errors.New("invalid credentials")
	}
	return gatewayTestAuthSession{}, nil
}

type httpTestShares struct {
	permission apiv1alpha1.SessionSharePermission
	sessionID  string
	err        error
	digest     []byte
}

func (s *httpTestShares) GetSessionShareByTokenHash(_ context.Context, digest []byte) (*apiv1alpha1.SessionShare, string, error) {
	s.digest = digest
	return &apiv1alpha1.SessionShare{SessionId: s.sessionID, Permission: s.permission}, "owner", s.err
}

func newHTTPTestServer(t *testing.T, store *gatewayTestStore, authorizer auth.Authorizer, shares *httpTestShares) (*httptest.Server, *gatewayTestRuntime) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	store.task = &a2atype.Task{ID: "runtime-task", ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}
	runtime := &gatewayTestRuntime{task: store.task}
	gateway := newTestGateway(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, "http://"+server.Listener.Addr().String()+"/")
	server.Config.Handler = NewHTTPHandler(gateway, httpTestAuthenticator{}, shares)
	server.Start()
	t.Cleanup(server.Close)
	return server, runtime
}

func httpTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{"authorization": {"Bearer valid"}})
}

func TestHTTPAgentCardDiscoveryAndRouting(t *testing.T) {
	store := &gatewayTestStore{
		session: gatewayTestSession(),
		revision: &database.RuntimeRevision{AgentCard: &a2apb.AgentCard{
			Name: "assistant", Description: "pinned description", Version: "1",
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://private-runtime", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
			Capabilities:        &a2apb.AgentCapabilities{Extensions: []*a2apb.AgentExtension{{Uri: "https://kagent.dev/extensions/hitl/v1"}}},
			DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		}},
	}
	server, _ := newHTTPTestServer(t, store, &gatewayTestAuthorizer{}, nil)
	endpoint := server.URL + HTTPPathPrefix + gatewayTestAgent
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+a2asrv.WellKnownAgentCardPath, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer valid")
	// A conflicting header must never change the URL's authority.
	request.Header.Set("x-kagent-session-id", "12345678-1234-4234-8234-123456789abc")
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))
	require.Equal(t, "private, no-store", response.Header.Get("Cache-Control"))
	var card a2atype.AgentCard
	require.NoError(t, json.NewDecoder(response.Body).Decode(&card))
	require.Equal(t, "pinned description", card.Description)
	require.Empty(t, store.id, "card discovery must not require a conversation")
	require.Empty(t, store.userID)
	require.Len(t, card.SupportedInterfaces, 2)
	require.Equal(t, endpoint, card.SupportedInterfaces[0].URL)
	require.Equal(t, a2atype.TransportProtocolJSONRPC, card.SupportedInterfaces[0].ProtocolBinding)
	require.Equal(t, a2atype.TransportProtocolGRPC, card.SupportedInterfaces[1].ProtocolBinding)
	require.Len(t, card.Capabilities.Extensions, 1)

	// A standard SDK client can use the discovered URL without a routing header.
	client, err := a2aclient.NewFromCard(t.Context(), &card)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })
	extended, err := client.GetExtendedAgentCard(httpTestContext(t), &a2atype.GetExtendedAgentCardRequest{})
	require.NoError(t, err)
	require.Equal(t, &card, extended)
	result, err := client.SendMessage(httpTestContext(t), gatewayTestRequest())
	require.NoError(t, err)
	task, ok := result.(*a2atype.Task)
	require.True(t, ok)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Equal(t, gatewayTestContextID, task.ContextID)
	// JSON-RPC also uses the URL when callers supply conflicting routing headers.
	ctx := a2aclient.AttachServiceParams(httpTestContext(t), a2aclient.ServiceParams{
		"authorization":       {"Bearer valid"},
		"x-kagent-session-id": {"12345678-1234-4234-8234-123456789abc", "invalid"},
	})
	persisted, err := client.GetTask(ctx, &a2atype.GetTaskRequest{Tenant: gatewayTestAgent, ID: task.ID})
	require.NoError(t, err)
	require.Equal(t, task, persisted)
	require.Equal(t, gatewayTestID, store.id)
}

func TestHTTPGatewayStreamingAndCancellation(t *testing.T) {
	store := &gatewayTestStore{session: gatewayTestSession()}
	server, runtime := newHTTPTestServer(t, store, &gatewayTestAuthorizer{}, nil)
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{
		a2atype.NewAgentInterface(server.URL+HTTPPathPrefix+gatewayTestAgent, a2atype.TransportProtocolJSONRPC),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })
	ctx := httpTestContext(t)
	var taskID a2atype.TaskID
	var completed bool
	for event, err := range client.SendStreamingMessage(ctx, gatewayTestRequest()) {
		require.NoError(t, err)
		taskID = event.TaskInfo().TaskID
		if task, ok := event.(*a2atype.Task); ok && task.Status.State == a2atype.TaskStateCompleted {
			completed = true
		}
	}
	require.True(t, completed)
	require.NotEmpty(t, taskID)
	// Reconnect reads the durable task without dispatching another model call.
	var events []a2atype.Event
	for event, err := range client.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{Tenant: gatewayTestAgent, ID: taskID}) {
		require.NoError(t, err)
		events = append(events, event)
	}
	require.Len(t, events, 1)
	require.Equal(t, taskID, events[0].TaskInfo().TaskID)

	// Cancel a quiescent task awaiting input and read the persisted cancellation.
	store.task = &a2atype.Task{ID: "awaiting-input", ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}}
	runtime.task = &a2atype.Task{ID: store.task.ID, ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCanceled}}
	store.settledRead = func() error {
		store.task = runtime.task
		return nil
	}
	canceled, err := client.CancelTask(ctx, &a2atype.CancelTaskRequest{Tenant: gatewayTestAgent, ID: store.task.ID})
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateCanceled, canceled.Status.State)
	persisted, err := client.GetTask(ctx, &a2atype.GetTaskRequest{Tenant: gatewayTestAgent, ID: canceled.ID})
	require.NoError(t, err)
	require.Equal(t, canceled, persisted)
	_, err = client.GetTask(ctx, &a2atype.GetTaskRequest{Tenant: gatewayTestAgent, ID: "missing"})
	require.ErrorIs(t, err, a2atype.ErrTaskNotFound)
}

func TestHTTPGatewayRejectsInvalidAccess(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		path       string
		authorized bool
		storeErr   error
		authzErr   error
		want       int
	}{
		{name: "card needs authentication", method: "GET", path: gatewayTestAgent + a2asrv.WellKnownAgentCardPath, want: 401},
		{name: "RPC needs authentication", method: "POST", path: gatewayTestAgent, want: 401},
		{name: "card needs authorization", method: "GET", path: gatewayTestAgent + a2asrv.WellKnownAgentCardPath, authorized: true, authzErr: errors.New("denied"), want: 403},
		{name: "missing session is hidden", method: "GET", path: gatewayTestAgent + a2asrv.WellKnownAgentCardPath, authorized: true, storeErr: database.ErrNotFound, want: 403},
		{name: "internal errors are hidden", method: "GET", path: gatewayTestAgent + a2asrv.WellKnownAgentCardPath, authorized: true, storeErr: errors.New("private connection details"), want: 500},
		{name: "invalid session ID", method: "GET", path: "invalid!/assistant" + a2asrv.WellKnownAgentCardPath, authorized: true, want: 400},
		{name: "unknown route", method: "GET", path: gatewayTestAgent + "/unknown", authorized: true, want: 404},
		{name: "card only supports reads", method: "POST", path: gatewayTestAgent + a2asrv.WellKnownAgentCardPath, authorized: true, want: 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &gatewayTestStore{session: gatewayTestSession(), err: test.storeErr}
			handler := NewHTTPHandler(newTestGateway(store, &gatewayTestAuthorizer{err: test.authzErr}, nil, gatewayTestURL), httpTestAuthenticator{}, nil)
			request := httptest.NewRequest(test.method, HTTPPathPrefix+test.path, nil)
			if test.authorized {
				request.Header.Set("Authorization", "Bearer valid")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.want, response.Code)
			require.NotContains(t, response.Body.String(), "private connection details")
		})
	}
}

func TestGatewaySharePermissionsAcrossTransports(t *testing.T) {
	for _, protocol := range []a2atype.TransportProtocol{a2atype.TransportProtocolJSONRPC, a2atype.TransportProtocolGRPC} {
		t.Run(string(protocol), func(t *testing.T) {
			for _, test := range []struct {
				name       string
				permission apiv1alpha1.SessionSharePermission
				sessionID  string
				storeErr   error
				wantStatus int
				canRead    bool
				canWrite   bool
			}{
				{name: "read only", permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY, sessionID: gatewayTestID, canRead: true},
				{name: "read write", permission: apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_WRITE, sessionID: gatewayTestID, canRead: true, canWrite: true},
				{name: "other session", sessionID: "12345678-1234-4234-8234-123456789abc"},
				{name: "expired", storeErr: database.ErrNotFound, wantStatus: 403},
				{name: "store unavailable", storeErr: errors.New("database credentials"), wantStatus: 500},
			} {
				t.Run(test.name, func(t *testing.T) {
					store := &gatewayTestStore{session: gatewayTestSession(), task: &a2atype.Task{ID: "task", ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}}
					shares := &httpTestShares{permission: test.permission, sessionID: test.sessionID, err: test.storeErr}
					runtime := &gatewayTestRuntime{cancelErr: a2atype.ErrTaskNotFound}
					gateway := newTestGateway(store, &gatewayDenyAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, gatewayTestURL)
					address := startCoreTestServer(t, gateway, shares)
					var transport a2aclient.Transport
					params := a2aclient.ServiceParams{"authorization": {"Bearer valid"}, "x-share-token": {"share"}}
					if protocol == a2atype.TransportProtocolGRPC {
						connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
						require.NoError(t, err)
						transport = a2agrpc.NewGRPCTransport(connection)
					} else {
						httpClient := &http.Client{Transport: &http.Transport{}}
						t.Cleanup(httpClient.CloseIdleConnections)
						transport = a2aclient.NewJSONRPCTransport("http://"+address+HTTPPathPrefix+gatewayTestAgent, httpClient)
					}
					t.Cleanup(func() { require.NoError(t, transport.Destroy()) })
					_, err := transport.GetTask(t.Context(), params, &a2atype.GetTaskRequest{Tenant: gatewayTestAgent, ID: "task"})
					if test.canRead {
						require.NoError(t, err)
						require.Equal(t, "owner", store.userID)
					} else {
						require.Error(t, err)
					}
					digest := sha256.Sum256([]byte("share"))
					require.Equal(t, digest[:], shares.digest)
					_, err = transport.CancelTask(t.Context(), params, &a2atype.CancelTaskRequest{Tenant: gatewayTestAgent, ID: "task"})
					if test.canWrite {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					_, err = transport.SendMessage(t.Context(), params, gatewayTestRequest())
					if test.canWrite {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					if !test.canWrite {
						errorsSeen := 0
						for _, err := range transport.SendStreamingMessage(t.Context(), params, gatewayTestRequest()) {
							require.Error(t, err)
							errorsSeen++
						}
						require.Equal(t, 1, errorsSeen)
						require.Empty(t, store.stored)
					}
					if test.wantStatus != 0 && protocol == a2atype.TransportProtocolJSONRPC {
						require.Contains(t, err.Error(), http.StatusText(test.wantStatus))
					}
				})
			}
		})
	}
}

func TestHTTPGatewayMalformedJSONRPC(t *testing.T) {
	store := &gatewayTestStore{}
	handler := NewHTTPHandler(newTestGateway(store, &gatewayTestAuthorizer{}, nil, gatewayTestURL), httpTestAuthenticator{}, nil)
	request := httptest.NewRequest(http.MethodPost, HTTPPathPrefix+gatewayTestAgent, strings.NewReader(`{"jsonrpc":`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"code":-32700`)
	require.Empty(t, store.id)
}

func startCoreTestServer(t *testing.T, gateway a2asrv.RequestHandler, shares sessionsvc.ShareStore) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server, err := grpcserver.New(grpcserver.Config{
		Listener: listener, Authenticator: httpTestAuthenticator{}, ShareStore: shares,
		SystemService: systemservice.NewService(nil, nil, nil, nil),
		A2AHandler:    gateway, HTTPHandler: NewHTTPHandler(gateway, httpTestAuthenticator{}, shares),
	})
	require.NoError(t, err)
	// Close the clients before shutting down their server during test cleanup.
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("core listener did not stop")
		}
	})

	return listener.Addr().String()
}
