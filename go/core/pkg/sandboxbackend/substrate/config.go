package substrate

import "time"

// Config holds connection settings for Agent Substrate ate-api.
type Config struct {
	// AteAPIEndpoint is a gRPC target (e.g. dns:///api.ate-system.svc:443).
	AteAPIEndpoint string
	// TokenFile is a path to a file containing a bearer token for ate-api.
	TokenFile   string
	Insecure    bool
	DialTimeout time.Duration
	CallTimeout time.Duration
}
