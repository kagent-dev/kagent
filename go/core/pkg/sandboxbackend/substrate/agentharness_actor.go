package substrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AgentHarnessSessionActorBackend manages the single shared ate-api actor for an
// AgentHarness. The AgentHarness is a template: its generated ActorTemplate is
// the golden snapshot, and one actor is spun from it per harness. Every chat is
// an ACP session inside that one actor's long-lived child process (created via
// session/new and resumed via session/load), so a Telegram/Slack gateway and
// its single getUpdates consumer also stay singular per harness.
type AgentHarnessSessionActorBackend struct {
	client          *Client
	atenetRouterURL string
}

// NewAgentHarnessSessionActorBackend returns a backend that ensures the shared
// AgentHarness actor on ate-api.
func NewAgentHarnessSessionActorBackend(client *Client, atenetRouterURL string) *AgentHarnessSessionActorBackend {
	atenetRouterURL = strings.TrimSpace(atenetRouterURL)
	if atenetRouterURL == "" {
		atenetRouterURL = DefaultAtenetRouterURL
	}
	return &AgentHarnessSessionActorBackend{
		client:          client,
		atenetRouterURL: atenetRouterURL,
	}
}

// EnsureSessionActor creates (if needed) and resumes the harness's single shared
// actor, then waits for it to be reachable via atenet-router. The sessionID
// identifies the chat for logging only; chats are multiplexed as ACP sessions
// inside the one actor, so they all resolve to the same ActorID(ah).
func (b *AgentHarnessSessionActorBackend) EnsureSessionActor(ctx context.Context, ah *v1alpha2.AgentHarness, sessionID string) (sandboxbackend.EnsureResult, error) {
	if ah == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("AgentHarness is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("session id is required")
	}
	if b == nil || b.client == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate ate-api client is required")
	}

	actorID := ActorID(ah)
	tmplNS, tmplName := generatedActorTemplateKey(ah)
	atespace := ah.Namespace

	actor, err := b.client.GetActor(ctx, atespace, actorID)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
		}
		if err := b.client.EnsureAtespace(ctx, atespace); err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate EnsureAtespace %q: %w", atespace, err)
		}
		actor, err = b.client.CreateActor(ctx, atespace, actorID, tmplNS, tmplName)
		if err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate CreateActor %q: %w", actorID, err)
		}
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		// already active or waking
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED,
		ateapipb.Actor_STATUS_PAUSED, ateapipb.Actor_STATUS_PAUSING:
		// PAUSED/PAUSING keep a node-local snapshot; ResumeActor brings them back
		// the same as a suspended actor.
		if _, err = b.client.ResumeActor(ctx, atespace, actorID); err != nil {
			return sandboxbackend.EnsureResult{}, wrapResumeActorError(actorID, err)
		}
	}

	if err := waitForActorReachableViaAtenet(ctx, b.client, nil, b.atenetRouterURL, atespace, actorID); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}

	host := ActorHost(atespace, actorID, "")
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: actorID, Atespace: atespace},
		Endpoint: fmt.Sprintf("atenet-router Host %s", host),
	}, nil
}

// SuspendSessionActor checkpoints and frees the worker for the harness's shared
// actor. It is resumed automatically on the next EnsureSessionActor. Because the
// actor is shared, suspending affects every chat in the harness.
func (b *AgentHarnessSessionActorBackend) SuspendSessionActor(ctx context.Context, ah *v1alpha2.AgentHarness, sessionID string) error {
	if b == nil || b.client == nil || ah == nil {
		return nil
	}
	actorID := ActorID(ah)
	atespace := ah.Namespace
	actor, err := b.client.GetActor(ctx, atespace, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("substrate GetActor %q: %w", actorID, err)
	}
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING, ateapipb.Actor_STATUS_SUSPENDING:
		if err := b.client.SuspendActor(ctx, atespace, actorID); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("substrate SuspendActor %q: %w", actorID, err)
		}
	}
	return nil
}

// DeleteSessionActor deletes a single per-session actor by id.
func (b *AgentHarnessSessionActorBackend) DeleteSessionActor(ctx context.Context, atespace, actorID string) (bool, error) {
	if strings.TrimSpace(actorID) == "" {
		return true, nil
	}
	return deleteActor(ctx, b.client, atespace, actorID)
}

// SessionActorState is the coarse lifecycle state of a chat session's actor as
// surfaced to the UI.
type SessionActorState string

const (
	// SessionActorStateRunning means the actor is running or waking up.
	SessionActorStateRunning SessionActorState = "running"
	// SessionActorStateSuspended means the actor is checkpointed/freed and will
	// resume on the next connect.
	SessionActorStateSuspended SessionActorState = "suspended"
	// SessionActorStateMissing means no actor exists yet for the session.
	SessionActorStateMissing SessionActorState = "missing"
)

// GetSessionActorState reports whether the harness's shared actor is running,
// suspended, or not yet created.
func (b *AgentHarnessSessionActorBackend) GetSessionActorState(ctx context.Context, ah *v1alpha2.AgentHarness, sessionID string) (SessionActorState, error) {
	if b == nil || b.client == nil || ah == nil {
		return SessionActorStateMissing, nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return SessionActorStateMissing, fmt.Errorf("session id is required")
	}
	actorID := ActorID(ah)
	atespace := ah.Namespace
	actor, err := b.client.GetActor(ctx, atespace, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return SessionActorStateMissing, nil
		}
		return SessionActorStateMissing, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
	}
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		return SessionActorStateRunning, nil
	default:
		return SessionActorStateSuspended, nil
	}
}

// DeleteAllAgentHarnessActors deletes the legacy single harness actor and every
// per-session actor belonging to the AgentHarness. It is best-effort and returns
// false while any actor is still terminating.
func (b *AgentHarnessSessionActorBackend) DeleteAllAgentHarnessActors(ctx context.Context, ah *v1alpha2.AgentHarness) (bool, error) {
	if b == nil || b.client == nil || ah == nil {
		return true, nil
	}
	prefix := agentHarnessActorPrefix(ah)
	actors, err := b.client.ListActors(ctx)
	if err != nil {
		return false, fmt.Errorf("list substrate actors: %w", err)
	}
	allDone := true
	for _, actor := range actors {
		id := strings.TrimSpace(actor.GetActorId())
		if id == "" {
			continue
		}
		if id != prefix && !strings.HasPrefix(id, prefix+"-") {
			continue
		}
		done, err := deleteActor(ctx, b.client, ah.Namespace, id)
		if err != nil {
			return false, fmt.Errorf("delete substrate actor %q: %w", id, err)
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
}

func agentHarnessActorPrefix(ah *v1alpha2.AgentHarness) string {
	return ActorID(ah)
}
