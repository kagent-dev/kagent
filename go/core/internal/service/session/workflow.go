package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/validation"
)

type workflowStore interface {
	ClaimSessionQuiescence(context.Context) (*database.SessionQuiescence, error)
	FinishSessionQuiescence(context.Context, *database.SessionQuiescence, *database.SessionTaskSnapshot) error
	GetSessionForRuntime(context.Context, string, string) (*apiv1alpha1.Session, error)
	GetSessionCheckpointSnapshot(context.Context, string, string) (*database.SessionTaskSnapshot, string, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	BeginSessionOperation(context.Context, string, apiv1alpha1.RuntimeOperation) (*database.SessionOperation, error)
	ClaimSessionOperation(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
	FinishSessionOperation(context.Context, string, uuid.UUID, uuid.UUID, string, string, string) (*apiv1alpha1.Session, error)
	GetSessionOperation(context.Context, string, uuid.UUID) (*database.SessionOperation, error)
}

type actorClient interface {
	EnsureActorEgressPolicy(context.Context, string, string, *ateapipb.EgressPolicy) error
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	CreateActor(context.Context, string, string, string, string) (*ateapipb.Actor, error)
	CreateActorFromTag(context.Context, string, string, string, string, string, string) (*ateapipb.Actor, error)
	ResumeActor(context.Context, string, string) (*ateapipb.Actor, error)
	PauseActor(context.Context, string, string) (*ateapipb.Actor, error)
	SuspendActor(context.Context, string, string) (*ateapipb.Actor, error)
	DeleteActor(context.Context, string, string) error
}

// ActorWorkflow runs the imperative Substrate operations behind Session
// lifecycle RPCs. Only the claiming caller issues lifecycle mutations; others
// observe current completion or receive a pending/superseded-operation error.
type ActorWorkflow struct {
	store  workflowStore
	actors actorClient
}

func NewActorWorkflow(store workflowStore, actors actorClient) *ActorWorkflow {
	return &ActorWorkflow{store: store, actors: actors}
}

// Pause checkpoints the runtime on its current worker without changing the
// Session logical state or persisting A2A task state.
func (w *ActorWorkflow) Pause(ctx context.Context, session *apiv1alpha1.Session) error {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	current, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return err
	}
	if err := w.verifyActor(ctx, session, revision, current); err != nil {
		return err
	}
	actor, err := w.actors.PauseActor(ctx, atespace, name)
	if err != nil {
		return fmt.Errorf("pause Actor %s/%s: %w", atespace, name, err)
	}
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != current.GetMetadata().GetUid() || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		return fmt.Errorf("pause Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	return nil
}

// Quiesce durably suspends the runtime without changing the Session's
// logical READY state and records its external snapshot URI. Only a checkpoint
// retains a copy after the Actor advances.
func (w *ActorWorkflow) Quiesce(ctx context.Context, session *apiv1alpha1.Session) (*database.SessionTaskSnapshot, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.GetId())
	current, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, err
	}
	if err := w.verifyActor(ctx, session, revision, current); err != nil {
		return nil, err
	}
	actor, err := w.actors.SuspendActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("suspend Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("suspend Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	metadata := actor.GetMetadata()
	if !validActorIdentity(actor, revision, name) || metadata.GetUid() != current.GetMetadata().GetUid() {
		return nil, fmt.Errorf("suspend actor %s/%s returned invalid identity", atespace, name)
	}
	snapshot := actor.GetStatus().GetExternalSnapshot()
	if snapshot.GetSnapshotUri() == "" {
		return nil, fmt.Errorf("suspend Actor %s/%s returned no snapshot", atespace, name)
	}
	scope := snapshot.GetContentScope()
	if scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL && scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		return nil, fmt.Errorf("actor %s/%s returned invalid snapshot content scope %s", atespace, name, scope)
	}
	return &database.SessionTaskSnapshot{
		Atespace: atespace, URI: snapshot.GetSnapshotUri(),
		ContentScope: strings.TrimPrefix(scope.String(), "SNAPSHOT_CONTENT_SCOPE_"),
	}, nil
}

// Create provisions the persisted session once, using its pinned checkpoint for
// forks. Retries return current state; an uncertain prior creation blocks execution.
func (w *ActorWorkflow) Create(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
}

// Suspend returns after the Actor and session are suspended. A retry observes
// the same operation rather than issuing a second mutation.
func (w *ActorWorkflow) Suspend(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
}

// Resume returns after the Actor is running and the session is ready. Missing
// Actors are errors; Resume never creates replacement compute.
func (w *ActorWorkflow) Resume(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
}

// Delete closes admission before stopping and deleting compute. It can supersede
// unissued creation, but never deletes a session while a prior call is uncertain.
func (w *ActorWorkflow) Delete(ctx context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return w.run(ctx, session.GetId(), apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
}

// run keeps lifecycle preparation separate from the durable issue boundary.
// Multiple callers may prepare using read-only calls; exactly one can authorize
// runtime mutations. Once authorized, every error retains the operation and its
// resource pins. The session lock serializes admission against runtime writes,
// claimed idle work, and checkpoint capture.
func (w *ActorWorkflow) run(ctx context.Context, sessionID string, requestedKind apiv1alpha1.RuntimeOperation) (*apiv1alpha1.Session, error) {
	operation, err := w.store.BeginSessionOperation(ctx, sessionID, requestedKind)
	if err != nil {
		return nil, err
	}
	if operation.Session.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE || operation.ExecutorID != uuid.Nil {
		return operationOutcome(operation)
	}
	kind := operation.Session.Operation
	session := operation.Session
	revision, err := w.store.GetRuntimeRevision(ctx, session.GetPreparedRevision())
	if err != nil {
		return w.failPreparation(ctx, operation, fmt.Errorf("load prepared revision: %w", err))
	}
	var snapshot *database.SessionTaskSnapshot
	var tagName string
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && operation.SourceCheckpointID != nil {
		snapshot, _, err = w.store.GetSessionCheckpointSnapshot(ctx, operation.SourceCheckpointID.String(), session.Creator)
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("load pinned checkpoint: %w", err))
		}
		if snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope != "DATA" {
			return w.failPreparation(ctx, operation, fmt.Errorf("fork requires a retained DATA checkpoint"))
		}
		tagName = "checkpoint-" + operation.SourceCheckpointID.String()
	}

	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.Id)
	var policy *ateapipb.EgressPolicy
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		policy, err = actorEgressPolicy(atespace, revision.EgressDestinations, revision.Credentials)
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("build Actor %s/%s egress policy: %w", atespace, name, err))
		}
	}
	actor, err := w.actors.GetActor(ctx, atespace, name)
	missing := status.Code(err) == codes.NotFound
	if err != nil && !missing {
		return w.failPreparation(ctx, operation, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err))
	}
	if missing && kind != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && kind != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		return w.failPreparation(ctx, operation, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err))
	}
	if !missing {
		if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
			return w.failPreparation(ctx, operation, fmt.Errorf("refuse to adopt existing Actor %s/%s while creation is still pending", atespace, name))
		}
		if err := w.verifyActor(ctx, session, revision, actor); err != nil {
			return w.failPreparation(ctx, operation, err)
		}
		if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME || kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND {
			switch actor.GetStatus().GetState() {
			case ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
				ateapipb.ActorState_ACTOR_STATE_PAUSED, ateapipb.ActorState_ACTOR_STATE_RESUMING,
				ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
			default:
				return w.failPreparation(ctx, operation, fmt.Errorf("actor %s/%s cannot perform %s from %s", atespace, name, kind, actor.GetStatus().GetState()))
			}
		}
	}

	executorID := uuid.New()
	claimed, err := w.store.ClaimSessionOperation(ctx, sessionID, operation.ID, executorID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		// A superseded generation returns a conflict without runtime work.
		current, err := w.store.GetSessionOperation(ctx, sessionID, operation.ID)
		if err != nil {
			return nil, err
		}
		return operationOutcome(current)
	}
	var authority string
	switch kind {
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE:
		// The pinned ActorTemplate already belongs to a provisioned Atespace.
		if snapshot == nil {
			actor, err = w.actors.CreateActor(ctx, atespace, name, revision.ActorTemplateAtespace, revision.ActorTemplateName)
		} else {
			actor, err = w.actors.CreateActorFromTag(ctx, atespace, name, revision.ActorTemplateAtespace, revision.ActorTemplateName, snapshot.Atespace, tagName)
		}
		if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED) {
			err = fmt.Errorf("created Actor %s/%s has unexpected identity or state", atespace, name)
		}
		if err == nil && snapshot != nil {
			source := actor.GetStatus().GetExternalSnapshot()
			if !proto.Equal(actor.GetSourceTag(), &ateapipb.ObjectRef{Atespace: snapshot.Atespace, Name: tagName}) ||
				source.GetSnapshotUri() != snapshot.URI || strings.TrimPrefix(source.GetContentScope().String(), "SNAPSHOT_CONTENT_SCOPE_") != snapshot.ContentScope {
				err = fmt.Errorf("fork Actor %s/%s does not match the retained checkpoint", atespace, name)
			}
		}
		if err == nil {
			err = w.actors.EnsureActorEgressPolicy(ctx, atespace, name, policy)
			if err != nil {
				err = fmt.Errorf("ensure Actor %s/%s egress policy: %w", atespace, name, err)
			}
		}
		authority = substrate.ActorHost(atespace, name, "")
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME:
		if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			actor, err = w.actors.ResumeActor(ctx, atespace, name)
		}
		if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING) {
			err = fmt.Errorf("resume Actor %s/%s returned unexpected identity or state", atespace, name)
		}
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND:
		if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
			actor, err = w.actors.SuspendActor(ctx, atespace, name)
		}
		if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED) {
			err = fmt.Errorf("suspend Actor %s/%s returned unexpected identity or state", atespace, name)
		}
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE:
		if !missing {
			switch actor.GetStatus().GetState() {
			case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_DELETING:
			default:
				actor, err = w.actors.SuspendActor(ctx, atespace, name)
				if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED) {
					err = fmt.Errorf("suspend Actor %s/%s before deletion returned unexpected identity or state", atespace, name)
				}
			}
			if err == nil {
				err = w.actors.DeleteActor(ctx, atespace, name)
				if status.Code(err) == codes.NotFound {
					err = nil
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("perform %s on Actor %s/%s; lifecycle operation %s remains pending: %w", kind, atespace, name, operation.ID, err)
	}
	// A disconnected client must not discard an already known runtime outcome.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.store.FinishSessionOperation(finishCtx, sessionID, operation.ID, executorID, authority, actor.GetMetadata().GetUid(), "")
}

// failPreparation releases only unissued work. If another caller won, observe
// the same generation instead. Completion returns current state; supersession
// returns a conflict. A local preparation error cannot clear a newer operation.
func (w *ActorWorkflow) failPreparation(ctx context.Context, admitted *database.SessionOperation, cause error) (*apiv1alpha1.Session, error) {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := w.store.FinishSessionOperation(finishCtx, admitted.Session.Id, admitted.ID, uuid.Nil, "", "", "lifecycle preparation failed")
	if errors.Is(err, database.ErrConflict) {
		operation, readErr := w.store.GetSessionOperation(finishCtx, admitted.Session.Id, admitted.ID)
		if readErr != nil {
			return nil, errors.Join(cause, err, readErr)
		}
		return operationOutcome(operation)
	}
	return nil, errors.Join(cause, err)
}

func operationOutcome(operation *database.SessionOperation) (*apiv1alpha1.Session, error) {
	if operation.Session.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return operation.Session, nil
	}
	return nil, fmt.Errorf("lifecycle operation %s is pending; runtime effects may be unresolved: %w", operation.ID, database.ErrConflict)
}

// Verify the recorded actor UID before issuing lifecycle work. Names and
// templates alone also match an externally replaced actor, which we must not
// adopt or checkpoint as this session's runtime.
func (w *ActorWorkflow) verifyActor(ctx context.Context, session *apiv1alpha1.Session, revision *database.RuntimeRevision, actor *ateapipb.Actor) error {
	if !validActorIdentity(actor, revision, substrate.ActorName(session.Id)) {
		return fmt.Errorf("runtime actor identity or template changed")
	}
	if _, err := w.store.GetSessionForRuntime(ctx, session.Id, actor.GetMetadata().GetUid()); err != nil {
		return fmt.Errorf("verify runtime actor UID: %w", err)
	}
	return nil
}

func validActorIdentity(actor *ateapipb.Actor, revision *database.RuntimeRevision, name string) bool {
	metadata, ref := actor.GetMetadata(), actor.GetActorTemplate()
	return metadata.GetName() == name && metadata.GetAtespace() == revision.ActorTemplateAtespace && metadata.GetUid() != "" &&
		ref.GetAtespace() == revision.ActorTemplateAtespace && ref.GetName() == revision.ActorTemplateName
}

// actorEgressPolicy compiles destinations into an actor's default allowlist.
// Credential bindings are already canonicalized by the store.
func actorEgressPolicy(atespace string, destinations []string, credentials []egress.Credential) (*ateapipb.EgressPolicy, error) {
	var hostnames, cidrs []string
	for _, destination := range destinations {
		if ip, err := netip.ParseAddr(destination); err == nil && ip.Zone() == "" {
			ip = ip.Unmap()
			cidrs = append(cidrs, netip.PrefixFrom(ip, ip.BitLen()).String())
			continue
		}
		hostname := strings.TrimSuffix(strings.ToLower(destination), ".")
		if len(validation.IsDNS1123Subdomain(hostname)) != 0 {
			return nil, fmt.Errorf("invalid egress destination %q", destination)
		}
		hostnames = append(hostnames, hostname)
	}
	policy := &ateapipb.EgressPolicy{Metadata: &ateapipb.ResourceMetadata{Atespace: atespace, Name: "default"}}
	for _, binding := range credentials {
		if !slices.Contains(hostnames, binding.Hostname) {
			return nil, fmt.Errorf("credential destination %q is not allowed", binding.Hostname)
		}
		var rule *ateapipb.HostnameRule
		if len(policy.Rules) > 0 {
			rule = policy.Rules[len(policy.Rules)-1].GetHostnames()
		}
		if rule == nil || rule.Patterns[0] != binding.Hostname {
			rule = &ateapipb.HostnameRule{Patterns: []string{binding.Hostname}, Effects: &ateapipb.EgressRuleEffects{}}
			policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Hostnames: rule})
		}
		rule.Effects.InjectStaticHeaders = append(rule.Effects.InjectStaticHeaders, &ateapipb.CredentialHeaderInjection{Header: binding.Header, Prefix: binding.Prefix, CredentialUri: binding.URI})
	}
	if len(hostnames) > 0 {
		slices.Sort(hostnames)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Hostnames: &ateapipb.HostnameRule{Patterns: slices.Compact(hostnames)}})
	}
	if len(cidrs) > 0 {
		slices.Sort(cidrs)
		policy.Rules = append(policy.Rules, &ateapipb.EgressRule{Cidrs: &ateapipb.CIDRRule{Cidrs: slices.Compact(cidrs)}})
	}
	if len(policy.Rules) > 256 {
		return nil, fmt.Errorf("egress policy exceeds 256 rules")
	}
	return policy, nil
}
