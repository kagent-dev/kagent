package substrate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SandboxAgentActorBackend manages ate-api actors for SandboxAgent workloads.
type SandboxAgentActorBackend struct {
	client          *Client
	atenetRouterURL string
}

// NewSandboxAgentActorBackend returns a backend that ensures SandboxAgent actors on ate-api.
func NewSandboxAgentActorBackend(client *Client, atenetRouterURL string) *SandboxAgentActorBackend {
	atenetRouterURL = strings.TrimSpace(atenetRouterURL)
	if atenetRouterURL == "" {
		atenetRouterURL = DefaultAtenetRouterURL
	}
	return &SandboxAgentActorBackend{
		client:          client,
		atenetRouterURL: atenetRouterURL,
	}
}

// EnsureSessionActor creates and resumes the per-session actor for a SandboxAgent chat.
func (b *SandboxAgentActorBackend) EnsureSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (sandboxbackend.EnsureResult, error) {
	if sa == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("SandboxAgent is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("session id is required")
	}
	if b == nil || b.client == nil {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate ate-api client is required")
	}
	if v1alpha2.AgentSandboxPlatform(sa) != v1alpha2.SandboxPlatformSubstrate {
		return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate actor backend called for platform %q", v1alpha2.AgentSandboxPlatform(sa))
	}

	actorID := SandboxAgentSessionActorID(sa, sessionID)
	tmplNS, tmplName := sa.Namespace, SandboxAgentActorTemplateName(sa)

	actor, err := b.client.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
		}
		actor, err = b.client.CreateActor(ctx, actorID, tmplNS, tmplName)
		if err != nil {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate CreateActor %q: %w", actorID, err)
		}
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED:
		_, err = b.client.ResumeActor(ctx, actorID)
		if err != nil {
			return sandboxbackend.EnsureResult{}, wrapResumeActorError(actorID, err)
		}
	}

	if err := waitForActorReachableViaAtenet(ctx, b.client, nil, b.atenetRouterURL, actorID); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}

	host := ActorHost(actorID, "")
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: actorID},
		Endpoint: fmt.Sprintf("atenet-router Host %s", host),
	}, nil
}

// SuspendSessionActor checkpoints and frees the worker for a chat session actor.
func (b *SandboxAgentActorBackend) SuspendSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) error {
	if b == nil || b.client == nil || sa == nil {
		return nil
	}
	actorID := SandboxAgentSessionActorID(sa, sessionID)
	actor, err := b.client.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return fmt.Errorf("substrate GetActor %q: %w", actorID, err)
	}
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING, ateapipb.Actor_STATUS_SUSPENDING:
		if err := b.client.SuspendActor(ctx, actorID); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("substrate SuspendActor %q: %w", actorID, err)
		}
	}
	return nil
}

// DeleteSandboxAgentActor deletes a substrate actor by id.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentActor(ctx context.Context, actorID string) (bool, error) {
	if strings.TrimSpace(actorID) == "" {
		return true, nil
	}
	return deleteActor(ctx, b.client, actorID)
}

// DeleteSandboxAgentSessionActor deletes the actor for a single chat session.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (bool, error) {
	if sa == nil {
		return true, nil
	}
	return b.DeleteSandboxAgentActor(ctx, SandboxAgentSessionActorID(sa, sessionID))
}

// DeleteAllSandboxAgentActors deletes legacy per-agent actors and all session actors for a SandboxAgent.
func (b *SandboxAgentActorBackend) DeleteAllSandboxAgentActors(ctx context.Context, sa *v1alpha2.SandboxAgent) (bool, error) {
	if b == nil || b.client == nil || sa == nil {
		return true, nil
	}
	prefix := sandboxAgentActorPrefix(sa)
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
		if id != SandboxAgentActorID(sa) && !strings.HasPrefix(id, prefix+"-") {
			continue
		}
		done, err := deleteActor(ctx, b.client, id)
		if err != nil {
			return false, fmt.Errorf("delete substrate actor %q: %w", id, err)
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
}

func sandboxAgentActorPrefix(sa *v1alpha2.SandboxAgent) string {
	return SandboxAgentActorID(sa)
}

// SandboxAgentSessionActorID returns a stable ate-api actor id for a SandboxAgent chat session.
func SandboxAgentSessionActorID(sa *v1alpha2.SandboxAgent, sessionID string) string {
	raw := fmt.Sprintf("%s-%s", sandboxAgentActorPrefix(sa), sanitizeSessionID(sessionID))
	raw = strings.ToLower(strings.ReplaceAll(raw, "_", "-"))
	if len(raw) <= 63 && dns1123Label.MatchString(raw) {
		return raw
	}
	sum := sha256.Sum256([]byte(sa.Namespace + "/" + sa.Name + "/" + sessionID))
	return fmt.Sprintf("%s-%x", sandboxAgentIDPrefix, sum[:12])
}

func sanitizeSessionID(sessionID string) string {
	sessionID = strings.ToLower(strings.TrimSpace(sessionID))
	sessionID = strings.ReplaceAll(sessionID, "_", "-")
	return sessionID
}

// SandboxAgentActorID returns the legacy stable actor id prefix for a SandboxAgent.
func SandboxAgentActorID(sa *v1alpha2.SandboxAgent) string {
	raw := fmt.Sprintf("%s-%s-%s", sandboxAgentIDPrefix, sa.Namespace, sa.Name)
	raw = strings.ToLower(strings.ReplaceAll(raw, "_", "-"))
	if len(raw) > 63 {
		raw = strings.TrimRight(raw[:63], "-")
	}
	if !dns1123Label.MatchString(raw) {
		raw = fmt.Sprintf("%s-%s", sandboxAgentIDPrefix, sa.UID)
		if len(raw) > 63 {
			raw = raw[:63]
		}
	}
	return raw
}
