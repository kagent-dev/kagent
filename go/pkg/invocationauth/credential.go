// Package invocationauth resolves the current invocation's outbound credential.
package invocationauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// Source captures the invocation's caller token and header bindings without
// retaining its context. Its zero value has no credential authority.
type Source struct {
	targets []Target
	bearer  string
}

// FromContext snapshots credentials from private invocation metadata, never
// from durable Session state or the Actor's own control-plane identity.
func FromContext(ctx context.Context) (Source, error) {
	source := Source{}
	call, ok := a2asrv.CallContextFrom(ctx)
	if !ok || call.ServiceParams() == nil {
		return source, nil
	}
	if values, present := call.ServiceParams().Get(HeadersMetadataKey); present {
		if len(values) != 1 || json.Unmarshal([]byte(values[0]), &source.targets) != nil || source.targets == nil {
			return Source{}, fmt.Errorf("invalid invocation header bindings")
		}
	}
	values, ok := call.ServiceParams().Get("Authorization")
	if !ok {
		return source, nil
	}
	if len(values) != 1 {
		return Source{}, fmt.Errorf("one invocation Bearer credential is required")
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || token == "kagent-credential-injected" {
		return Source{}, fmt.Errorf("invalid invocation Bearer credential")
	}
	source.bearer = token
	return source, nil
}

// ApplyRequest uses the invocation's header snapshot, including EgressPolicy
// bindings resolved by the control plane before dispatch.
func ApplyRequest(ctx context.Context, request *http.Request) error {
	source, err := FromContext(ctx)
	if err != nil {
		return err
	}
	return source.ApplyRequest(request)
}

// ApplyRequest applies the captured credential and bindings, including for
// background SDK requests after the initiating call returns.
func (s Source) ApplyRequest(request *http.Request) error {
	for _, target := range s.targets {
		if target.Origin != egresspolicy.Origin(request.URL) {
			continue
		}
		for name, value := range target.Headers {
			request.Header.Set(name, value)
		}
		return s.apply(request.Header, target.Passthrough)
	}
	return nil
}

// apply formats all configured headers from one acquisition. It never falls
// back to a static header when credential acquisition fails.
func (s Source) apply(headers http.Header, bindings []Header) error {
	if len(bindings) == 0 {
		return nil
	}
	if s.bearer == "" {
		return fmt.Errorf("invocation credential unavailable")
	}
	for _, binding := range bindings {
		headers.Set(binding.Name, binding.Prefix+s.bearer)
	}
	return nil
}
