package auth

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// Provider translates between kagent's AuthzRequest/AuthzDecision and
// engine-specific wire formats (e.g. OPA's {"input":...}/{"result":...}).
type Provider interface {
	// Name returns the provider identifier (e.g. "opa").
	Name() string
	// MarshalRequest serializes an AuthzRequest into the engine's wire format.
	MarshalRequest(req auth.AuthzRequest) ([]byte, error)
	// UnmarshalDecision deserializes the engine's response into an AuthzDecision.
	UnmarshalDecision(data []byte) (*auth.AuthzDecision, error)
}

// ProviderByName returns a Provider for the given name.
// An empty name defaults to OPA.
func ProviderByName(name string) (Provider, error) {
	switch name {
	case "opa", "":
		return &OPAProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown authz provider: %q (supported: opa)", name)
	}
}
