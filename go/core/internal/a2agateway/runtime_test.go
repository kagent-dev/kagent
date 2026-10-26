package a2agateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	"github.com/kagent-dev/kagent/go/pkg/invocationauth"
)

type runtimeTestAuth struct {
	auth.AuthProvider
	methods               []string
	destinationSessionIDs []string
}

type runtimeTestPolicies struct {
	access   egress.Access
	err      error
	calls    int
	revision string
}

func (p *runtimeTestPolicies) GetRuntimeEgressPolicy(_ context.Context, revision string) (egress.Access, error) {
	p.calls++
	p.revision = revision
	return p.access, p.err
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
	policies := &runtimeTestPolicies{}
	dialer, err := NewRuntimeDialer("http://"+listener.Addr().String(), authenticator, policies)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	client, err := dialer.Dial(ctx, &apiv1.Session{
		Id: "session", A2AAuthority: substrate.ActorHost("team", "session-session", ""), PreparedRevision: "revision",
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
	require.Equal(t, 2, policies.calls, "only sends resolve a new invocation snapshot")
	require.Equal(t, "revision", policies.revision)
	snapshots := 0
	for range 5 {
		select {
		case md := <-received:
			if values := md.Get(invocationauth.HeadersMetadataKey); len(values) > 0 {
				require.Equal(t, []string{"[]"}, values, "empty grants must not fall back to pinned headers")
				snapshots++
			}
			require.Equal(t, []string{"team/session-session"}, md.Get("ate-target-actor"))
			require.Equal(t, []string{"Bearer runtime-test"}, md.Get("authorization"))
			require.Equal(t, []string{listener.Addr().String()}, md.Get(":authority"))
		case <-ctx.Done():
			t.Fatal("runtime did not receive every call")
		}
	}
	require.Equal(t, 2, snapshots)
}

func TestRuntimeDispatchSnapshotsHeaders(t *testing.T) {
	const origin = "https://tools.example:443"
	policies := &runtimeTestPolicies{access: egress.Access{
		Credentials: []egress.Credential{
			{Origin: origin, Header: "x-api-key", Prefix: "Secret ", URI: "ate-secret://k8s.io/default/team/access/key"},
			{Origin: origin, Header: "x-actor", Prefix: "Bearer ", ActorToken: &egress.ActorToken{Audiences: []string{"tools"}, ExpirationSeconds: 900}},
		},
		TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: origin, Header: "authorization", Prefix: "Bearer "}},
	}}
	interceptor := &upstreamAuthInterceptor{policies: policies, session: &apiv1.Session{PreparedRevision: "revision"}}
	request := func() *a2aclient.Request {
		return &a2aclient.Request{Method: "SendMessage", BaseURL: "runtime", ServiceParams: a2aclient.ServiceParams{
			"X-Kagent-Invocation-Headers-Bin": {"forged"},
			invocationauth.HeadersMetadataKey: {"forged"},
		}}
	}
	first := request()
	_, _, err := interceptor.Before(t.Context(), first)
	require.NoError(t, err)
	require.NotContains(t, first.ServiceParams, "X-Kagent-Invocation-Headers-Bin")
	var targets []invocationauth.Target
	require.NoError(t, json.Unmarshal([]byte(first.ServiceParams[invocationauth.HeadersMetadataKey][0]), &targets))
	require.Len(t, targets, 1)
	require.Equal(t, "Bearer ", targets[0].Passthrough[0].Prefix)
	require.Equal(t, "Secret kagent-credential-injected", targets[0].Headers["x-api-key"])
	require.Equal(t, "Bearer kagent-credential-injected", targets[0].Headers["x-actor"])

	policies.access = egress.Access{}
	next := request()
	_, _, err = interceptor.Before(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, []string{"[]"}, next.ServiceParams[invocationauth.HeadersMetadataKey])
	require.Contains(t, first.ServiceParams[invocationauth.HeadersMetadataKey][0], origin)

	policies.err = errors.New("policy unavailable")
	_, _, err = interceptor.Before(t.Context(), request())
	require.ErrorContains(t, err, "resolve invocation header bindings")
	read := request()
	read.Method = "CancelTask"
	_, _, err = interceptor.Before(t.Context(), read)
	require.NoError(t, err, "policy failures must not prevent cancellation")
	require.NotContains(t, read.ServiceParams, invocationauth.HeadersMetadataKey)
}
