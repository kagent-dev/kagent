package agentinstance

import (
	"context"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	legacysubstrate "github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workflowStore interface {
	GetRuntimeRevision(context.Context, string) (*dbpkg.RuntimeRevision, error)
	MarkAgentInstanceReady(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	MarkAgentInstanceDeleted(context.Context, string) (*apiv1alpha1.AgentInstance, error)
	RecordAgentInstanceActorUID(context.Context, string, string) (string, error)
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
// create and delete RPCs. It returns only when the requested operation finishes
// or the RPC context is canceled.
type ActorWorkflow struct {
	store   workflowStore
	actors  actorClient
	cleanup func(context.Context) error
}

func NewActorWorkflow(store workflowStore, actors actorClient, cleanup func(context.Context) error) *ActorWorkflow {
	return &ActorWorkflow{store: store, actors: actors, cleanup: cleanup}
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
		return nil, fmt.Errorf("Actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplateNamespace(), actor.GetActorTemplateName())
	}
	actorUID := actor.GetMetadata().GetUid()
	if actorUID == "" {
		return nil, fmt.Errorf("Actor %s/%s has no UID", atespace, name)
	}
	storedUID, err := w.store.RecordAgentInstanceActorUID(ctx, instance.GetId(), actorUID)
	if err != nil {
		return nil, err
	}
	if storedUID != actorUID {
		return nil, fmt.Errorf("Actor %s/%s UID changed", atespace, name)
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

func (w *ActorWorkflow) Delete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	if instance.GetState() == apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED {
		if err := w.cleanup(ctx); err != nil {
			return nil, fmt.Errorf("clean up runtime revisions: %w", err)
		}
		return instance, nil
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETING {
		return nil, fmt.Errorf("AgentInstance %s is not deleting", instance.GetId())
	}

	revision, err := w.store.GetRuntimeRevision(ctx, instance.GetPreparedRevision())
	if err != nil {
		return nil, fmt.Errorf("load prepared revision: %w", err)
	}
	atespace := instance.GetNamespace()
	name := actorName(instance.GetId())
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		return w.finishDelete(ctx, instance)
	}
	if err != nil {
		return nil, fmt.Errorf("get Actor %s/%s for deletion: %w", atespace, name, err)
	}
	if actor.GetActorTemplateNamespace() != revision.ActorTemplateNamespace || actor.GetActorTemplateName() != revision.ActorTemplateName {
		return nil, fmt.Errorf("refuse to delete Actor %s/%s: ActorTemplate changed", atespace, name)
	}
	actorUID := actor.GetMetadata().GetUid()
	storedUID, err := w.store.RecordAgentInstanceActorUID(ctx, instance.GetId(), actorUID)
	if err != nil {
		return nil, err
	}
	if actorUID == "" || storedUID != actorUID {
		return nil, fmt.Errorf("refuse to delete Actor %s/%s: UID changed", atespace, name)
	}

	// Substrate's suspend and delete RPCs each run their workflows to
	// completion, so no local status polling is needed between them.
	switch actor.GetStatus() {
	case ateapipb.Actor_STATUS_SUSPENDED, ateapipb.Actor_STATUS_CRASHED, ateapipb.Actor_STATUS_DELETING:
	default:
		if err := w.actors.SuspendActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
			return nil, fmt.Errorf("suspend Actor %s/%s before deletion: %w", atespace, name, err)
		}
	}
	if err := w.actors.DeleteActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
		return nil, fmt.Errorf("delete Actor %s/%s: %w", atespace, name, err)
	}
	return w.finishDelete(ctx, instance)
}

func (w *ActorWorkflow) finishDelete(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	instance, err := w.store.MarkAgentInstanceDeleted(ctx, instance.GetId())
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance deleted: %w", err)
	}
	if err := w.cleanup(ctx); err != nil {
		return nil, fmt.Errorf("clean up runtime revisions: %w", err)
	}
	return instance, nil
}

func actorName(instanceID string) string { return "ai-" + strings.ToLower(instanceID) }
