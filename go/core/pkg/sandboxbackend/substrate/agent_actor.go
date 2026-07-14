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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// EnsureSessionActor creates (or resumes) the per-session actor for a SandboxAgent chat and waits
// for it to be reachable. One session ⇔ one actor, for the session's entire life: the actor id is
// derived from the session alone, and an existing actor is always resumed — substrate rebuilds
// its workload spec from the actor's BIRTH template (the actor record stores the template name),
// so a session stays pinned to the shape it was created under across shape rollouts. Only when no
// actor exists yet is one created from the agent's current (newest Ready) ActorTemplate — during
// a shape rollout, new sessions keep landing on the previous Ready golden until the new one is
// Ready.
//
// If the WorkerPool has no free worker, CreateActor/ResumeActor surface ErrNoFreeWorkers and this
// returns it immediately (no buffering). On a single-replica pool the lone worker may be busy
// building a new golden, so a shape change can briefly make chat return "no free workers"; on a
// multi-replica pool the spare workers keep serving existing actors, so a rollout does not hit
// that error. Scaling the WorkerPool is the remedy for capacity pressure, not in-process retries.
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

	actorID, tmplName, err := b.sessionActorRef(ctx, sa, sessionID)
	if err != nil {
		return sandboxbackend.EnsureResult{}, err
	}
	tmplNS := sa.Namespace
	atespace := sa.Namespace

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
			return sandboxbackend.EnsureResult{}, wrapCreateActorError(actorID, err)
		}
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED,
		ateapipb.Actor_STATUS_PAUSED, ateapipb.Actor_STATUS_PAUSING:
		// PAUSED/PAUSING keep a node-local snapshot; ResumeActor brings them back
		// the same as a suspended actor. RUNNING/RESUMING actors need nothing.
		_, err = b.client.ResumeActor(ctx, atespace, actorID)
		if err != nil {
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

// SuspendSessionActor checkpoints and frees the worker for a chat session actor.
func (b *SandboxAgentActorBackend) SuspendSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) error {
	if sa == nil {
		return nil
	}
	actorID, _, err := b.sessionActorRef(ctx, sa, sessionID)
	if err != nil {
		return err
	}
	atespace := sa.Namespace
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

// DeleteSandboxAgentActor deletes a substrate actor by id.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentActor(ctx context.Context, atespace, actorID string) (bool, error) {
	if strings.TrimSpace(actorID) == "" {
		return true, nil
	}
	return deleteActor(ctx, b.client, atespace, actorID)
}

// DeleteSandboxAgentSessionActor deletes the actor for a single chat session. One session ⇔ one
// actor with a session-derived id, so a single deterministic delete covers the session's whole
// life regardless of how many shape rollouts it survived.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (bool, error) {
	if sa == nil {
		return true, nil
	}
	return b.DeleteSandboxAgentActor(ctx, sa.Namespace, SandboxAgentSessionActorID(sa, sessionID))
}

// sessionActorRef returns the session's stable actor id plus the agent's CURRENT template name.
// The template name is only used when the actor does not exist yet (first message): an existing
// actor is resumed under its original template — substrate stores the template name on the actor
// record and rebuilds the workload spec from it — which is what pins a session to the shape it
// was created under for its entire life.
func (b *SandboxAgentActorBackend) sessionActorRef(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (actorID, templateName string, err error) {
	tmpl, err := ResolveCurrentActorTemplate(ctx, b.kube, sa.Namespace, sa.Name)
	if err != nil {
		return "", "", err
	}
	if tmpl == nil {
		return "", "", fmt.Errorf("no ActorTemplate generated yet for SandboxAgent %s/%s", sa.Namespace, sa.Name)
	}
	return SandboxAgentSessionActorID(sa, sessionID), tmpl.Name, nil
}

// DeleteAllSandboxAgentActors deletes legacy per-agent actors and all session actors for a SandboxAgent.
func (b *SandboxAgentActorBackend) DeleteAllSandboxAgentActors(ctx context.Context, sa *v1alpha2.SandboxAgent) (bool, error) {
	if sa == nil {
		return true, nil
	}
	prefix := sandboxAgentActorPrefix(sa)

	// Build the set of ActorTemplates this agent owns (one per retained config hash). Session
	// actors are created FROM these templates, so matching an actor's source template reliably
	// identifies it even when its id falls back to the prefix-less asr-<hash> form (long agent
	// name / session id), which id-prefix matching alone would miss. This runs before template
	// cleanup in the delete path, so the templates are still present here.
	templates, err := listSandboxAgentActorTemplates(ctx, b.kube, sa.Namespace, sa.Name)
	if err != nil {
		return false, err
	}
	ownedTemplates := make(map[string]struct{}, len(templates))
	for _, t := range templates {
		ownedTemplates[t.Name] = struct{}{}
	}

	// Session actors live in the agent's namespace atespace, so the sweep scopes its scan there.
	actors, err := b.client.ListActors(ctx, sa.Namespace)
	if err != nil {
		return false, fmt.Errorf("list substrate actors: %w", err)
	}
	allDone := true
	for _, actor := range actors {
		id := strings.TrimSpace(actor.GetActorId())
		if id == "" {
			continue
		}
		if !actorBelongsToSandboxAgent(sa, actor, prefix, ownedTemplates) {
			continue
		}
		done, err := deleteActor(ctx, b.client, sa.Namespace, id)
		if err != nil {
			return false, fmt.Errorf("delete substrate actor %q: %w", id, err)
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
}

// actorBelongsToSandboxAgent reports whether an actor was created for this SandboxAgent. It matches
// on the actor's source ActorTemplate first (robust: survives the prefix-less asr-<hash> id
// fallback), then falls back to id-prefix matching as a backstop for orphaned actors whose
// template was already deleted.
func actorBelongsToSandboxAgent(sa *v1alpha2.SandboxAgent, actor *ateapipb.Actor, prefix string, ownedTemplates map[string]struct{}) bool {
	if actor.GetActorTemplateNamespace() == sa.Namespace {
		if _, ok := ownedTemplates[actor.GetActorTemplateName()]; ok {
			return true
		}
	}
	id := strings.TrimSpace(actor.GetActorId())
	return id == SandboxAgentActorID(sa) || strings.HasPrefix(id, prefix+"-")
}

func sandboxAgentActorPrefix(sa *v1alpha2.SandboxAgent) string {
	return SandboxAgentActorID(sa)
}

// SandboxAgentSessionActorID returns the ate-api actor id for a SandboxAgent chat session,
// derived from the session alone: one session ⇔ one actor for the session's entire life, across
// config AND shape rollouts (the actor's template binding lives on the actor record, not in the
// id). The id keeps the agent prefix (asr-<ns>-<name>-) so per-agent cleanup still matches.
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
