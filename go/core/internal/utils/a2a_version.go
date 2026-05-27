package utils

import (
	"fmt"
	"net/http"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// NegotiateA2AWireVersion validates and returns the A2A wire version from the request.
// Requires A2A-Version: 1.0. Missing or unrecognized versions are rejected.
func NegotiateA2AWireVersion(r *http.Request) error {
	version := r.Header.Get(a2atype.SvcParamVersion)
	if version == string(a2atype.Version) {
		return nil
	}
	return fmt.Errorf("unsupported A2A version %q: this server requires A2A-Version: %s", version, a2atype.Version)
}
