package a2agateway

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
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
	client, err := dialer.Dial(ctx, &apiv1alpha1.AgentInstance{
		Id: "instance", A2AAuthority: substrate.ActorHost("team", "ai-instance", ""),
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
			require.Equal(t, []string{"team/ai-instance"}, md.Get("ate-target-actor"))
			require.Equal(t, []string{"Bearer runtime-test"}, md.Get("authorization"))
			require.Equal(t, []string{listener.Addr().String()}, md.Get(":authority"))
		case <-ctx.Done():
			t.Fatal("runtime did not receive both calls")
		}
	}
}

// TestCallerHeadersInterceptor verifies that caller-supplied custom headers on
// the public gateway request are relayed to the runtime call, while
// credential, hop-by-hop, transport, and gRPC system metadata are not.
func TestCallerHeadersInterceptor(t *testing.T) {
	ctx, _ := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		"x-guardrail-token": {"caller-token"},
		"X-User-Email":      {"user@example.com"},
		"Authorization":     {"Bearer caller"},
		"Cookie":            {"session=abc"},
		"content-type":      {"application/grpc"},
		"grpc-timeout":      {"5S"},
		":authority":        {"gateway"},
		"traceparent":       {"00-abc-def-01"},
		"x-empty":           {""},
	}))
	req := &a2aclient.Request{ServiceParams: a2aclient.ServiceParams{}}

	if _, _, err := (&callerHeadersInterceptor{}).Before(ctx, req); err != nil {
		t.Fatalf("Before() error = %v", err)
	}

	for key, want := range map[string]string{
		"x-guardrail-token": "caller-token",
		"x-user-email":      "user@example.com",
	} {
		if got := req.ServiceParams.Get(key); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want [%s]", key, got, want)
		}
	}
	for _, key := range []string{
		"Authorization", "Cookie", "content-type", "grpc-timeout", ":authority", "traceparent", "x-empty",
	} {
		if got := req.ServiceParams.Get(key); len(got) != 0 {
			t.Errorf("%s must not be forwarded, got %v", key, got)
		}
	}
}

// TestCallerHeadersInterceptor_NoCallContext verifies the interceptor is a
// no-op for calls without a public call context (e.g. internal invocations).
func TestCallerHeadersInterceptor_NoCallContext(t *testing.T) {
	req := &a2aclient.Request{ServiceParams: a2aclient.ServiceParams{}}
	if _, _, err := (&callerHeadersInterceptor{}).Before(context.Background(), req); err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if got := req.ServiceParams.Get("x-guardrail-token"); len(got) != 0 {
		t.Errorf("expected no forwarded params, got %v", got)
	}
}
