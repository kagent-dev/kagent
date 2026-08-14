package agentinstance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	legacysubstrate "github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workflowStore interface {
	GetRuntimeRevision(context.Context, string) (*dbpkg.RuntimeRevision, error)
	MarkAgentInstanceReady(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	TransitionAgentInstance(context.Context, *apiv1alpha1.AgentInstance, apiv1alpha1.AgentInstanceState, apiv1alpha1.AgentInstanceOperation) (*apiv1alpha1.AgentInstance, error)
	DeleteAgentInstance(context.Context, string) error
}

type actorClient interface {
	EnsureAtespace(context.Context, string) error
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	CreateActor(context.Context, string, string, string, string) (*ateapipb.Actor, error)
	ResumeActor(context.Context, string, string) (*ateapipb.Actor, error)
	SuspendActor(context.Context, string, string) error
	DeleteActor(context.Context, string, string) error
}

// ActorWorkflow runs the imperative Substrate operations behind AgentInstance
// lifecycle RPCs. It returns only when the requested operation finishes
// or the RPC context is canceled.
type ActorWorkflow struct {
	store  workflowStore
	actors actorClient
}

func NewActorWorkflow(store workflowStore, actors actorClient) *ActorWorkflow {
	return &ActorWorkflow{store: store, actors: actors}
}

func (w *ActorWorkflow) Create(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	if instance.GetState() == apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY {
		return instance, nil
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING {
		return nil, fmt.Errorf("AgentInstance %s is not creating", instance.GetId())
	}

	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace := instance.GetNamespace()
	name := actorName(instance.GetId())
	if err := w.actors.EnsureAtespace(ctx, atespace); err != nil {
		return nil, fmt.Errorf("ensure Atespace %s: %w", atespace, err)
	}

	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		actor, err = w.actors.CreateActor(ctx, atespace, name, revision.ActorTemplateNamespace, revision.ActorTemplateName)
	}
	if err != nil {
		return nil, fmt.Errorf("ensure Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetActorTemplateNamespace() != revision.ActorTemplateNamespace || actor.GetActorTemplateName() != revision.ActorTemplateName {
		return nil, fmt.Errorf("actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplateNamespace(), actor.GetActorTemplateName())
	}
	// Substrate's resume RPC is an imperative workflow and returns only after
	// the Actor is running.
	if actor.GetStatus() != ateapipb.Actor_STATUS_RUNNING {
		actor, err = w.actors.ResumeActor(ctx, atespace, name)
		if err != nil {
			return nil, fmt.Errorf("resume Actor %s/%s: %w", atespace, name, err)
		}
		if actor.GetStatus() != ateapipb.Actor_STATUS_RUNNING {
			return nil, fmt.Errorf("resume Actor %s/%s returned status %s", atespace, name, actor.GetStatus())
		}
	}

	instance, err = w.store.MarkAgentInstanceReady(ctx, instance.GetId(), legacysubstrate.ActorHost(atespace, name, ""))
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance ready: %w", err)
	}
	return instance, nil
}

func (w *ActorWorkflow) Suspend(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	instance, claimed, err := w.claim(ctx, instance,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND,
	)
	if err != nil {
		return nil, err
	}
	actor, err := w.lifecycleActor(ctx, instance)
	if err == nil {
		switch actor.GetStatus() {
		case ateapipb.Actor_STATUS_SUSPENDED:
		case ateapipb.Actor_STATUS_RUNNING, ateapipb.Actor_STATUS_RESUMING, ateapipb.Actor_STATUS_SUSPENDING:
			err = w.actors.SuspendActor(ctx, instance.GetNamespace(), actorName(instance.GetId()))
		default:
			err = fmt.Errorf("actor %s/%s cannot be suspended from status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus())
		}
	}
	if err != nil {
		return nil, w.release(ctx, instance, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, claimed, err)
	}
	return w.finish(ctx, instance,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED,
	)
}

func (w *ActorWorkflow) Resume(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	instance, claimed, err := w.claim(ctx, instance,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED,
		apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME,
	)
	if err != nil {
		return nil, err
	}
	actor, err := w.lifecycleActor(ctx, instance)
	if err == nil {
		switch actor.GetStatus() {
		case ateapipb.Actor_STATUS_RUNNING:
		case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_SUSPENDING, ateapipb.Actor_STATUS_RESUMING:
			actor, err = w.actors.ResumeActor(ctx, instance.GetNamespace(), actorName(instance.GetId()))
			if err == nil && actor.GetStatus() != ateapipb.Actor_STATUS_RUNNING {
				err = fmt.Errorf("resume Actor %s/%s returned status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus())
			}
		default:
			err = fmt.Errorf("actor %s/%s cannot be resumed from status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus())
		}
	}
	if err != nil {
		return nil, w.release(ctx, instance, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, claimed, err)
	}
	return w.finish(ctx, instance,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED,
		apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY,
	)
}

func (w *ActorWorkflow) lifecycleActor(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*ateapipb.Actor, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace, name := instance.GetNamespace(), actorName(instance.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("get Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetActorTemplateNamespace() != revision.ActorTemplateNamespace || actor.GetActorTemplateName() != revision.ActorTemplateName {
		return nil, fmt.Errorf("actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplateNamespace(), actor.GetActorTemplateName())
	}
	return actor, nil
}

func (w *ActorWorkflow) claim(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState apiv1alpha1.AgentInstanceState,
	operation apiv1alpha1.AgentInstanceOperation,
) (*apiv1alpha1.AgentInstance, bool, error) {
	// The operation is persisted with a compare-and-set before touching
	// Substrate. This lets every API replica reject a different mutation while
	// still allowing the same mutation to finish after a lost response.
	if instance.GetState() != expectedState {
		return nil, false, dbpkg.ErrAgentInstanceConflict
	}
	if instance.GetOperation() == operation {
		return instance, false, nil
	}
	if instance.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return nil, false, dbpkg.ErrAgentInstanceConflict
	}
	next := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	next.Operation = operation
	next.UpdatedAt = timestamppb.Now()
	claimed, err := w.store.TransitionAgentInstance(ctx, next, expectedState, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED)
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) && claimed.GetState() == expectedState && claimed.GetOperation() == operation {
		return claimed, false, nil
	}
	return claimed, err == nil, err
}

func (w *ActorWorkflow) finish(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState, nextState apiv1alpha1.AgentInstanceState,
) (*apiv1alpha1.AgentInstance, error) {
	// Completing the operation uses the same compare-and-set. If another retry
	// already committed the target state, that state is the successful result.
	next := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	next.State = nextState
	next.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	next.UpdatedAt = timestamppb.Now()
	current, err := w.store.TransitionAgentInstance(ctx, next, expectedState, instance.GetOperation())
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) && current.GetState() == nextState && current.GetOperation() == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return current, nil
	}
	return current, err
}

func (w *ActorWorkflow) release(ctx context.Context, instance *apiv1alpha1.AgentInstance, state apiv1alpha1.AgentInstanceState, claimed bool, operationErr error) error {
	// Only the request that installed the marker may clear it. A concurrent
	// retry must not release an operation that it merely joined.
	if !claimed {
		return operationErr
	}
	next := proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	next.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	next.UpdatedAt = timestamppb.Now()
	_, err := w.store.TransitionAgentInstance(ctx, next, state, instance.GetOperation())
	if errors.Is(err, dbpkg.ErrAgentInstanceConflict) {
		return operationErr
	}
	return errors.Join(operationErr, err)
}

func (w *ActorWorkflow) Delete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	originalState := instance.GetState()
	instance, claimed, err := w.claim(ctx, instance, originalState, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	if err != nil {
		return nil, err
	}
	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("load prepared revision: %w", err))
	}
	atespace := instance.GetNamespace()
	name := actorName(instance.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		return w.finishDelete(ctx, instance)
	}
	if err != nil {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("get Actor %s/%s for deletion: %w", atespace, name, err))
	}
	if actor.GetActorTemplateNamespace() != revision.ActorTemplateNamespace || actor.GetActorTemplateName() != revision.ActorTemplateName {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("refuse to delete Actor %s/%s: ActorTemplate changed", atespace, name))
	}
	// Substrate's suspend and delete RPCs each run their workflows to
	// completion, so no local status polling is needed between them.
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_CRASHED, ateapipb.Actor_STATUS_DELETING:
	default:
		if err := w.actors.SuspendActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
			return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("suspend Actor %s/%s before deletion: %w", atespace, name, err))
		}
	}
	if err := w.actors.DeleteActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("delete Actor %s/%s: %w", atespace, name, err))
	}
	return w.finishDelete(ctx, instance)
}

func (w *ActorWorkflow) finishDelete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	if err := w.store.DeleteAgentInstance(ctx, instance.GetId()); err != nil {
		return nil, fmt.Errorf("delete AgentInstance: %w", err)
	}
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	instance.PreparedRevision = ""
	instance.A2AAuthority = ""
	// TODO: Trigger runtime revision garbage collection outside the AgentInstance delete workflow.
	return instance, nil
}

func actorName(instanceID string) string { return "ai-" + strings.ToLower(instanceID) }
