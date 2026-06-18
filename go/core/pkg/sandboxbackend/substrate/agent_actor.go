package substrate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ensureActorBufferTimeout caps how long a chat request waits for substrate worker capacity
	// before giving up. During a config rollout the new golden's build can occupy the worker(s),
	// so resuming/creating the session actor briefly returns "no free workers"; we buffer the
	// request rather than fail it. Bounded so a genuinely stuck/zero-capacity pool still errors.
	ensureActorBufferTimeout = 2 * time.Minute
	ensureActorRetryInitial  = 500 * time.Millisecond
	ensureActorRetryMax      = 4 * time.Second
)

// SandboxAgentActorBackend manages ate-api actors for SandboxAgent workloads.
type SandboxAgentActorBackend struct {
	client          *Client
	kube            client.Client
	atenetRouterURL string
}

// NewSandboxAgentActorBackend returns a backend that ensures SandboxAgent actors on ate-api.
// kube is used to resolve the agent's current (config-hashed) ActorTemplate.
func NewSandboxAgentActorBackend(client *Client, kube client.Client, atenetRouterURL string) *SandboxAgentActorBackend {
	atenetRouterURL = strings.TrimSpace(atenetRouterURL)
	if atenetRouterURL == "" {
		atenetRouterURL = DefaultAtenetRouterURL
	}
	return &SandboxAgentActorBackend{
		client:          client,
		kube:            kube,
		atenetRouterURL: atenetRouterURL,
	}
}

// EnsureSessionActor creates and resumes the per-session actor for a SandboxAgent chat.
//
// During a config rollout the new golden's build can occupy the WorkerPool (especially a
// single-replica pool), so the underlying ResumeActor/CreateActor briefly returns "no free
// workers". Rather than failing the chat, this buffers the request: it retries with backoff
// (re-resolving the current template each pass, so once the new golden is Ready the request lands
// on it with the new config) until a worker frees, the caller's context is cancelled, or the
// buffer timeout elapses. Other errors are returned immediately.
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

	bufferDeadline := time.Now().Add(ensureActorBufferTimeout)
	backoff := ensureActorRetryInitial
	for {
		res, err := b.ensureSessionActorOnce(ctx, sa, sessionID)
		if err == nil || !isNoFreeWorkersError(err) {
			return res, err
		}
		if time.Now().After(bufferDeadline) {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate worker capacity for %s/%s not available within %s: %w", sa.Namespace, sa.Name, ensureActorBufferTimeout, err)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return sandboxbackend.EnsureResult{}, fmt.Errorf("waiting for substrate worker capacity for %s/%s: %w", sa.Namespace, sa.Name, ctx.Err())
		case <-timer.C:
		}
		if backoff *= 2; backoff > ensureActorRetryMax {
			backoff = ensureActorRetryMax
		}
	}
}

// ensureSessionActorOnce performs a single create/resume/reachability attempt for the session's
// actor against the currently-resolved template.
func (b *SandboxAgentActorBackend) ensureSessionActorOnce(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (sandboxbackend.EnsureResult, error) {
	actorID, tmplName, err := b.sessionActorRef(ctx, sa, sessionID)
	if err != nil {
		return sandboxbackend.EnsureResult{}, err
	}
	tmplNS := sa.Namespace

	actor, err := b.client.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
		}
		actor, err = b.client.CreateActor(ctx, actorID, tmplNS, tmplName)
		if err != nil {
			return sandboxbackend.EnsureResult{}, wrapCreateActorError(actorID, err)
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
	actorID, _, err := b.sessionActorRef(ctx, sa, sessionID)
	if err != nil {
		return err
	}
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
	actorID, _, err := b.sessionActorRef(ctx, sa, sessionID)
	if err != nil {
		return false, err
	}
	return b.DeleteSandboxAgentActor(ctx, actorID)
}

// sessionActorRef resolves the agent's current (config-hashed) ActorTemplate and returns the
// session actor id keyed to it plus the template name to create the actor from. Keying the
// id on the config hash means a config change yields a new actor id, so the next message
// creates a fresh actor from the new golden instead of resuming the stale one.
func (b *SandboxAgentActorBackend) sessionActorRef(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (actorID, templateName string, err error) {
	tmpl, err := ResolveCurrentActorTemplate(ctx, b.kube, sa.Namespace, sa.Name)
	if err != nil {
		return "", "", err
	}
	if tmpl == nil {
		return "", "", fmt.Errorf("no ActorTemplate generated yet for SandboxAgent %s/%s", sa.Namespace, sa.Name)
	}
	hash := tmpl.Annotations[consts.ConfigHashAnnotation]
	return SandboxAgentSessionActorID(sa, hash, sessionID), tmpl.Name, nil
}

// ReapStaleSessionActors deletes this agent's per-session actors that were created from a
// superseded ActorTemplate (before a config change). It only deletes SUSPENDED actors: a RUNNING
// one may be mid-request, and kagent's transport suspends session actors after each request, so a
// stale actor converges to SUSPENDED on its own — force-suspending it could cut a live response.
// With config-hashed actor ids these actors are never addressed again, so this is storage hygiene,
// not correctness, and is best-effort. (Superseded GOLDEN actors are handled by
// Lifecycle.RetireSupersededTemplates, which removes them with their template once a newer
// template is Ready.) Returns true when no stale suspended session actors remain.
func (b *SandboxAgentActorBackend) ReapStaleSessionActors(ctx context.Context, sa *v1alpha2.SandboxAgent, activeTemplateName string) (bool, error) {
	if b == nil || b.client == nil || sa == nil {
		return true, nil
	}
	sessionPrefix := sandboxAgentActorPrefix(sa) + "-"
	actors, err := b.client.ListActors(ctx)
	if err != nil {
		return false, fmt.Errorf("list substrate actors: %w", err)
	}
	allDone := true
	for _, actor := range actors {
		id := strings.TrimSpace(actor.GetActorId())
		if id == "" || !strings.HasPrefix(id, sessionPrefix) {
			continue // not a session actor of this agent
		}
		if actor.GetActorTemplateName() == activeTemplateName {
			continue // belongs to the active template
		}
		done, err := deleteActorIfSuspended(ctx, b.client, id)
		if err != nil {
			return false, fmt.Errorf("delete stale session actor %q: %w", id, err)
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
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

// SandboxAgentSessionActorID returns the ate-api actor id for a SandboxAgent chat session at a
// given config hash. The hash segment ties the actor to a specific golden snapshot: a config
// change produces a new id, so the next message creates a fresh actor instead of resuming the
// stale one. The id keeps the agent prefix (asr-<ns>-<name>-) so per-agent cleanup still matches.
func SandboxAgentSessionActorID(sa *v1alpha2.SandboxAgent, configHash, sessionID string) string {
	hashSeg := ""
	if configHash != "" {
		hashSeg = configHash + "-"
	}
	raw := fmt.Sprintf("%s-%s%s", sandboxAgentActorPrefix(sa), hashSeg, sanitizeSessionID(sessionID))
	raw = strings.ToLower(strings.ReplaceAll(raw, "_", "-"))
	if len(raw) <= 63 && dns1123Label.MatchString(raw) {
		return raw
	}
	sum := sha256.Sum256([]byte(sa.Namespace + "/" + sa.Name + "/" + configHash + "/" + sessionID))
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
