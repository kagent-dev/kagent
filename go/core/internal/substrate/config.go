package substrate

import "time"

// Config holds connection settings for Agent Substrate ate-api.
type Config struct {
	// AteAPIEndpoint is a gRPC target (e.g. dns:///api.ate-system.svc:443).
	AteAPIEndpoint string
	// CAFile contains the CA certificates used to verify ate-api.
	CAFile string
	// ClientCertFile contains the PEM client certificate and private key used for mTLS.
	ClientCertFile string
	DialTimeout    time.Duration
	CallTimeout    time.Duration
}

// Router identifies the Atenet router for agent and sandbox guest traffic.
// For an https router, CAFile verifies the router and ClientCertFile carries the
// controller identity it requests for mTLS. An http router ignores both.
type Router struct {
	URL            string
	CAFile         string
	ClientCertFile string
}
