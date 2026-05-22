package handlers

import (
	"fmt"
	"net/http"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
)

type a2aWireVersion string

const (
	a2aWireV0 a2aWireVersion = "v0"
	a2aWireV1 a2aWireVersion = "v1"
)

// negotiatedA2AWireVersion returns the A2A wire version negotiated by the client.
// Uses the constants exposed by a2a-go so this function is reusable for all versions in the future
func negotiatedA2AWireVersion(r *http.Request) (a2aWireVersion, error) {
	version := r.Header.Get(a2a.SvcParamVersion)
	switch version {
	case "":
		return a2aWireV0, nil
	case string(a2a.Version):
		return a2aWireV1, nil
	default:
		return "", fmt.Errorf("unsupported A2A version %q", version)
	}
}
