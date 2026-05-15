package openshell

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	inferencev1 "github.com/kagent-dev/kagent/go/api/openshell/gen/inferencev1"
	openshellv1 "github.com/kagent-dev/kagent/go/api/openshell/gen/openshellv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// OpenShellClients is the result of Dial: one gRPC connection plus the generated
// openshell.v1.OpenShell and inference.v1.Inference stubs. It does not interpret
// AgentHarness or apply per-call policy; use AgentHarnessOpenShellClient for that
// (see agentharness_openshell_client.go in this package).
type OpenShellClients struct {
	OpenShell openshellv1.OpenShellClient
	Inference inferencev1.InferenceClient
	Conn      *grpc.ClientConn
}

// Close closes the underlying connection.
func (c *OpenShellClients) Close() error {
	if c == nil || c.Conn == nil {
		return nil
	}
	return c.Conn.Close()
}

// Dial opens a single connection to cfg.GatewayURL and constructs clients for
// openshell.v1.OpenShell and openshell.inference.v1.Inference. Close OpenShellClients
// when the connection is no longer needed.
func Dial(ctx context.Context, cfg Config) (*OpenShellClients, error) {
	if cfg.GatewayURL == "" {
		return nil, fmt.Errorf("openshell: gateway URL is required")
	}

	var transportCreds credentials.TransportCredentials
	switch {
	case cfg.Insecure:
		transportCreds = insecure.NewCredentials()
	case len(cfg.TLSCAPEM) > 0:
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.TLSCAPEM) {
			return nil, fmt.Errorf("openshell: no PEM certificates found in TLS CA bundle")
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

	conn, err := grpc.NewClient(cfg.GatewayURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("openshell: dial %s: %w", cfg.GatewayURL, err)
	}
	// NewClient stays idle until Connect() or an RPC; otherwise waitConnReady times out.
	conn.Connect()
	if err := waitConnReady(dialCtx, conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("openshell: dial %s: %w", cfg.GatewayURL, err)
	}
	return &OpenShellClients{
		OpenShell: openshellv1.NewOpenShellClient(conn),
		Inference: inferencev1.NewInferenceClient(conn),
		Conn:      conn,
	}, nil
}

func waitConnReady(ctx context.Context, conn *grpc.ClientConn) error {
	for {
		switch s := conn.GetState(); s {
		case connectivity.Ready:
			return nil
		case connectivity.Shutdown:
			return fmt.Errorf("connection shut down")
		default:
			if !conn.WaitForStateChange(ctx, s) {
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("connection closed before ready")
			}
		}
	}
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
