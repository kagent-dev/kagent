package openshell

import "time"

// Config configures the OpenShell gateway gRPC client.
type Config struct {
	// GatewayURL is a gRPC target (e.g. "dns:///gateway.openshell.svc:443"
	// or "localhost:7443"). Required.
	GatewayURL string

	// Token is a static bearer token sent as grpc metadata "authorization:
	// Bearer <token>". Optional.
	Token string

	// TLSCAPEM is a PEM-encoded CA bundle used to verify the gateway
	// certificate. If empty, system roots are used. If both TLSCAPEM is
	// empty and GatewayURL has no TLS scheme, the client dials insecurely
	// (intended for local/in-cluster plaintext only).
	TLSCAPEM []byte

	// Insecure, when true, dials without TLS regardless of other settings.
	// Use only for tests or explicit local development.
	Insecure bool

	// DialTimeout bounds the initial dial. Zero means no timeout.
	DialTimeout time.Duration

	// CallTimeout bounds each RPC. Zero means no per-call timeout.
	CallTimeout time.Duration
}
