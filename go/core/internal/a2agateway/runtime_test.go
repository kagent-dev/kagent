package a2agateway

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/kagent-dev/kagent/go/api/adk"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type runtimeTestAuth struct{ auth.AuthProvider }

func (runtimeTestAuth) UpstreamAuth(req *http.Request, _ auth.Session, _ auth.Principal) error {
	req.Header.Set("Authorization", "Bearer runtime-test")
	req.Header.Set("ate-target-actor", "wrong/actor")
	return nil
}

// claimsSession is a caller whose identity the authenticator resolved from a
// validated token; the control-plane session and an unsecure-mode session
// carry no claims.
type claimsSession struct{ user string }

func (s claimsSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: s.user}, Claims: map[string]any{"email": s.user}}
}

// The runtime learns the person of the turn only when the gateway resolved
// one: a session with claims forwards x-kagent-user, any other forwards none.
func TestRuntimeDialerForwardsTheResolvedPersonOnly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	received := make(chan metadata.MD, 2)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		received <- md
		return nil, status.Error(codes.Unimplemented, "observed")
	}))
	a2apb.RegisterA2AServiceServer(server, &a2apb.UnimplementedA2AServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	dialer, err := NewRuntimeDialer("http://"+listener.Addr().String(), runtimeTestAuth{})
	require.NoError(t, err)
	for _, tt := range []struct {
		name    string
		session auth.Session
		want    []string
	}{
		{name: "resolved person", session: claimsSession{user: "alice@example.com"}, want: []string{"alice@example.com"}},
		{name: "control plane", session: auth.ControlPlaneSession{}, want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ctx = auth.AuthSessionTo(ctx, tt.session)
			client, err := dialer.Dial(ctx, &apiv1alpha1.AgentInstance{Id: "instance", A2AAuthority: substrate.ActorHost("team", "ai-instance", "")})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Destroy()) })
			_, err = client.GetTask(ctx, &a2atype.GetTaskRequest{ID: "task"})
			require.Error(t, err)
			select {
			case md := <-received:
				require.Equal(t, tt.want, md.Get(adk.UserHeader))
				require.Equal(t, []string{"Bearer runtime-test"}, md.Get("authorization"))
			case <-ctx.Done():
				t.Fatal("runtime did not receive the call")
			}
		})
	}
}

func TestRuntimeDialerRoutesUnaryAndStreamingCalls(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	received := make(chan metadata.MD, 2)
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
	dialer, err := NewRuntimeDialer("http://"+listener.Addr().String(), runtimeTestAuth{})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	client, err := dialer.Dial(ctx, &apiv1alpha1.Session{
		Id: "session", A2AAuthority: substrate.ActorHost("team", "session-session", ""),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Destroy()) })

	_, err = client.GetTask(ctx, &a2atype.GetTaskRequest{ID: "task"})
	require.Error(t, err)
	for _, err := range client.SendStreamingMessage(ctx, &a2atype.SendMessageRequest{
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello")),
	}) {
		require.Error(t, err)
	}
	for range 2 {
		select {
		case md := <-received:
			require.Equal(t, []string{"team/session-session"}, md.Get("ate-target-actor"))
			require.Equal(t, []string{"Bearer runtime-test"}, md.Get("authorization"))
			require.Equal(t, []string{listener.Addr().String()}, md.Get(":authority"))
		case <-ctx.Done():
			t.Fatal("runtime did not receive both calls")
		}
	}
}
