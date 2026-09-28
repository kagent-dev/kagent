package substrate

import (
	"context"
	"fmt"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type LifecycleClient interface {
	GetActor(context.Context, string, string) (*ateapipb.Actor, error)
	CreateActor(context.Context, string, string, string, string) (*ateapipb.Actor, error)
	CreateActorFromTag(context.Context, string, string, string, string, string, string) (*ateapipb.Actor, error)
	EnsureActorEgressPolicy(context.Context, string, string, *ateapipb.EgressPolicy) error
	ResumeActor(context.Context, string, string) (*ateapipb.Actor, error)
	SuspendActor(context.Context, string, string) (*ateapipb.Actor, error)
	DeleteActor(context.Context, string, string) error
}

var _ LifecycleClient = (*Client)(nil)

type ActorBinding struct {
	Atespace         string
	Name             string
	TemplateAtespace string
	TemplateName     string
}

type ActorSnapshot struct {
	Tag          *ateapipb.ObjectRef
	URI          string
	ContentScope ateapipb.SnapshotContentScope
}

// ActorCreation describes runtime startup, independent of the owning API.
// Agent creation stays suspended; a standalone guest starts serving immediately.
type ActorCreation struct {
	EgressPolicy *ateapipb.EgressPolicy
	Snapshot     *ActorSnapshot
	Resume       bool
}

type ActorTransition struct {
	binding   ActorBinding
	operation apiv1alpha1.RuntimeOperation
	actor     *ateapipb.Actor
	creation  *ActorCreation
}

// ActorUID returns the observed runtime identity, including after creation.
func (t *ActorTransition) ActorUID() string {
	return t.actor.GetMetadata().GetUid()
}

// PrepareActorTransition performs read-only checks before the store's durable
// claim. It never adopts an existing Actor for an unissued creation.
func PrepareActorTransition(ctx context.Context, actors LifecycleClient, binding ActorBinding, operation apiv1alpha1.RuntimeOperation, creation *ActorCreation) (*ActorTransition, error) {
	return prepareActorTransition(ctx, actors, binding, operation, creation, false)
}

// PrepareActorRetry resumes previously issued intent. Substrate serializes
// lifecycle mutations with its Actor lease and resumes incomplete workflows.
// The immutable runtime name and pinned template identify an existing creation.
func PrepareActorRetry(ctx context.Context, actors LifecycleClient, binding ActorBinding, operation apiv1alpha1.RuntimeOperation, creation *ActorCreation) (*ActorTransition, error) {
	return prepareActorTransition(ctx, actors, binding, operation, creation, true)
}

func prepareActorTransition(ctx context.Context, actors LifecycleClient, binding ActorBinding, operation apiv1alpha1.RuntimeOperation, creation *ActorCreation, reconcile bool) (*ActorTransition, error) {
	if operation < apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE || operation > apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		return nil, fmt.Errorf("invalid actor lifecycle operation %s", operation)
	}
	if operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		if creation == nil || creation.EgressPolicy == nil {
			return nil, fmt.Errorf("actor creation requires an egress policy")
		}
		if snapshot := creation.Snapshot; snapshot != nil && (snapshot.Tag.GetAtespace() == "" || snapshot.Tag.GetName() == "" || snapshot.URI == "" || snapshot.ContentScope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA) {
			return nil, fmt.Errorf("actor restoration requires a retained DATA snapshot")
		}
	}
	actor, err := actors.GetActor(ctx, binding.Atespace, binding.Name)
	if status.Code(err) == codes.NotFound {
		if operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
			return nil, err
		}
		actor, err = nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get Actor %s/%s: %w", binding.Atespace, binding.Name, err)
	}
	if actor != nil {
		if operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE && !reconcile {
			return nil, fmt.Errorf("refuse to adopt existing Actor %s/%s while creation is still pending", binding.Atespace, binding.Name)
		}
		if !binding.matches(actor) {
			return nil, fmt.Errorf("actor %s/%s identity or template changed", binding.Atespace, binding.Name)
		}
		if operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME || operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND {
			switch actor.GetStatus().GetState() {
			case ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
				ateapipb.ActorState_ACTOR_STATE_PAUSED, ateapipb.ActorState_ACTOR_STATE_RESUMING, ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
			default:
				return nil, fmt.Errorf("actor cannot perform %s from %s", operation, actor.GetStatus().GetState())
			}
		}
	}
	return &ActorTransition{binding: binding, operation: operation, actor: actor, creation: creation}, nil
}

// ApplyActorTransition requires durable execution authority. Errors retain the
// claim's durable intent and pins. Another bounded attempt can retry that intent.
func ApplyActorTransition(ctx context.Context, actors LifecycleClient, transition *ActorTransition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binding, actor := transition.binding, transition.actor
	var err error
	switch transition.operation {
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE:
		creation := transition.creation
		if actor == nil {
			if creation.Snapshot == nil {
				actor, err = actors.CreateActor(ctx, binding.Atespace, binding.Name, binding.TemplateAtespace, binding.TemplateName)
			} else {
				tag := creation.Snapshot.Tag
				actor, err = actors.CreateActorFromTag(ctx, binding.Atespace, binding.Name, binding.TemplateAtespace, binding.TemplateName, tag.Atespace, tag.Name)
			}
			if err == nil {
				err = transition.checkResult(actor, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
				if err == nil {
					transition.actor = actor
				}
			}
		}
		if err == nil && creation.Snapshot != nil {
			snapshot := creation.Snapshot
			observed := actor.GetStatus().GetExternalSnapshot()
			if !proto.Equal(actor.GetSourceTag(), snapshot.Tag) || observed.GetSnapshotUri() != snapshot.URI || observed.GetContentScope() != snapshot.ContentScope {
				err = fmt.Errorf("restored Actor does not match the retained checkpoint")
			}
		}
		if err == nil {
			err = actors.EnsureActorEgressPolicy(ctx, binding.Atespace, binding.Name, creation.EgressPolicy)
		}
		if err == nil && creation.Resume && actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			actor, err = actors.ResumeActor(ctx, binding.Atespace, binding.Name)
			if err == nil {
				err = transition.checkResult(actor, ateapipb.ActorState_ACTOR_STATE_RUNNING)
			}
		}
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME:
		actor, err = actors.ResumeActor(ctx, binding.Atespace, binding.Name)
		if err == nil {
			err = transition.checkResult(actor, ateapipb.ActorState_ACTOR_STATE_RUNNING)
		}
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND:
		actor, err = actors.SuspendActor(ctx, binding.Atespace, binding.Name)
		if err == nil {
			err = transition.checkResult(actor, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
		}
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE:
		if actor != nil {
			switch actor.GetStatus().GetState() {
			case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_DELETING:
			default:
				actor, err = actors.SuspendActor(ctx, binding.Atespace, binding.Name)
				if err == nil {
					err = transition.checkResult(actor, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)
				}
			}
			if err == nil {
				err = actors.DeleteActor(ctx, binding.Atespace, binding.Name)
				if status.Code(err) == codes.NotFound {
					err = nil
				}
			}
		}
	}
	if err != nil {
		return fmt.Errorf("perform %s on Actor %s/%s: %w", transition.operation, binding.Atespace, binding.Name, err)
	}
	return nil
}

func (b ActorBinding) matches(actor *ateapipb.Actor) bool {
	return actor.GetMetadata().GetName() == b.Name && actor.GetMetadata().GetAtespace() == b.Atespace && actor.GetMetadata().GetUid() != "" &&
		actor.GetActorTemplate().GetAtespace() == b.TemplateAtespace && actor.GetActorTemplate().GetName() == b.TemplateName
}

func (t *ActorTransition) checkResult(actor *ateapipb.Actor, state ateapipb.ActorState) error {
	if !t.binding.matches(actor) || actor.GetStatus().GetState() != state || (t.actor != nil && actor.GetMetadata().GetUid() != t.actor.GetMetadata().GetUid()) {
		return fmt.Errorf("actor returned unexpected identity or state")
	}
	return nil
}
