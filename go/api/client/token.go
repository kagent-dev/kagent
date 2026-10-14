package client

import (
	"context"
	"net"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenProvider returns the bearer token for one call. The client asks once
// for each unary call and once when each stream starts, so a provider that
// refreshes an expiring token keeps a long-lived client working. An empty
// token sends the call without authorization and leaves the decision to the
// server; an error fails the call without sending it.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// UnauthenticatedHinter is an optional TokenProvider extension. Its hint is
// appended to the error for a call the server rejects as Unauthenticated, so
// the user learns how to get a credential rather than only that one is missing.
type UnauthenticatedHinter interface {
	UnauthenticatedHint() string
}

// WithTokenProvider sends the provider's token as `authorization: Bearer` on
// every call, A2A calls and streams included. The token goes only over TLS or
// to a loopback host; a plaintext call to any other host fails before it is
// sent, because a bearer token on that connection is readable by anyone on the
// path. WithInsecureTokenTransport lifts that rule.
//
// This is a dial interceptor rather than request metadata: the a2a-go gRPC
// transport replaces the outgoing metadata on each call, and an interceptor
// runs after it does. It is not a PerRPCCredentials either: gRPC turns a
// credential error into Unavailable, which reads as an unreachable server and
// sends the CLI into its port-forward fallback.
func WithTokenProvider(provider TokenProvider) ClientOption {
	return func(client *baseClient) {
		client.transport.tokenProvider = provider
	}
}

// WithInsecureTokenTransport allows WithTokenProvider to send its token to a
// non-loopback host without TLS, such as a Service address from inside the
// cluster.
func WithInsecureTokenTransport() ClientOption {
	return func(client *baseClient) {
		client.transport.insecureTokenTransport = true
	}
}

// IsLoopbackHost reports whether host, without a port, names this machine:
// `localhost`, an address in 127.0.0.0/8, or ::1. A kubectl port-forward
// listens there and carries the traffic to the cluster over the API server's
// TLS connection.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type connContextKey struct{}

type withoutTokenKey struct{}

// ConnFromContext returns, inside TokenProvider.Token, the connection of the
// call that asked for the token. A provider that must ask the same server
// something first, such as which issuer it trusts, calls it over this
// connection, which already rides any port-forward the call does.
func ConnFromContext(ctx context.Context) (grpc.ClientConnInterface, bool) {
	cc, ok := ctx.Value(connContextKey{}).(grpc.ClientConnInterface)
	return cc, ok
}

// WithoutToken marks calls made with ctx to skip the TokenProvider and go
// without authorization. A provider uses it for the calls it makes from inside
// Token, which would otherwise ask it for a token again and never return.
func WithoutToken(ctx context.Context) context.Context {
	return context.WithValue(ctx, withoutTokenKey{}, true)
}

type tokenAuth struct {
	provider  TokenProvider
	target    string
	sendToken bool
}

func newTokenAuth(transport *grpcTransport, securityProtocol string) *tokenAuth {
	// The URL was validated when the transport was built; the dial target may
	// be a test override, so the host comes from the URL the user gave.
	u, _ := url.Parse(transport.url)
	return &tokenAuth{
		provider:  transport.tokenProvider,
		target:    transport.url,
		sendToken: securityProtocol == "tls" || IsLoopbackHost(u.Hostname()) || transport.insecureTokenTransport,
	}
}

func (a *tokenAuth) dialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(a.unary),
		grpc.WithChainStreamInterceptor(a.stream),
	}
}

func (a *tokenAuth) authorize(ctx context.Context, cc *grpc.ClientConn) (context.Context, error) {
	if skip, _ := ctx.Value(withoutTokenKey{}).(bool); skip {
		return ctx, nil
	}
	token, err := a.provider.Token(context.WithValue(ctx, connContextKey{}, grpc.ClientConnInterface(cc)))
	if err != nil || token == "" {
		return ctx, err
	}
	if !a.sendToken {
		return ctx, status.Errorf(codes.FailedPrecondition,
			"refusing to send a credential to %s without TLS: use an https:// URL, or reach the server through a port-forward to localhost", a.target)
	}
	// Set, not append: the server must see exactly one credential.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(ctx, md), nil
}

// rejected rewrites an Unauthenticated status from the server. It keeps the
// code, so callers that branch on it still work, and never retries the call
// without the token.
func (a *tokenAuth) rejected(err error) error {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		return err
	}
	message := "the server requires authentication: " + st.Message()
	if hinter, ok := a.provider.(UnauthenticatedHinter); ok {
		if hint := hinter.UnauthenticatedHint(); hint != "" {
			message += "; " + hint
		}
	}
	return status.Error(codes.Unauthenticated, message)
}

func (a *tokenAuth) unary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := a.authorize(ctx, cc)
	if err != nil {
		return err
	}
	return a.rejected(invoker(ctx, method, req, reply, cc, opts...))
}

func (a *tokenAuth) stream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := a.authorize(ctx, cc)
	if err != nil {
		return nil, err
	}
	stream, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, a.rejected(err)
	}
	return &tokenAuthStream{ClientStream: stream, auth: a}, nil
}

// tokenAuthStream rewrites the rejection on a stream, which a server reports
// on the first receive rather than when the stream opens.
type tokenAuthStream struct {
	grpc.ClientStream
	auth *tokenAuth
}

func (s *tokenAuthStream) RecvMsg(m any) error {
	return s.auth.rejected(s.ClientStream.RecvMsg(m))
}

func (s *tokenAuthStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	return md, s.auth.rejected(err)
}
