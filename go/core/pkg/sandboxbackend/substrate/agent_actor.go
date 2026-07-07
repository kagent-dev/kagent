package substrate

import (
	"context"
	"crypto/sha256"
	"errors"
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
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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
// for it to be reachable. It resolves the agent's current (newest Ready) ActorTemplate, so during
// a config change requests keep landing on the previous Ready golden until the new one is Ready.
//
// If the WorkerPool has no free worker, CreateActor/ResumeActor surface ErrNoFreeWorkers and this
// returns it immediately (no buffering). On a single-replica pool the lone worker may be busy
// building a new golden, so a config change can briefly make chat return "no free workers"; on a
// multi-replica pool the spare workers keep serving the current golden, so a rollout does not hit
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

	created := false
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
		created = true
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED,
		ateapipb.Actor_STATUS_PAUSED, ateapipb.Actor_STATUS_PAUSING:
		// PAUSED/PAUSING keep a node-local snapshot; ResumeActor brings them back
		// the same as a suspended actor.
		_, err = b.client.ResumeActor(ctx, atespace, actorID)
		if err != nil {
			return sandboxbackend.EnsureResult{}, wrapResumeActorError(actorID, err)
		}
	}

	if err := waitForActorReachableViaAtenet(ctx, b.client, nil, b.atenetRouterURL, atespace, actorID); err != nil {
		return sandboxbackend.EnsureResult{}, err
	}

	// when a new actor is created, all sessions will be resumed on the new actor
	// so we need to reap the orphaned session actors. There is currently a gap here
	// where new actors do not retain artifacts of the previous actor. Note that on a config rollout,
	// previous actor artifacts will be lost but we are aware of this gap and will be
	// actively addressing it. https://github.com/kagent-dev/kagent/issues/2111
	if created {
		b.scheduleReapOrphanedSessionActors(sa, actorID)
	}

	host := ActorHost(atespace, actorID, "")
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: actorID, Atespace: atespace},
		Endpoint: fmt.Sprintf("atenet-router Host %s", host),
	}, nil
}

// reapOrphanedActorsTimeout bounds the detached best-effort cleanup launched after a session actor
// is created. Deleting suspended orphans is a couple of ate-api round-trips per retained config
// hash; this leaves ample headroom while ensuring the goroutine cannot linger.
const reapOrphanedActorsTimeout = 30 * time.Second

// scheduleReapOrphanedSessionActors fires reapOrphanedSessionActors on a detached, time-bounded
// context so it never adds latency to (or fails) the chat request that triggered the new actor.
// Mirrors the transport's post-response suspend scheduling.
func (b *SandboxAgentActorBackend) scheduleReapOrphanedSessionActors(sa *v1alpha2.SandboxAgent, keepActorID string) {
	if sa == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reapOrphanedActorsTimeout)
		defer cancel()
		if err := b.reapOrphanedSessionActors(ctx, sa, keepActorID); err != nil {
			ctrllog.Log.WithName("substrate-actor-reaper").Error(err, "failed to reap orphaned session actors after config rollout",
				"sandboxagent", sa.Namespace+"/"+sa.Name)
		}
	}()
}

// reapOrphanedSessionActors deletes all agent's SUSPENDED session actors that were created from a
// superseded ActorTemplate
func (b *SandboxAgentActorBackend) reapOrphanedSessionActors(ctx context.Context, sa *v1alpha2.SandboxAgent, keepActorID string) error {
	if sa == nil {
		return nil
	}
	templates, err := listSandboxAgentActorTemplates(ctx, b.kube, sa.Namespace, sa.Name)
	if err != nil {
		return err
	}
	current := selectCurrentActorTemplate(templates)
	if current == nil {
		// No resolvable current template — can't tell orphans from live actors; reap nothing.
		return nil
	}
	ownedTemplates := make(map[string]struct{}, len(templates))
	for _, t := range templates {
		ownedTemplates[t.Name] = struct{}{}
	}

	actors, err := b.client.ListActors(ctx)
	if err != nil {
		return fmt.Errorf("list substrate actors: %w", err)
	}
	// Session actor ids start with "asr-"; substrate goldens use UUIDs, so this prefix excludes them.
	sessionPrefix := sandboxAgentIDPrefix + "-"
	agentPrefix := sandboxAgentActorPrefix(sa)
	var errs []error
	for _, actor := range actors {
		id := strings.TrimSpace(actor.GetActorId())
		if id == "" || id == keepActorID {
			continue
		}
		if !strings.HasPrefix(id, sessionPrefix) {
			continue // golden actor (UUID id) — never reap
		}
		if !actorBelongsToSandboxAgent(sa, actor, agentPrefix, ownedTemplates) {
			continue // another agent's session actor
		}
		if actor.GetActorTemplateNamespace() == sa.Namespace && actor.GetActorTemplateName() == current.Name {
			continue // under the current desired config — a live session, keep
		}
		if _, err := deleteActorIfSuspended(ctx, b.client, sa.Namespace, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

// DeleteSandboxAgentSessionActor deletes the actor(s) for a single chat session. Because the
// session actor id is keyed on the config hash and old templates/goldens are retained, a session
// can have actors under several hashes (one per config it was active under). Deleting only the
// current-hash actor would orphan the others, so this deletes the session's actor for every
// retained config hash.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (bool, error) {
	if sa == nil {
		return true, nil
	}
	hashes, err := b.retainedSessionConfigHashes(ctx, sa)
	if err != nil {
		return false, err
	}
	allDone := true
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		actorID := SandboxAgentSessionActorID(sa, hash, sessionID)
		if _, ok := seen[actorID]; ok {
			continue
		}
		seen[actorID] = struct{}{}
		done, err := b.DeleteSandboxAgentActor(ctx, sa.Namespace, actorID)
		if err != nil {
			return false, err
		}
		if !done {
			allDone = false
		}
	}
	return allDone, nil
}

// retainedSessionConfigHashes returns the distinct config-hash segments across the agent's
// retained ActorTemplates (plus "" for legacy/no-hash actors). These are the hashes a session's
// actor id could have been keyed on, mirroring sessionActorRef's per-template derivation.
func (b *SandboxAgentActorBackend) retainedSessionConfigHashes(ctx context.Context, sa *v1alpha2.SandboxAgent) ([]string, error) {
	templates, err := listSandboxAgentActorTemplates(ctx, b.kube, sa.Namespace, sa.Name)
	if err != nil {
		return nil, err
	}
	// Always include "" so a session actor created before any config hash existed is still cleaned.
	hashes := []string{""}
	seen := map[string]struct{}{"": {}}
	for _, t := range templates {
		hash := t.Annotations[consts.ConfigHashAnnotation]
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	return hashes, nil
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
