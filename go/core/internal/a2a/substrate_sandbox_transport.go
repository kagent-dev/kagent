package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

// substrateSandboxSessionRoundTripper routes each A2A request to the session actor identified by contextId.
type substrateSandboxSessionRoundTripper struct {
	routerURL    *url.URL
	sandboxAgent *v1alpha2.SandboxAgent
	actorBackend *substrate.SandboxAgentActorBackend
	base         http.RoundTripper
}

func newSubstrateSandboxSessionRoundTripper(
	routerURL string,
	sa *v1alpha2.SandboxAgent,
	actorBackend *substrate.SandboxAgentActorBackend,
	base http.RoundTripper,
) (http.RoundTripper, error) {
	routerURL = strings.TrimSpace(routerURL)
	if routerURL == "" {
		routerURL = substrate.DefaultAtenetRouterURL
	}
	u, err := url.Parse(routerURL)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	if sa == nil || actorBackend == nil {
		return nil, fmt.Errorf("substrate sandbox session transport requires SandboxAgent and actor backend")
	}
	return &substrateSandboxSessionRoundTripper{
		routerURL:    u,
		sandboxAgent: sa,
		actorBackend: actorBackend,
		base:         base,
	}, nil
}

func (t *substrateSandboxSessionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.routerURL == nil {
		return nil, http.ErrSkipAltProtocol
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read A2A request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))

	sessionID, err := extractA2AContextID(body)
	if err != nil {
		return nil, err
	}
	if sessionID == "" {
		return nil, fmt.Errorf("message contextId (session id) is required for substrate sandbox agents")
	}

	res, err := t.actorBackend.EnsureSessionActor(req.Context(), t.sandboxAgent, sessionID)
	if err != nil {
		return nil, err
	}

	req = req.Clone(req.Context())
	req.URL.Scheme = t.routerURL.Scheme
	req.URL.Host = t.routerURL.Host
	req.Host = substrate.ActorHost(res.Handle.ID, "")
	return t.base.RoundTrip(req)
}

func extractA2AContextID(body []byte) (string, error) {
	var payload struct {
		Params struct {
			Message struct {
				ContextID *string `json:"contextId"`
			} `json:"message"`
			ContextID *string `json:"contextId"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse A2A request body: %w", err)
	}
	if payload.Params.Message.ContextID != nil {
		return strings.TrimSpace(*payload.Params.Message.ContextID), nil
	}
	if payload.Params.ContextID != nil {
		return strings.TrimSpace(*payload.Params.ContextID), nil
	}
	return "", nil
}

// EnsureSessionActorOnCreate provisions the substrate actor when a chat session starts.
func EnsureSessionActorOnCreate(
	ctx context.Context,
	backend *substrate.SandboxAgentActorBackend,
	sa *v1alpha2.SandboxAgent,
	sessionID string,
) error {
	if backend == nil || sa == nil {
		return nil
	}
	_, err := backend.EnsureSessionActor(ctx, sa, sessionID)
	return err
}
