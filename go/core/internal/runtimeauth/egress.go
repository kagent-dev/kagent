package runtimeauth

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/pkg/invocationauth"
)

type egressPolicyStore interface {
	SessionStore
	GetRuntimeEgressPolicy(context.Context, string) (egress.Access, error)
}

var _ egressPolicyStore = (*database.Client)(nil)
var _ http.Handler = (*EgressPolicyHandler)(nil)

// EgressPolicyHandler exposes live header bindings for the authenticated Actor.
// It holds no invocation credentials or per-request registry.
type EgressPolicyHandler struct{ store egressPolicyStore }

func NewEgressPolicyHandler(store egressPolicyStore) *EgressPolicyHandler {
	return &EgressPolicyHandler{store: store}
}

func (h *EgressPolicyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, err := Session(r.Context(), h.store)
	if err != nil {
		http.Error(w, "runtime authentication required", http.StatusForbidden)
		return
	}
	access, err := h.store.GetRuntimeEgressPolicy(r.Context(), session.GetPreparedRevision())
	if err != nil {
		http.Error(w, "invocation authentication configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	targets := make(map[string]*invocationauth.Target)
	get := func(origin string) *invocationauth.Target {
		if targets[origin] == nil {
			targets[origin] = &invocationauth.Target{Origin: origin, Headers: map[string]string{}}
		}
		return targets[origin]
	}
	for _, c := range access.Credentials {
		get(c.Origin).Headers[c.Header] = c.Prefix + "kagent-credential-injected"
	}
	for _, c := range access.TokenPassthroughTargets {
		t := get(c.Origin)
		t.Passthrough = append(t.Passthrough, &configv1.InvocationCredentialHeader{Name: c.Header, Prefix: c.Prefix})
	}
	result := make([]invocationauth.Target, 0, len(targets))
	for _, target := range targets {
		result = append(result, *target)
	}
	slices.SortFunc(result, func(a, b invocationauth.Target) int { return strings.Compare(a.Origin, b.Origin) })
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(result)
	if err != nil {
		http.Error(w, "invalid invocation authentication configuration", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}
