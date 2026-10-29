package a2agateway

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type runtimeTestAuth struct {
	auth.AuthProvider
	methods               []string
	destinationSessionIDs []string
}

func (a *runtimeTestAuth) UpstreamAuth(req *http.Request, _ auth.Session, destinationSessionID string) error {
	method, _ := auth.UpstreamMethodFrom(req.Context())
	a.methods = append(a.methods, method)
	a.destinationSessionIDs = append(a.destinationSessionIDs, destinationSessionID)
	req.Header.Set("Authorization", "Bearer runtime-test")
	req.Header.Set("ate-target-actor", "wrong/actor")
	return nil
}

func TestRuntimeDialerRoutesUnaryAndStreamingCalls(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	received := make(chan metadata.MD, 5)
	observe := func(ctx context.Context) error {
		md, _ := metadata.FromIncomingContext(ctx)
		received <- md
		return status.Error(codes.Unimplemented, "routing observed")
	}
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
			return nil, observe(ctx)
		}),
		grpc.StreamInterceptor(func(_ any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, _ grpc.StreamHandler) error {
			return observe(stream.Context())
		}),
	)
	a2apb.RegisterA2AServiceServer(server, &a2apb.UnimplementedA2AServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	authenticator := &runtimeTestAuth{}
	dialer, err := NewRuntimeDialer("http://"+listener.Addr().String(), authenticator)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	client, err := dialer.Dial(ctx, &apiv1.Session{
		Id: "session", A2AAuthority: substrate.ActorHost("team", "session-session", ""),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })

	send := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))}
	subscribe := &a2atype.SubscribeToTaskRequest{ID: "task"}
	get := &a2atype.GetTaskRequest{ID: "task"}
	cancelTask := &a2atype.CancelTaskRequest{ID: "task"}
	_, err = client.SendMessage(ctx, send)
	require.Error(t, err)
	for _, err := range client.SendStreamingMessage(ctx, send) {
		require.Error(t, err)
	}
	for _, err := range client.SubscribeToTask(ctx, subscribe) {
		require.Error(t, err)
	}
	_, err = client.GetTask(ctx, get)
	require.Error(t, err)
	_, err = client.CancelTask(ctx, cancelTask)
	require.Error(t, err)
	require.Equal(t, []string{"SendMessage", "SendStreamingMessage", "SubscribeToTask", "GetTask", "CancelTask"}, authenticator.methods)
	require.Equal(t, []string{"session", "session", "session", "session", "session"}, authenticator.destinationSessionIDs)
	for range 5 {
		select {
		case md := <-received:
			require.Equal(t, []string{"team/session-session"}, md.Get("ate-target-actor"))
			require.Equal(t, []string{"Bearer runtime-test"}, md.Get("authorization"))
			require.Equal(t, []string{listener.Addr().String()}, md.Get(":authority"))
		case <-ctx.Done():
			t.Fatal("runtime did not receive every call")
		}
	}
}
