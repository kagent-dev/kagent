package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

type staticTokenProvider struct {
	token string
	err   error
	hint  string
	calls int
	conns []grpc.ClientConnInterface
}

func (p *staticTokenProvider) Token(ctx context.Context) (string, error) {
	p.calls++
	cc, _ := ConnFromContext(ctx)
	p.conns = append(p.conns, cc)
	return p.token, p.err
}

func (p *staticTokenProvider) UnauthenticatedHint() string { return p.hint }

// tokenTestServer records the authorization each call carries and rejects
// every call as Unauthenticated when reject is set.
type tokenTestServer struct {
	mu             sync.Mutex
	authorizations [][]string
	reject         bool
}

func (s *tokenTestServer) observe(ctx context.Context) error {
	md, _ := metadata.FromIncomingContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizations = append(s.authorizations, md.Get("authorization"))
	if s.reject {
		return status.Error(codes.Unauthenticated, "invalid credentials")
	}
	return nil
}

func (s *tokenTestServer) observed() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorizations
}

func startTokenTestServer(t *testing.T, reject bool) (*tokenTestServer, ClientOption) {
	t.Helper()
	recorder := &tokenTestServer{reject: reject}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if err := recorder.observe(ctx); err != nil {
				return nil, err
			}
			return handler(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			if err := recorder.observe(stream.Context()); err != nil {
				return err
			}
			return handler(srv, stream)
		}),
	)
	grpc_health_v1.RegisterHealthServer(server, health.NewServer())
	a2apb.RegisterA2AServiceServer(server, &recordingA2AService{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	return recorder, WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}))
}

// callEveryKind makes a health check, an A2A unary call and an A2A stream:
// the reachability check, a control-plane call and agent traffic.
func callEveryKind(t *testing.T, url string, options ...ClientOption) []error {
	t.Helper()
	errs := []error{CheckHealth(context.Background(), url, options...)}
	gateway, err := NewGateway(url, options...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = gateway.Close() })
	a2aClient, err := gateway.A2A.ForAgent(context.Background(), &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"})
	require.NoError(t, err)
	request := &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))}
	_, err = a2aClient.SendMessage(context.Background(), request)
	errs = append(errs, err)
	var streamErr error
	for _, err := range a2aClient.SendStreamingMessage(context.Background(), request) {
		if err != nil {
			streamErr = err
		}
	}
	return append(errs, streamErr)
}

func TestTokenProviderReachesEveryCall(t *testing.T) {
	server, dial := startTokenTestServer(t, false)
	provider := &staticTokenProvider{token: "user-token"}

	for _, err := range callEveryKind(t, "http://localhost:8083", dial, WithTokenProvider(provider)) {
		require.NoError(t, err)
	}

	require.Equal(t, [][]string{{"Bearer user-token"}, {"Bearer user-token"}, {"Bearer user-token"}}, server.observed())
	require.Equal(t, 3, provider.calls, "the token is fetched for each call, so a refreshed token takes effect")
	for _, cc := range provider.conns {
		require.NotNil(t, cc, "the provider sees the connection of the call it serves")
	}
}

func TestTokenProviderReplacesCallerAuthorization(t *testing.T) {
	server, dial := startTokenTestServer(t, false)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer stale")

	require.NoError(t, CheckHealth(ctx, "http://localhost:8083", dial, WithTokenProvider(&staticTokenProvider{token: "user-token"})))

	require.Equal(t, [][]string{{"Bearer user-token"}}, server.observed())
}

func TestTokenTransportRule(t *testing.T) {
	for _, tt := range []struct {
		name             string
		url              string
		securityProtocol string
		insecure         bool
		want             bool
	}{
		{name: "localhost", url: "http://localhost:8083", securityProtocol: "insecure", want: true},
		{name: "localhost in any case", url: "http://LocalHost:8083", securityProtocol: "insecure", want: true},
		{name: "IPv4 loopback", url: "http://127.0.0.1:8083", securityProtocol: "insecure", want: true},
		{name: "IPv4 loopback range", url: "http://127.8.9.10:8083", securityProtocol: "insecure", want: true},
		{name: "IPv6 loopback", url: "http://[::1]:8083", securityProtocol: "insecure", want: true},
		{name: "TLS to a remote host", url: "https://kagent.example", securityProtocol: "tls", want: true},
		{name: "plaintext to a remote host", url: "http://kagent.example:8083", securityProtocol: "insecure", want: false},
		{name: "plaintext to a host that only looks local", url: "http://localhost.example:8083", securityProtocol: "insecure", want: false},
		{name: "plaintext to a private address", url: "http://10.0.0.1:8083", securityProtocol: "insecure", want: false},
		{name: "plaintext with the opt-in", url: "http://kagent.kagent:8083", securityProtocol: "insecure", insecure: true, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := newGRPCTransport(tt.url)
			require.NoError(t, err)
			transport.insecureTokenTransport = tt.insecure
			require.Equal(t, tt.want, newTokenAuth(transport, tt.securityProtocol).sendToken)
		})
	}
}

func TestTokenIsNotSentInPlaintextToRemoteHost(t *testing.T) {
	server, dial := startTokenTestServer(t, false)

	for _, err := range callEveryKind(t, "http://kagent.example:8083", dial, WithTokenProvider(&staticTokenProvider{token: "user-token"})) {
		require.Error(t, err)
		require.Contains(t, err.Error(), "use an https:// URL")
		require.NotContains(t, err.Error(), "user-token")
	}
	require.Empty(t, server.observed(), "a refused call must not reach the server at all")
}

func TestNoTokenIsSentWithoutAuthorization(t *testing.T) {
	server, dial := startTokenTestServer(t, false)

	// No credential is at risk, so even a plaintext remote target gets the call.
	for _, err := range callEveryKind(t, "http://kagent.example:8083", dial, WithTokenProvider(&staticTokenProvider{})) {
		require.NoError(t, err)
	}
	require.Equal(t, [][]string{nil, nil, nil}, server.observed())
}

func TestInsecureTokenTransportSendsToRemoteHost(t *testing.T) {
	server, dial := startTokenTestServer(t, false)

	require.NoError(t, CheckHealth(context.Background(), "http://kagent.kagent:8083", dial,
		WithTokenProvider(&staticTokenProvider{token: "user-token"}), WithInsecureTokenTransport()))
	require.Equal(t, [][]string{{"Bearer user-token"}}, server.observed())
}

func TestTokenProviderErrorFailsTheCall(t *testing.T) {
	server, dial := startTokenTestServer(t, false)
	providerErr := errors.New("login expired")

	err := CheckHealth(context.Background(), "http://localhost:8083", dial, WithTokenProvider(&staticTokenProvider{err: providerErr}))

	require.ErrorIs(t, err, providerErr)
	// A plain error, not Unavailable, so a caller does not read it as an
	// unreachable server.
	require.NotEqual(t, codes.Unavailable, status.Code(err))
	require.Empty(t, server.observed())
}

func TestWithoutTokenSkipsTheProvider(t *testing.T) {
	server, dial := startTokenTestServer(t, false)
	provider := &staticTokenProvider{token: "user-token"}

	require.NoError(t, CheckHealth(WithoutToken(context.Background()), "http://localhost:8083", dial, WithTokenProvider(provider)))

	require.Zero(t, provider.calls)
	require.Equal(t, [][]string{nil}, server.observed())
}

func TestUnauthenticatedNamesTheNextStep(t *testing.T) {
	server, dial := startTokenTestServer(t, true)
	provider := &staticTokenProvider{hint: "sign in to get a credential"}

	for _, err := range callEveryKind(t, "http://localhost:8083", dial, WithTokenProvider(provider)) {
		require.Error(t, err)
		require.Contains(t, err.Error(), "the server requires authentication")
		require.Contains(t, err.Error(), "sign in to get a credential")
	}
	require.Len(t, server.observed(), 3, "a rejected call is never retried")
}

func TestUnauthenticatedKeepsItsCode(t *testing.T) {
	_, dial := startTokenTestServer(t, true)

	err := CheckHealth(context.Background(), "http://localhost:8083", dial, WithTokenProvider(&staticTokenProvider{}))

	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.ErrorContains(t, err, "the server requires authentication: invalid credentials")
}
