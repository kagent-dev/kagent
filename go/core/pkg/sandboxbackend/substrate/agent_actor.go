package substrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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

	created := false
	wasRunning := false
	actor, err := b.client.GetActor(ctx, actorID)
	if err != nil {
		if status.Code(err) != codes.NotFound {
			return sandboxbackend.EnsureResult{}, fmt.Errorf("substrate GetActor %q: %w", actorID, err)
		}
		actor, err = b.client.CreateActor(ctx, actorID, tmplNS, tmplName)
		if err != nil {
			return sandboxbackend.EnsureResult{}, wrapCreateActorError(actorID, err)
		}
		created = true
	}

	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING:
		wasRunning = !created
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_UNSPECIFIED,
		ateapipb.Actor_STATUS_PAUSED, ateapipb.Actor_STATUS_PAUSING:
		// PAUSED/PAUSING keep a node-local snapshot; ResumeActor brings them back
		// the same as a suspended actor.
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
		Handle:     sandboxbackend.Handle{ID: actorID},
		Endpoint:   fmt.Sprintf("atenet-router Host %s", host),
		WasRunning: wasRunning,
	}, nil
}

// localSessionEventsPathFmt is the kagent-adk runtime route serving the session's events from
// the actor-local store, registered only when KAGENT_SESSION_DB_URL is set.
const localSessionEventsPathFmt = "/local/sessions/%s/events"

// ErrLocalSessionEventsUnsupported means the actor answered 404 for the local events route:
// the runtime image predates durable-dir sessions or KAGENT_SESSION_DB_URL is not set.
// Callers must surface this loudly (502), never as an empty event list.
var ErrLocalSessionEventsUnsupported = errors.New(
	"actor runtime does not serve local session events (image predates durable-dir sessions or KAGENT_SESSION_DB_URL is unset)")

// FetchLocalSessionEvents resumes the session actor if needed, reads the session's events from
// the runtime's local store through atenet-router, and suspends the actor again only when this
// read woke it — an actor that was already RUNNING is serving an in-flight chat and its
// lifecycle belongs to that chat's suspend-on-close. Returns the raw response body: a JSON
// array of rows in the controller event wire shape ({id, data, created_at}, ascending).
func (b *SandboxAgentActorBackend) FetchLocalSessionEvents(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID, userID string) ([]byte, error) {
	res, err := b.EnsureSessionActor(ctx, sa, sessionID)
	if err != nil {
		return nil, err
	}
	if !res.WasRunning {
		// This read woke the actor: put it back to sleep on the way out, success or failure.
		// A failed suspend must not fail the read; detach from ctx so a canceled request
		// cannot leak a running actor.
		defer func() {
			suspendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := b.SuspendSessionActor(suspendCtx, sa, sessionID); err != nil {
				ctrllog.FromContext(ctx).WithName("substrate-actor-backend").Error(err,
					"failed to suspend session actor after events read", "actorID", res.Handle.ID)
			}
		}()
	}

	target, host, err := GatewayRouterTarget(b.atenetRouterURL, res.Handle.ID)
	if err != nil {
		return nil, err
	}
	eventsURL := strings.TrimSuffix(target.String(), "/") + fmt.Sprintf(localSessionEventsPathFmt, url.PathEscape(sessionID))
	if userID != "" {
		eventsURL += "?user_id=" + url.QueryEscape(userID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build local events request: %w", err)
	}
	req.Host = host

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch local session events from actor %q: %w", res.Handle.ID, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read local session events from actor %q: %w", res.Handle.ID, err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("actor %q: %w", res.Handle.ID, ErrLocalSessionEventsUnsupported)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("actor %q local events endpoint returned %d: %.200s", res.Handle.ID, resp.StatusCode, body)
	}
	return body, nil
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

// DeleteSandboxAgentSessionActor deletes the actor for a single chat session. One session ⇔ one
// actor with a session-derived id, so a single deterministic delete covers the session's whole
// life regardless of how many shape rollouts it survived.
func (b *SandboxAgentActorBackend) DeleteSandboxAgentSessionActor(ctx context.Context, sa *v1alpha2.SandboxAgent, sessionID string) (bool, error) {
	if sa == nil {
		return true, nil
	}
	return b.DeleteSandboxAgentActor(ctx, SandboxAgentSessionActorID(sa, sessionID))
}

// sessionActorRef returns the session's stable actor id plus the agent's CURRENT template name.
// The template name is only used when the actor does not exist yet (first message): an existing
// actor is resumed under its BIRTH template — substrate stores the template name on the actor
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
