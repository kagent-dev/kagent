package utils

import (
	"fmt"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
)

type A2AWireVersion string

const (
	A2AWireVersionLegacy A2AWireVersion = "v0"
	A2AWireVersionV1     A2AWireVersion = "v1"
)

// NegotiateA2AWireVersion returns the A2A wire version requested by the client.
// Missing or explicit 0.3 headers use the legacy/current kagent A2A wire shape.
// TODO(0.11.0): Revisit missing-header behavior once legacy wire clients are unsupported.
func NegotiateA2AWireVersion(r *http.Request) (A2AWireVersion, error) {
	version := r.Header.Get(a2atype.SvcParamVersion)
	switch version {
	case "", string(a2av0.Version):
		return A2AWireVersionLegacy, nil
	case string(a2atype.Version):
		return A2AWireVersionV1, nil
	default:
		return "", fmt.Errorf("unsupported A2A version %q", version)
	}
}
