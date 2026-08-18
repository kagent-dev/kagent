package utils

import (
	"fmt"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

type A2AWireVersion string

const (
	A2AWireVersionV1 A2AWireVersion = "v1"
)

// NegotiateA2AWireVersion returns the A2A wire version requested by the client.
// Use only on A2A protocol endpoints (JSON-RPC mux). kagent REST APIs do not
// negotiate A2A-Version. Protocol clients must send A2A-Version: 1.0.
func NegotiateA2AWireVersion(r *http.Request) (A2AWireVersion, error) {
	version := r.Header.Get(a2atype.SvcParamVersion)
	switch version {
	case string(a2atype.Version):
		return A2AWireVersionV1, nil
	case "":
		return "", fmt.Errorf("missing required %s header; expected %q", a2atype.SvcParamVersion, a2atype.Version)
	default:
		return "", fmt.Errorf("unsupported %s %q; expected %q", a2atype.SvcParamVersion, version, a2atype.Version)
	}
}
