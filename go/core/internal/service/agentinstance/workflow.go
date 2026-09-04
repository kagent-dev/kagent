package agentinstance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
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
	CreateActorFromSnapshotTag(context.Context, string, string, string, string, string, string) (*ateapipb.Actor, error)
	ResumeActor(context.Context, string, string) (*ateapipb.Actor, error)
	PauseActor(context.Context, string, string) (*ateapipb.Actor, error)
	SuspendActor(context.Context, string, string) (*ateapipb.Actor, error)
	GetActorSnapshot(context.Context, string, string) (*ateapipb.ActorSnapshot, error)
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

// PauseForInput checkpoints the runtime on its current worker, then commits the
// durable input-required transition. If the commit fails, it resumes the Actor
// so durable task state and runtime availability cannot disagree.
func (w *ActorWorkflow) PauseForInput(ctx context.Context, instance *apiv1alpha1.AgentInstance, commit func(context.Context) error) error {
	if commit == nil {
		return fmt.Errorf("input-required commit is required")
	}
	atespace, name := instance.GetNamespace(), actorName(instance.GetId())
	actor, err := w.actors.PauseActor(ctx, atespace, name)
	if err != nil {
		return fmt.Errorf("pause Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		return fmt.Errorf("pause Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	if err := commit(ctx); err != nil {
		commitErr := fmt.Errorf("persist input-required transition: %w", err)
		// The request context commonly carries the database failure or cancellation
		// that made the commit fail. Compensation must still get its own bounded
		// Substrate call through the client.
		actor, resumeErr := w.actors.ResumeActor(context.WithoutCancel(ctx), atespace, name)
		if resumeErr == nil && actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
			resumeErr = fmt.Errorf("resume Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
		}
		if resumeErr != nil {
			resumeErr = fmt.Errorf("resume Actor %s/%s after failed input-required transition: %w", atespace, name, resumeErr)
		}
		return errors.Join(commitErr, resumeErr)
	}
	return nil
}

// Quiesce durably suspends the runtime without changing the AgentInstance's
// logical READY state and returns the exact immutable snapshot it produced.
func (w *ActorWorkflow) Quiesce(ctx context.Context, instance *apiv1alpha1.AgentInstance) (*dbpkg.AgentInstanceTaskSnapshot, error) {
	atespace, name := instance.GetNamespace(), actorName(instance.GetId())
	actor, err := w.actors.SuspendActor(ctx, atespace, name)
	if err != nil {
		return nil, fmt.Errorf("suspend Actor %s/%s: %w", atespace, name, err)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("suspend Actor %s/%s returned status %s", atespace, name, actor.GetStatus().GetState())
	}
	ref := actor.GetStatus().GetLatestSnapshot()
	if ref.GetAtespace() == "" || ref.GetName() == "" {
		return nil, fmt.Errorf("suspend Actor %s/%s returned no snapshot", atespace, name)
	}
	snapshot, err := w.actors.GetActorSnapshot(ctx, ref.GetAtespace(), ref.GetName())
	if err != nil {
		return nil, fmt.Errorf("get ActorSnapshot %s/%s: %w", ref.GetAtespace(), ref.GetName(), err)
	}
	metadata := snapshot.GetMetadata()
	if metadata.GetAtespace() != ref.GetAtespace() || metadata.GetName() != ref.GetName() || metadata.GetUid() == "" {
		return nil, fmt.Errorf("ActorSnapshot %s/%s returned invalid identity", ref.GetAtespace(), ref.GetName())
	}
	scope := snapshot.GetStatus().GetContentScope()
	if scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL && scope != ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		return nil, fmt.Errorf("ActorSnapshot %s/%s returned invalid content scope %s", ref.GetAtespace(), ref.GetName(), scope)
	}
	return &dbpkg.AgentInstanceTaskSnapshot{
		Atespace: metadata.GetAtespace(), Name: metadata.GetName(), UID: metadata.GetUid(),
		ContentScope: strings.TrimPrefix(scope.String(), "SNAPSHOT_CONTENT_SCOPE_"),
	}, nil
}

// Create converges a persisted CREATING instance to READY. Retries discover
// the deterministically named Actor before creating it. An existing Actor is
// accepted only when it still references the instance's prepared template;
// this prevents an ID collision from attaching the instance to another
// workload.
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
		actor, err = w.actors.CreateActor(ctx, atespace, name, revision.ActorTemplateAtespace, revision.ActorTemplateName)
	}
	if err != nil {
		return nil, fmt.Errorf("ensure Actor %s/%s: %w", atespace, name, err)
	}
	if !usesActorTemplate(actor, revision) {
		return nil, fmt.Errorf("actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplate().GetAtespace(), actor.GetActorTemplate().GetName())
	}
	instance, err = w.store.MarkAgentInstanceReady(ctx, instance.GetId(), substrate.ActorHost(atespace, name, ""))
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance ready: %w", err)
	}
	return instance, nil
}

func (w *ActorWorkflow) Fork(ctx context.Context, instance *apiv1alpha1.AgentInstance, checkpoint *dbpkg.AgentInstanceCheckpoint) (*apiv1alpha1.AgentInstance, error) {
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
	atespace, name := instance.GetNamespace(), actorName(instance.GetId())
	if err := w.actors.EnsureAtespace(ctx, atespace); err != nil {
		return nil, fmt.Errorf("ensure Atespace %s: %w", atespace, err)
	}
	tag := &ateapipb.ObjectRef{Atespace: checkpoint.SnapshotAtespace, Name: "checkpoint-" + checkpoint.ID.String()}
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if status.Code(err) == codes.NotFound {
		actor, err = w.actors.CreateActorFromSnapshotTag(ctx, atespace, name,
			revision.ActorTemplateAtespace, revision.ActorTemplateName, tag.GetAtespace(), tag.GetName())
	}
	if err != nil {
		return nil, fmt.Errorf("ensure fork Actor %s/%s: %w", atespace, name, err)
	}
	if !usesActorTemplate(actor, revision) {
		return nil, fmt.Errorf("actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplate().GetAtespace(), actor.GetActorTemplate().GetName())
	}
	if !proto.Equal(actor.GetSourceSnapshotTag(), tag) {
		return nil, fmt.Errorf("actor %s/%s uses unexpected source snapshot tag", atespace, name)
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, fmt.Errorf("fork actor %s/%s is not suspended", atespace, name)
	}
	source := actor.GetStatus().GetSourceSnapshot()
	if source.GetSnapshot().GetAtespace() != checkpoint.SnapshotAtespace || source.GetSnapshot().GetName() != checkpoint.SnapshotName ||
		source.GetSnapshotUid() != checkpoint.SnapshotUID {
		return nil, fmt.Errorf("actor %s/%s uses unexpected source snapshot", atespace, name)
	}
	instance, err = w.store.MarkAgentInstanceReady(ctx, instance.GetId(), substrate.ActorHost(atespace, name, ""))
	if err != nil {
		return nil, fmt.Errorf("mark fork AgentInstance ready: %w", err)
	}
	return instance, nil
}

// Suspend completes synchronously: success means both Substrate and the
// AgentInstance record are suspended. Transitional Actor states are accepted
// because Substrate's imperative SuspendActor call joins or completes the
// in-flight operation. The workflow never recreates a missing Actor.
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
		switch actor.GetStatus().GetState() {
		case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
		case ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_RESUMING, ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
			_, err = w.actors.SuspendActor(ctx, instance.GetNamespace(), actorName(instance.GetId()))
		default:
			err = fmt.Errorf("actor %s/%s cannot be suspended from status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus().GetState())
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

// Resume completes synchronously: success means Substrate reports the Actor
// running and the AgentInstance record is ready. As with Suspend, retries may
// join a transitional Actor, but a missing Actor is an error rather than a
// request to recreate it.
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
		switch actor.GetStatus().GetState() {
		case ateapipb.ActorState_ACTOR_STATE_RUNNING:
		case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_SUSPENDING, ateapipb.ActorState_ACTOR_STATE_RESUMING:
			actor, err = w.actors.ResumeActor(ctx, instance.GetNamespace(), actorName(instance.GetId()))
			if err == nil && actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_RUNNING {
				err = fmt.Errorf("resume Actor %s/%s returned status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus().GetState())
			}
		default:
			err = fmt.Errorf("actor %s/%s cannot be resumed from status %s", instance.GetNamespace(), actorName(instance.GetId()), actor.GetStatus().GetState())
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

// lifecycleActor is intentionally lookup-only. Lifecycle operations may act
// only on the Actor created for this prepared revision; a missing Actor or a
// changed ActorTemplate indicates broken identity and must be surfaced rather
// than repaired by creating or adopting an Actor.
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
	if !usesActorTemplate(actor, revision) {
		return nil, fmt.Errorf("actor %s/%s uses unexpected ActorTemplate %s/%s", atespace, name, actor.GetActorTemplate().GetAtespace(), actor.GetActorTemplate().GetName())
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
	// still allowing the same mutation to finish after a lost response. The
	// returned bool reports whether this call installed the marker; a retry
	// which finds the same operation joins it but must not later clear it.
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
	// Completing the operation uses a second compare-and-set so only the owner
	// of the active operation can publish the new stable state. If another retry
	// already committed that state, its result makes this retry successful too.
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
	// retry must not release an operation that it merely joined. Clearing the
	// marker restores the last stable database state after the Substrate call
	// failed, allowing a later request to retry the workflow.
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

// Delete fences other lifecycle mutations with the same persisted operation
// marker used by Suspend and Resume. A missing Actor is treated as recovery
// from a previously completed Substrate deletion. Otherwise the Actor's
// template identity is checked and it is suspended before deletion, as
// required by Substrate's lifecycle contract.
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
	if !usesActorTemplate(actor, revision) {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("refuse to delete Actor %s/%s: ActorTemplate changed", atespace, name))
	}
	// Substrate's suspend and delete RPCs each run their workflows to
	// completion, so no local status polling is needed between them.
	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED, ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_DELETING:
	default:
		if _, err := w.actors.SuspendActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
			return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("suspend Actor %s/%s before deletion: %w", atespace, name, err))
		}
	}
	if err := w.actors.DeleteActor(ctx, atespace, name); err != nil && status.Code(err) != codes.NotFound {
		return nil, w.release(ctx, instance, originalState, claimed, fmt.Errorf("delete Actor %s/%s: %w", atespace, name, err))
	}
	return w.finishDelete(ctx, instance)
}

// finishDelete removes the durable AgentInstance row only after the Actor is
// gone. The returned message is detached from storage and scrubbed of runtime
// routing details so the synchronous Delete RPC can describe the completed
// operation without leaving a tombstone in the database.
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

func usesActorTemplate(actor *ateapipb.Actor, revision *dbpkg.RuntimeRevision) bool {
	ref := actor.GetActorTemplate()
	return ref.GetAtespace() == revision.ActorTemplateAtespace && ref.GetName() == revision.ActorTemplateName
}
