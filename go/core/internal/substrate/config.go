package substrate

import (
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// Config holds connection settings for Agent Substrate ate-api.
type Config struct {
	EgressPolicies egress.PolicyStore
	// EgressPolicyCompilers extend each actor's EgressPolicy in order.
	EgressPolicyCompilers []egresspolicy.Compiler
	// ControlPlaneURL is the runtime endpoint whose egress rule injects Actor JWTs.
	ControlPlaneURL string
	// AteAPIEndpoint is a gRPC target (e.g. dns:///api.ate-system.svc:443).
	AteAPIEndpoint string
	// CAFile contains the CA certificates used to verify ate-api.
	CAFile string
	// ClientCertFile contains the PEM client certificate and private key used for mTLS.
	ClientCertFile string
	DialTimeout    time.Duration
	CallTimeout    time.Duration
}
