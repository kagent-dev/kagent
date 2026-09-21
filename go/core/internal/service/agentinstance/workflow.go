package agentinstance

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
	GetAgentInstanceCheckpointSnapshot(context.Context, string, string) (*database.AgentInstanceTaskSnapshot, string, error)
	GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error)
	BeginAgentInstanceOperation(context.Context, string, apiv1alpha1.AgentInstanceOperation) (*database.InstanceOperation, error)
	ClaimAgentInstanceOperation(context.Context, string, uuid.UUID, uuid.UUID) (bool, error)
	FinishAgentInstanceOperation(context.Context, string, uuid.UUID, uuid.UUID, string, string) (*apiv1alpha1.AgentInstance, error)
	GetAgentInstanceOperation(context.Context, string, uuid.UUID) (*database.InstanceOperation, error)
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

// ActorWorkflow runs the imperative Substrate operations behind AgentInstance
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
// AgentInstance logical state or persisting A2A task state.
func (w *ActorWorkflow) Pause(ctx context.Context, instance *apiv1alpha1.AgentInstance) error {
	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(instance.GetId())
	actor, err := w.actors.PauseActor(ctx, atespace, name)
	if err != nil {
		return fmt.Errorf("pause Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		return fmt.Errorf("pause Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	return nil
}

// Quiesce durably suspends the runtime without changing the AgentInstance's
// logical READY state and records its external snapshot URI. Only a checkpoint
// retains a copy after the Actor advances.
func (w *ActorWorkflow) Quiesce(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*database.AgentInstanceTaskSnapshot, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(instance.GetId())
	actor, err := w.actors.SuspendActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("suspend Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("suspend Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	metadata := actor.GetMetadata()
	if metadata.GetAtespace() != atespace || metadata.GetName() != name || metadata.GetUid() == "" {
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
	return &database.AgentInstanceTaskSnapshot{
		Atespace: atespace, URI: snapshot.GetSnapshotUri(),
		ContentScope: strings.TrimPrefix(scope.String(), "SNAPSHOT_CONTENT_SCOPE_"),
	}, nil
}

// Create provisions the persisted instance once, using its pinned checkpoint for
// forks. Retries return current state; an uncertain prior creation blocks execution.
func (w *ActorWorkflow) Create(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return w.run(ctx, instance.GetId(), apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
}

// Suspend returns after the Actor and instance are suspended. A retry observes
// the same operation rather than issuing a second mutation.
func (w *ActorWorkflow) Suspend(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return w.run(ctx, instance.GetId(), apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
}

// Resume returns after the Actor is running and the instance is ready. Missing
// Actors are errors; Resume never creates replacement compute.
func (w *ActorWorkflow) Resume(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return w.run(ctx, instance.GetId(), apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME)
}

// Delete closes admission before stopping and deleting compute. It can supersede
// unissued creation, but never deletes an instance while a prior call is uncertain.
func (w *ActorWorkflow) Delete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return w.run(ctx, instance.GetId(), apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
}

// run keeps lifecycle preparation separate from the durable issue boundary.
// Multiple callers may prepare using read-only calls; exactly one can authorize
// runtime mutations. Once authorized, every error retains the operation and its
// resource pins. Pause/Quiesce and A2A execution join this boundary in the later
// shared-instance-execution change; this is lifecycle serialization only.
func (w *ActorWorkflow) run(ctx context.Context, instanceID string, requestedKind apiv1alpha1.AgentInstanceOperation) (*apiv1alpha1.AgentInstance, error) {
	operation, err := w.store.BeginAgentInstanceOperation(ctx, instanceID, requestedKind)
	if err != nil {
		return nil, err
	}
	if operation.Instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED || operation.ExecutorID != uuid.Nil {
		return operationOutcome(operation)
	}
	kind := operation.Instance.Operation
	instance := operation.Instance
	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return w.failPreparation(ctx, operation, fmt.Errorf("load prepared revision: %w", err))
	}
	var snapshot *database.AgentInstanceTaskSnapshot
	var tagName string
	if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE && operation.SourceCheckpointID != nil {
		snapshot, _, err = w.store.GetAgentInstanceCheckpointSnapshot(ctx, operation.SourceCheckpointID.String(), instance.Creator)
		if err != nil {
			return w.failPreparation(ctx, operation, fmt.Errorf("load pinned checkpoint: %w", err))
		}
		if snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope != "DATA" {
			return w.failPreparation(ctx, operation, fmt.Errorf("fork requires a retained DATA checkpoint"))
		}
		tagName = "checkpoint-" + operation.SourceCheckpointID.String()
	}

	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(instance.Id)
	var policy *ateapipb.EgressPolicy
	if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
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
	if missing && kind != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE && kind != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE {
		return w.failPreparation(ctx, operation, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err))
	}
	if !missing {
		if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
			return w.failPreparation(ctx, operation, fmt.Errorf("refuse to adopt existing Actor %s/%s while creation is still pending", atespace, name))
		}
		if !validActorIdentity(actor, revision, name) {
			return w.failPreparation(ctx, operation, fmt.Errorf("actor %s/%s identity or template changed", atespace, name))
		}
		if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME || kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND {
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
	claimed, err := w.store.ClaimAgentInstanceOperation(ctx, instanceID, operation.ID, executorID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		// A superseded generation returns a conflict without runtime work.
		current, err := w.store.GetAgentInstanceOperation(ctx, instanceID, operation.ID)
		if err != nil {
			return nil, err
		}
		return operationOutcome(current)
	}
	var authority string
	switch kind {
	case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE:
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
	case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME:
		if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			actor, err = w.actors.ResumeActor(ctx, atespace, name)
		}
		if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING) {
			err = fmt.Errorf("resume Actor %s/%s returned unexpected identity or state", atespace, name)
		}
	case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND:
		if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
			actor, err = w.actors.SuspendActor(ctx, atespace, name)
		}
		if err == nil && (!validActorIdentity(actor, revision, name) || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED) {
			err = fmt.Errorf("suspend Actor %s/%s returned unexpected identity or state", atespace, name)
		}
	case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE:
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
	return w.store.FinishAgentInstanceOperation(finishCtx, instanceID, operation.ID, executorID, authority, "")
}

// failPreparation releases only unissued work. If another caller won, observe
// the same generation instead. Completion returns current state; supersession
// returns a conflict. A local preparation error cannot clear a newer operation.
func (w *ActorWorkflow) failPreparation(ctx context.Context, admitted *database.InstanceOperation, cause error) (*apiv1alpha1.AgentInstance, error) {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := w.store.FinishAgentInstanceOperation(finishCtx, admitted.Instance.Id, admitted.ID, uuid.Nil, "", "lifecycle preparation failed")
	if errors.Is(err, database.ErrConflict) {
		operation, readErr := w.store.GetAgentInstanceOperation(finishCtx, admitted.Instance.Id, admitted.ID)
		if readErr != nil {
			return nil, errors.Join(cause, err, readErr)
		}
		return operationOutcome(operation)
	}
	return nil, errors.Join(cause, err)
}

func operationOutcome(operation *database.InstanceOperation) (*apiv1alpha1.AgentInstance, error) {
	if operation.Instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return operation.Instance, nil
	}
	return nil, fmt.Errorf("lifecycle operation %s is pending; runtime effects may be unresolved: %w", operation.ID, database.ErrConflict)
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
