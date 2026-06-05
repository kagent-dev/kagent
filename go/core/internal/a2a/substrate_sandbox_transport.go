package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

// substrateSandboxSessionRoundTripper routes each A2A request to the session actor identified by contextId.
type substrateSandboxSessionRoundTripper struct {
	routerURL    string
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
	if base == nil {
		base = http.DefaultTransport
	}
	if sa == nil || actorBackend == nil {
		return nil, fmt.Errorf("substrate sandbox session transport requires SandboxAgent and actor backend")
	}
	return &substrateSandboxSessionRoundTripper{
		routerURL:    routerURL,
		sandboxAgent: sa,
		actorBackend: actorBackend,
		base:         base,
	}, nil
}

func (t *substrateSandboxSessionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil {
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

	// Proxy the A2A request through atenet-router to the session actor. The router
	// selects the actor by HTTP Host; actor ID comes from EnsureSessionActor above.
	actorRT, err := newSubstrateAgentRoundTripper(t.routerURL, res.Handle.ID, t.base)
	if err != nil {
		return nil, err
	}

	resp, err := actorRT.RoundTrip(req)
	if err != nil {
		t.scheduleSuspendSession(sessionID)
		return nil, err
	}

	// Suspend the actor after the client finishes reading the (streaming) response
	// so the worker can serve other sessions until this chat is resumed.
	resp.Body = &suspendSessionActorOnClose{
		ReadCloser: resp.Body,
		suspend: func() {
			t.scheduleSuspendSession(sessionID)
		},
	}
	return resp, nil
}

func (t *substrateSandboxSessionRoundTripper) scheduleSuspendSession(sessionID string) {
	if t == nil || t.actorBackend == nil || t.sandboxAgent == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = t.actorBackend.SuspendSessionActor(ctx, t.sandboxAgent, sessionID)
	}()
}

type suspendSessionActorOnClose struct {
	io.ReadCloser
	once    sync.Once
	suspend func()
}

func (b *suspendSessionActorOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.suspend)
	return err
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
