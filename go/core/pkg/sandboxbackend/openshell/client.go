package openshell

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Dial returns an OpenShell gRPC client connected to cfg.GatewayURL. The
// returned conn must be closed on shutdown.
func Dial(ctx context.Context, cfg Config) (openshellv1.OpenShellClient, *grpc.ClientConn, error) {
	if cfg.GatewayURL == "" {
		return nil, nil, fmt.Errorf("openshell: gateway URL is required")
	}

	var transportCreds credentials.TransportCredentials
	switch {
	case cfg.Insecure:
		transportCreds = insecure.NewCredentials()
	case len(cfg.TLSCAPEM) > 0:
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.TLSCAPEM) {
			return nil, nil, fmt.Errorf("openshell: no PEM certificates found in TLS CA bundle")
		}
		transportCreds = credentials.NewTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
	default:
		transportCreds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	dialCtx := ctx
	if cfg.DialTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, cfg.DialTimeout)
		defer cancel()
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(transportCreds)}
	if cfg.Token != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearerToken{token: cfg.Token, requireTLS: !cfg.Insecure}))
	}

	conn, err := grpc.DialContext(dialCtx, cfg.GatewayURL, opts...) //nolint:staticcheck // grpc.NewClient is preferred but DialContext is simpler here and still supported
	if err != nil {
		return nil, nil, fmt.Errorf("openshell: dial %s: %w", cfg.GatewayURL, err)
	}
	return openshellv1.NewOpenShellClient(conn), conn, nil
}

type bearerToken struct {
	token      string
	requireTLS bool
}

func (b bearerToken) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearerToken) RequireTransportSecurity() bool { return b.requireTLS }

// withAuth attaches the bearer token to the outgoing context metadata. The
// per-RPC creds already do this on TLS connections; withAuth covers the
// insecure case where RequireTransportSecurity() == false is still respected.
func withAuth(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
