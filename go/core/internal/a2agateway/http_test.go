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
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type httpTestAuthenticator struct {
	auth.AuthProvider
}

var _ auth.AuthProvider = httpTestAuthenticator{}

func (httpTestAuthenticator) Authenticate(_ context.Context, headers http.Header, _ url.Values) (auth.Session, error) {
	if headers.Get("Authorization") != "Bearer valid" {
		return nil, errors.New("invalid credentials")
	}
	return gatewayTestSession{}, nil
}

type httpTestShares struct {
	permission apiv1alpha1.AgentInstanceSharePermission
	instanceID string
	err        error
	digest     []byte
}

func (s *httpTestShares) GetAgentInstanceShareByTokenHash(_ context.Context, digest []byte) (*apiv1alpha1.AgentInstanceShare, string, error) {
	s.digest = digest
	return &apiv1alpha1.AgentInstanceShare{AgentInstanceId: s.instanceID, Permission: s.permission}, "owner", s.err
}

func newHTTPTestServer(t *testing.T, store *gatewayTestStore, authorizer auth.Authorizer, shares *httpTestShares) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	runtime := &gatewayTestRuntime{cancelErr: a2atype.ErrTaskNotFound}
	gateway := New(store, authorizer, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, "http://"+server.Listener.Addr().String()+"/")
	server.Config.Handler = NewHTTPHandler(gateway, httpTestAuthenticator{}, shares)
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func httpTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	return a2aclient.AttachServiceParams(ctx, a2aclient.ServiceParams{"authorization": {"Bearer valid"}})
}

func TestHTTPAgentCardDiscoveryAndRouting(t *testing.T) {
	store := &gatewayTestStore{
		instance: gatewayTestInstance(),
		revision: &database.RuntimeRevision{AgentCard: &a2apb.AgentCard{
			Name: "assistant", Description: "pinned description", Version: "1",
			SupportedInterfaces: []*a2apb.AgentInterface{{Url: "http://private-runtime", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"}},
			Capabilities:        &a2apb.AgentCapabilities{Extensions: []*a2apb.AgentExtension{{Uri: "https://kagent.dev/extensions/hitl/v1"}}},
			DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		}},
	}
	server := newHTTPTestServer(t, store, &gatewayTestAuthorizer{}, nil)
	endpoint := server.URL + HTTPPathPrefix + gatewayTestID
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+a2asrv.WellKnownAgentCardPath, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer valid")
	// A conflicting header must never change the URL's authority.
	request.Header.Set(apia2a.AgentInstanceIDHeader, "12345678-1234-4234-8234-123456789abc")
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "application/json", response.Header.Get("Content-Type"))
	require.Equal(t, "private, no-store", response.Header.Get("Cache-Control"))
	var card a2atype.AgentCard
	require.NoError(t, json.NewDecoder(response.Body).Decode(&card))
	require.Equal(t, "pinned description", card.Description)
	require.Equal(t, gatewayTestID, store.id)
	require.Equal(t, "alice", store.userID)
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
	persisted, err := client.GetTask(httpTestContext(t), &a2atype.GetTaskRequest{ID: task.ID})
	require.NoError(t, err)
	require.Equal(t, task, persisted)
}

func TestHTTPGatewayStreamingAndCancellation(t *testing.T) {
	store := &gatewayTestStore{instance: gatewayTestInstance()}
	server := newHTTPTestServer(t, store, &gatewayTestAuthorizer{}, nil)
	client, err := a2aclient.NewFromEndpoints(t.Context(), []*a2atype.AgentInterface{
		a2atype.NewAgentInterface(server.URL+HTTPPathPrefix+gatewayTestID, a2atype.TransportProtocolJSONRPC),
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
	for event, err := range client.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{ID: taskID}) {
		require.NoError(t, err)
		events = append(events, event)
	}
	require.Len(t, events, 1)
	require.Equal(t, taskID, events[0].TaskInfo().TaskID)

	// Cancel a quiescent task awaiting input and read the persisted cancellation.
	store.task = &a2atype.Task{ID: "awaiting-input", ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}}
	canceled, err := client.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: store.task.ID})
	require.NoError(t, err)
	require.Equal(t, a2atype.TaskStateCanceled, canceled.Status.State)
	persisted, err := client.GetTask(ctx, &a2atype.GetTaskRequest{ID: canceled.ID})
	require.NoError(t, err)
	require.Equal(t, canceled, persisted)
	_, err = client.GetTask(ctx, &a2atype.GetTaskRequest{ID: "missing"})
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
		{name: "card needs authentication", method: "GET", path: gatewayTestID + a2asrv.WellKnownAgentCardPath, want: 401},
		{name: "RPC needs authentication", method: "POST", path: gatewayTestID, want: 401},
		{name: "card needs authorization", method: "GET", path: gatewayTestID + a2asrv.WellKnownAgentCardPath, authorized: true, authzErr: errors.New("denied"), want: 403},
		{name: "missing instance is hidden", method: "GET", path: gatewayTestID + a2asrv.WellKnownAgentCardPath, authorized: true, storeErr: database.ErrNotFound, want: 403},
		{name: "internal errors are hidden", method: "GET", path: gatewayTestID + a2asrv.WellKnownAgentCardPath, authorized: true, storeErr: errors.New("private connection details"), want: 500},
		{name: "invalid instance ID", method: "GET", path: "invalid" + a2asrv.WellKnownAgentCardPath, authorized: true, want: 400},
		{name: "unknown route", method: "GET", path: gatewayTestID + "/unknown", authorized: true, want: 404},
		{name: "card only supports reads", method: "POST", path: gatewayTestID + a2asrv.WellKnownAgentCardPath, authorized: true, want: 405},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &gatewayTestStore{instance: gatewayTestInstance(), err: test.storeErr}
			handler := NewHTTPHandler(New(store, &gatewayTestAuthorizer{err: test.authzErr}, nil, nil, gatewayTestURL), httpTestAuthenticator{}, nil)
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

func TestHTTPGatewayShares(t *testing.T) {
	for _, test := range []struct {
		name       string
		permission apiv1alpha1.AgentInstanceSharePermission
		instanceID string
		storeErr   error
		wantStatus int
		canRead    bool
		canWrite   bool
	}{
		{name: "read only", permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY, instanceID: gatewayTestID, canRead: true},
		{name: "read write", permission: apiv1alpha1.AgentInstanceSharePermission_AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE, instanceID: gatewayTestID, canRead: true, canWrite: true},
		{name: "other instance", instanceID: "12345678-1234-4234-8234-123456789abc"},
		{name: "expired", storeErr: database.ErrNotFound, wantStatus: 403},
		{name: "store unavailable", storeErr: errors.New("database credentials"), wantStatus: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &gatewayTestStore{instance: gatewayTestInstance(), task: &a2atype.Task{ID: "task", ContextID: gatewayTestContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired}}}
			shares := &httpTestShares{permission: test.permission, instanceID: test.instanceID, err: test.storeErr}
			server := newHTTPTestServer(t, store, &gatewayDenyAuthorizer{}, shares)
			transport := a2aclient.NewJSONRPCTransport(server.URL+HTTPPathPrefix+gatewayTestID, server.Client())
			params := a2aclient.ServiceParams{"authorization": {"Bearer valid"}, "x-share-token": {"share"}}
			_, err := transport.GetTask(t.Context(), params, &a2atype.GetTaskRequest{ID: "task"})
			if test.canRead {
				require.NoError(t, err)
				require.Equal(t, "owner", store.userID)
			} else {
				require.Error(t, err)
			}
			digest := sha256.Sum256([]byte("share"))
			require.Equal(t, digest[:], shares.digest)
			_, err = transport.CancelTask(t.Context(), params, &a2atype.CancelTaskRequest{ID: "task"})
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
				for _, err := range transport.SendStreamingMessage(t.Context(), params, gatewayTestRequest()) {
					require.Error(t, err)
				}
				require.Empty(t, store.stored)
			}
			if test.wantStatus != 0 {
				require.Contains(t, err.Error(), http.StatusText(test.wantStatus))
			}
		})
	}
}

func TestHTTPGatewayMalformedJSONRPC(t *testing.T) {
	store := &gatewayTestStore{}
	handler := NewHTTPHandler(New(store, &gatewayTestAuthorizer{}, nil, nil, gatewayTestURL), httpTestAuthenticator{}, nil)
	request := httptest.NewRequest(http.MethodPost, HTTPPathPrefix+gatewayTestID, strings.NewReader(`{"jsonrpc":`))
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"code":-32700`)
	require.Empty(t, store.id)
}

func TestHTTPAndGRPCShareDurableTasks(t *testing.T) {
	store, instance := gatewayPostgresFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	runtime := &gatewayTestRuntime{}
	gateway := New(store, &gatewayTestAuthorizer{}, &gatewayTestDialer{client: gatewayTestClient(t, runtime)}, &gatewayTestWorkflow{}, "http://"+listener.Addr().String())
	server, err := grpcserver.New(grpcserver.Config{
		Listener: listener, Authenticator: httpTestAuthenticator{},
		SystemService: systemservice.NewService(nil, nil, nil, nil),
		A2AHandler:    gateway, HTTPHandler: NewHTTPHandler(gateway, httpTestAuthenticator{}, store),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
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

	httpClient := a2aclient.NewJSONRPCTransport("http://"+listener.Addr().String()+HTTPPathPrefix+instance.GetId(), http.DefaultClient)
	params := a2aclient.ServiceParams{"authorization": {"Bearer valid"}}
	result, err := httpClient.SendMessage(ctx, params, gatewayTestRequest())
	require.NoError(t, err)
	task, ok := result.(*a2atype.Task)
	require.True(t, ok)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Equal(t, instance.GetContextId(), task.ContextID)

	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	grpcClient := a2apb.NewA2AServiceClient(connection)
	grpcContext := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer valid", apia2a.AgentInstanceIDHeader, instance.GetId())
	getRequest, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: task.ID})
	require.NoError(t, err)
	storedProto, err := grpcClient.GetTask(grpcContext, getRequest)
	require.NoError(t, err)
	stored, err := pbconv.FromProtoTask(storedProto)
	require.NoError(t, err)
	requireSameTask(t, task, stored)

	sendRequest, err := pbconv.ToProtoSendMessageRequest(gatewayTestRequest())
	require.NoError(t, err)
	response, err := grpcClient.SendMessage(grpcContext, sendRequest)
	require.NoError(t, err)
	grpcResult, err := pbconv.FromProtoSendMessageResponse(response)
	require.NoError(t, err)
	grpcTask, ok := grpcResult.(*a2atype.Task)
	require.True(t, ok)
	stored, err = httpClient.GetTask(ctx, params, &a2atype.GetTaskRequest{ID: grpcTask.ID})
	require.NoError(t, err)
	requireSameTask(t, grpcTask, stored)
}

func requireSameTask(t *testing.T, expected, actual *a2atype.Task) {
	t.Helper()
	// Protobuf and JSON decode empty collections differently. Compare their
	// canonical protocol representation, including history and timestamps.
	expectedProto, err := pbconv.ToProtoTask(expected)
	require.NoError(t, err)
	actualProto, err := pbconv.ToProtoTask(actual)
	require.NoError(t, err)
	require.True(t, proto.Equal(expectedProto, actualProto), "tasks differ: expected %v, actual %v", expectedProto, actualProto)
}
