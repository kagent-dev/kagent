package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

// substrateSandboxSessionRoundTripper routes each A2A request to the session actor identified by contextId.
type substrateSandboxSessionRoundTripper struct {
	routerURL    string
	sandboxAgent *v1alpha3.SandboxAgent
	actorBackend *substrate.SandboxAgentActorBackend
	base         http.RoundTripper
	db           database.Client
}

func newSubstrateSandboxSessionRoundTripper(
	routerURL string,
	sa *v1alpha3.SandboxAgent,
	actorBackend *substrate.SandboxAgentActorBackend,
	base http.RoundTripper,
	db database.Client,
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
		db:           db,
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
	userID := strings.TrimSpace(req.Header.Get("X-User-Id"))
	if userID == "" {
		return nil, fmt.Errorf("request carries no user identity")
	}

	// Persist owner-scoped session metadata before creating or resuming an actor.
	// Failing closed keeps untracked sessions out of the shared sandbox runtime.
	if err := t.ensureSessionRow(req.Context(), sessionID, userID); err != nil {
		return nil, fmt.Errorf("ensure controller session row: %w", err)
	}

	res, err := t.actorBackend.EnsureSessionActor(req.Context(), t.sandboxAgent, userID, sessionID)
	if err != nil {
		return nil, err
	}

	// Proxy the A2A request through atenet-router to the session actor. The router
	// selects the actor by HTTP Host; actor ID comes from EnsureSessionActor above.
	actorRT, err := newSubstrateAgentRoundTripper(t.routerURL, res.Handle.Atespace, res.Handle.ID, t.base)
	if err != nil {
		return nil, err
	}

	resp, err := actorRT.RoundTrip(req)
	if err != nil {
		t.scheduleSuspendSession(userID, sessionID)
		return nil, err
	}

	// Suspend the actor after the client finishes reading the (streaming) response
	// so the worker can serve other sessions until this chat is resumed.
	resp.Body = &suspendSessionActorOnClose{
		ReadCloser: resp.Body,
		suspend: func() {
			t.scheduleSuspendSession(userID, sessionID)
		},
	}
	return resp, nil
}

// ensureSessionRow materializes the controller-side session row for sessions created by direct
// A2A calls that never went through POST /api/sessions (no UI involvement). Without the row the
// session is invisible to the sessions API: absent from listings, tasks unreadable, undeletable.
func (t *substrateSandboxSessionRoundTripper) ensureSessionRow(ctx context.Context, sessionID, userID string) error {
	if t.db == nil {
		return nil
	}
	if userID == "" {
		return fmt.Errorf("request carries no X-User-Id header; session row cannot be attributed to a user")
	}
	_, err := t.db.GetSession(ctx, sessionID, userID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("get session %q: %w", sessionID, err)
	}
	agentID := utils.ConvertToPythonIdentifier(t.sandboxAgent.Namespace + "/" + t.sandboxAgent.Name)
	if err := t.db.StoreSession(ctx, &database.Session{ID: sessionID, UserID: userID, AgentID: &agentID}); err != nil {
		return fmt.Errorf("store session row: %w", err)
	}
	return nil
}

func (t *substrateSandboxSessionRoundTripper) scheduleSuspendSession(userID, sessionID string) {
	if t == nil || t.actorBackend == nil || t.sandboxAgent == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = t.actorBackend.SuspendSessionActor(ctx, t.sandboxAgent, userID, sessionID)
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
