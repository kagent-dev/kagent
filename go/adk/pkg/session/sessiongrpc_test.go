package session

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type sessionTestServer struct {
	apiv1alpha1.UnimplementedSessionServiceServer
	create   func(context.Context, *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error)
	get      func(context.Context, *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error)
	delete   func(context.Context, *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error)
	addEvent func(context.Context, *apiv1alpha1.AddSessionEventRequest) (*apiv1alpha1.AddSessionEventResponse, error)
}

func (server *sessionTestServer) CreateSession(ctx context.Context, request *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
	return server.create(ctx, request)
}

func (server *sessionTestServer) GetSession(ctx context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
	return server.get(ctx, request)
}

func (server *sessionTestServer) DeleteSession(ctx context.Context, request *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error) {
	return server.delete(ctx, request)
}

func (server *sessionTestServer) AddSessionEvent(ctx context.Context, request *apiv1alpha1.AddSessionEventRequest) (*apiv1alpha1.AddSessionEventResponse, error) {
	return server.addEvent(ctx, request)
}

func newGRPCService(t *testing.T, service *sessionTestServer) *KAgentSessionService {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apiv1alpha1.RegisterSessionServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()

	client, err := controllerclient.New(controllerclient.Config{
		Target: "passthrough:///bufnet",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		server.Stop()
		require.NoError(t, listener.Close())
	})
	return NewKAgentSessionService(client)
}

func TestCreateUsesGeneratedGRPC(t *testing.T) {
	var gotRequest *apiv1alpha1.CreateSessionRequest
	service := newGRPCService(t, &sessionTestServer{create: func(ctx context.Context, request *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
		gotRequest = request
		values, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"user-1"}, values.Get("x-user-id"))
		return &apiv1alpha1.CreateSessionResponse{Session: &apiv1alpha1.Session{Id: "sess-1", UserId: "user-1"}}, nil
	}})

	response, err := service.Create(t.Context(), &adksession.CreateRequest{
		AppName:   "default__NS__agent",
		UserID:    "user-1",
		SessionID: "sess-1",
		State: map[string]any{
			"session_name": "My Session",
			"source":       "agent",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "sess-1", response.Session.ID())
	assert.Equal(t, "user-1", response.Session.UserID())
	assert.Equal(t, "default__NS__agent", response.Session.AppName())
	require.NotNil(t, gotRequest)
	assert.Equal(t, "sess-1", gotRequest.GetId())
	assert.Equal(t, "My Session", gotRequest.GetName())
	assert.Equal(t, "default__NS__agent", gotRequest.GetAgentRef())
	assert.Equal(t, apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT, gotRequest.GetSource())
}

func TestCreateRejectsUnknownSource(t *testing.T) {
	client := &controllerclient.Client{}
	service := NewKAgentSessionService(client)
	_, err := service.Create(t.Context(), &adksession.CreateRequest{
		AppName: "app",
		UserID:  "user",
		State:   map[string]any{"source": "unknown"},
	})
	require.EqualError(t, err, `create session: unsupported source "unknown"`)
}

func TestGetDeserializesAndFiltersEvents(t *testing.T) {
	after := time.Date(2026, time.July, 27, 10, 30, 0, 123456000, time.UTC)
	newerEvent, err := json.Marshal(map[string]any{"id": "newer", "author": "agent"})
	require.NoError(t, err)
	olderEvent, err := json.Marshal(map[string]any{"id": "older", "author": "user"})
	require.NoError(t, err)

	service := newGRPCService(t, &sessionTestServer{get: func(_ context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
		assert.Equal(t, "sess-filtered", request.GetSessionId())
		assert.Equal(t, apiv1alpha1.EventOrder_EVENT_ORDER_DESCENDING, request.GetOrder())
		assert.Equal(t, int32(2), request.GetLimit())
		assert.Equal(t, after, request.GetAfter().AsTime())
		return &apiv1alpha1.GetSessionResponse{
			Session: &apiv1alpha1.Session{Id: "sess-filtered", UserId: "user"},
			Events: []*apiv1alpha1.SessionEvent{
				{Id: "newer", Data: string(newerEvent)},
				{Id: "older", Data: string(olderEvent)},
			},
		}, nil
	}})

	response, err := service.Get(t.Context(), &adksession.GetRequest{
		AppName:         "app",
		UserID:          "user",
		SessionID:       "sess-filtered",
		After:           after,
		NumRecentEvents: 2,
	})
	require.NoError(t, err)
	events := EventsFromSession(response.Session)
	require.Len(t, events, 2)
	assert.Equal(t, "older", events[0].ID)
	assert.Equal(t, "newer", events[1].ID)
}

func TestGetSkipsEmptyAndMalformedEvents(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{get: func(_ context.Context, request *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
		assert.Equal(t, apiv1alpha1.EventOrder_EVENT_ORDER_ASCENDING, request.GetOrder())
		assert.Nil(t, request.Limit)
		return &apiv1alpha1.GetSessionResponse{
			Session: &apiv1alpha1.Session{Id: "sess-1", UserId: "user"},
			Events: []*apiv1alpha1.SessionEvent{
				{Data: `{}`},
				{Data: `{not-json`},
			},
		}, nil
	}})

	response, err := service.Get(t.Context(), &adksession.GetRequest{AppName: "app", UserID: "user", SessionID: "sess-1"})
	require.NoError(t, err)
	assert.Empty(t, EventsFromSession(response.Session))
}

func TestGetMapsNotFound(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{get: func(context.Context, *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
		return nil, status.Error(codes.NotFound, "missing")
	}})

	_, err := service.Get(t.Context(), &adksession.GetRequest{AppName: "app", UserID: "user", SessionID: "missing"})
	require.ErrorIs(t, err, ErrSessionNotFound)

	session, err := service.GetSession(t.Context(), "app", "user", "missing")
	require.NoError(t, err)
	assert.Nil(t, session)
}

func TestGetSessionReturnsBackendError(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{get: func(context.Context, *apiv1alpha1.GetSessionRequest) (*apiv1alpha1.GetSessionResponse, error) {
		return nil, status.Error(codes.Unavailable, "backend unavailable")
	}})

	session, err := service.GetSession(t.Context(), "app", "user", "broken")
	require.Error(t, err)
	assert.Nil(t, session)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestDeleteUsesGeneratedGRPC(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{delete: func(_ context.Context, request *apiv1alpha1.DeleteSessionRequest) (*apiv1alpha1.DeleteSessionResponse, error) {
		assert.Equal(t, "sess-1", request.GetSessionId())
		return &apiv1alpha1.DeleteSessionResponse{}, nil
	}})

	err := service.Delete(t.Context(), &adksession.DeleteRequest{AppName: "app", UserID: "user", SessionID: "sess-1"})
	require.NoError(t, err)
}

func TestAppendEventPersistsAndUpdatesLocalSession(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{addEvent: func(ctx context.Context, request *apiv1alpha1.AddSessionEventRequest) (*apiv1alpha1.AddSessionEventResponse, error) {
		assert.Equal(t, "sess-1", request.GetSessionId())
		assert.Equal(t, "evt-1", request.GetId())
		persisted := new(adksession.Event)
		require.NoError(t, json.Unmarshal([]byte(request.GetData()), persisted))
		assert.Equal(t, "evt-1", persisted.ID)
		assert.Equal(t, "agent", persisted.Author)
		values, _ := metadata.FromIncomingContext(ctx)
		assert.Equal(t, []string{"user"}, values.Get("x-user-id"))
		_, hasDeadline := ctx.Deadline()
		assert.True(t, hasDeadline)
		return &apiv1alpha1.AddSessionEventResponse{}, nil
	}})
	local := &localSession{appName: "app", userID: "user", sessionID: "sess-1", state: make(map[string]any)}
	event := &adksession.Event{ID: "evt-1", Author: "agent"}

	require.NoError(t, service.AppendEvent(t.Context(), local, event))
	require.Len(t, EventsFromSession(local), 1)
	assert.Equal(t, "evt-1", EventsFromSession(local)[0].ID)
}

func TestCreateSessionConvenienceWrapper(t *testing.T) {
	service := newGRPCService(t, &sessionTestServer{create: func(context.Context, *apiv1alpha1.CreateSessionRequest) (*apiv1alpha1.CreateSessionResponse, error) {
		return &apiv1alpha1.CreateSessionResponse{Session: &apiv1alpha1.Session{Id: "sess-1", UserId: "user"}}, nil
	}})
	require.NoError(t, service.CreateSession(t.Context(), "app", "user", nil, "sess-1"))
}

func TestEventsFromSessionLocalSession(t *testing.T) {
	local := &localSession{
		sessionID: "session",
		events: []*adksession.Event{
			{ID: "event-1", Author: "agent"},
			{ID: "event-2", Author: "user"},
		},
		state: make(map[string]any),
	}
	events := EventsFromSession(local)
	require.Len(t, events, 2)
	assert.Equal(t, "event-1", events[0].ID)
	assert.Equal(t, "event-2", events[1].ID)
}
