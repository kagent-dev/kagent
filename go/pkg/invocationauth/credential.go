// Package invocationauth resolves the current invocation's outbound credential.
package invocationauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// Source captures the invocation's caller token and policy endpoint without
// retaining its context. Its zero value has no credential authority.
type Source struct {
	policyURL string
	bearer    string
}

// FromContext snapshots credentials from private invocation metadata, never
// from durable Session state or the Actor's own control-plane identity.
func FromContext(ctx context.Context) (Source, error) {
	source := Source{}
	source.policyURL, _ = ctx.Value(policyURLKey{}).(string)
	call, ok := a2asrv.CallContextFrom(ctx)
	if !ok || call.ServiceParams() == nil {
		return source, nil
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

// ApplyRequest resolves live policy at the HTTP boundary, including bindings
// added by EgressPolicy rather than the integration's pinned configuration.
func ApplyRequest(ctx context.Context, request *http.Request, expected []*configv1.InvocationCredentialHeader) error {
	source, err := FromContext(ctx)
	if err != nil {
		return err
	}
	return source.ApplyRequest(ctx, request, expected)
}

// ApplyRequest applies this connection's credential source using the current
// request's cancellation and deadline, including for background SDK requests.
func (s Source) ApplyRequest(ctx context.Context, request *http.Request, expected []*configv1.InvocationCredentialHeader) error {
	if s.policyURL == "" {
		return s.apply(request.Header, expected)
	}
	targets, err := s.targets(ctx)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.Origin != egresspolicy.Origin(request.URL) {
			continue
		}
		for name, value := range target.Headers {
			request.Header.Set(name, value)
		}
		return s.apply(request.Header, target.Passthrough)
	}
	if len(expected) > 0 {
		return fmt.Errorf("invocation authentication binding unavailable")
	}
	return nil
}

// apply formats all configured headers from one acquisition. It never falls
// back to a static header when credential acquisition fails.
func (s Source) apply(headers http.Header, bindings []*configv1.InvocationCredentialHeader) error {
	if len(bindings) == 0 {
		return nil
	}
	if s.bearer == "" {
		return fmt.Errorf("invocation credential unavailable")
	}
	for _, binding := range bindings {
		headers.Set(binding.GetName(), binding.GetPrefix()+s.bearer)
	}
	return nil
}
