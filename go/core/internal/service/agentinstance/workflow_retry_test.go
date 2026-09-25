package agentinstance

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type retryTestActors struct {
	*lifecycleTestActors
	beforeRead  func(context.Context)
	afterRead   func(context.Context)
	readErr     error
	mutationErr error
	mutations   atomic.Int32
}

func (a *retryTestActors) GetActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	if a.beforeRead != nil {
		a.beforeRead(ctx)
	}
	if a.readErr != nil {
		return nil, a.readErr
	}
	actor, err := a.lifecycleTestActors.GetActor(ctx, space, name)
	if a.afterRead != nil {
		a.afterRead(ctx)
	}
	return actor, err
}

func (a *retryTestActors) CreateActor(ctx context.Context, space, name, templateSpace, templateName string) (*ateapipb.Actor, error) {
	a.mutations.Add(1)
	actor, err := a.lifecycleTestActors.CreateActor(ctx, space, name, templateSpace, templateName)
	if a.mutationErr != nil {
		return nil, a.mutationErr
	}
	return actor, err
}

func (a *retryTestActors) CreateActorFromTag(ctx context.Context, space, name, templateSpace, templateName, tagSpace, tagName string) (*ateapipb.Actor, error) {
	a.mutations.Add(1)
	actor, err := a.lifecycleTestActors.CreateActorFromTag(ctx, space, name, templateSpace, templateName, tagSpace, tagName)
	if a.mutationErr != nil {
		return nil, a.mutationErr
	}
	return actor, err
}

func (a *retryTestActors) ResumeActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	a.mutations.Add(1)
	actor, err := a.lifecycleTestActors.ResumeActor(ctx, space, name)
	if a.mutationErr != nil {
		return nil, a.mutationErr
	}
	return actor, err
}

func (a *retryTestActors) SuspendActor(ctx context.Context, space, name string) (*ateapipb.Actor, error) {
	a.mutations.Add(1)
	actor, err := a.lifecycleTestActors.SuspendActor(ctx, space, name)
	if a.mutationErr != nil {
		return nil, a.mutationErr
	}
	return actor, err
}

func (a *retryTestActors) DeleteActor(ctx context.Context, space, name string) error {
	a.mutations.Add(1)
	if err := a.lifecycleTestActors.DeleteActor(ctx, space, name); err != nil {
		return err
	}
	return a.mutationErr
}

func TestLifecycleRetainsAmbiguousMutation(t *testing.T) {
	for _, name := range []string{"create", "fork", "resume", "suspend", "delete"} {
		t.Run(name, func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base)
			var err error
			if name != "create" && name != "fork" {
				instance, err = setup.Create(t.Context(), instance)
				require.NoError(t, err)
				switch name {
				case "resume":
					instance, err = setup.Suspend(t.Context(), instance)
					require.NoError(t, err)
				case "suspend":
					_, err = base.ResumeActor(t.Context(), "team-a", substrate.ActorName(instance.Id))
					require.NoError(t, err)
				}
			}
			var checkpointID string
			if name == "fork" {
				instance, checkpointID = lifecycleForkFixture(t, store, base, instance)
			}
			actors := &retryTestActors{lifecycleTestActors: base, mutationErr: status.Error(codes.Unavailable, "response lost after effect")}
			workflow := NewActorWorkflow(store, actors)
			call := workflow.Create
			switch name {
			case "resume":
				call = workflow.Resume
			case "suspend":
				call = workflow.Suspend
			case "delete":
				call = workflow.Delete
			}
			_, err = call(t.Context(), instance)
			require.Equal(t, codes.Unavailable, status.Code(err))
			current, err := store.GetAgentInstanceByID(t.Context(), instance.Id)
			require.NoError(t, err, "uncertainty must preserve the instance and its resource pins")
			require.NotEqual(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED, current.Operation)
			_, err = call(t.Context(), current)
			require.ErrorIs(t, err, database.ErrConflict)
			_, err = setup.Delete(t.Context(), current)
			require.ErrorIs(t, err, database.ErrConflict, "Delete must not erase uncertain work")
			if checkpointID != "" {
				_, _, err = store.BeginDeleteAgentInstanceCheckpoint(t.Context(), checkpointID, instance.Creator)
				require.ErrorIs(t, err, database.ErrNotFound, "uncertain fork must retain its checkpoint pin")
				checkpoint, err := store.GetAgentInstanceCheckpoint(t.Context(), checkpointID, instance.Creator)
				require.NoError(t, err)
				require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, checkpoint.State)
			}
			require.EqualValues(t, 1, actors.mutations.Load())
		})
	}
}

func TestSupersededLifecycleObserverCannotExecute(t *testing.T) {
	for _, test := range []struct {
		name        string
		deleteAfter bool
		beforeRead  bool
	}{
		{name: "after suspend"},
		{name: "after delete", deleteAfter: true},
		{name: "lookup after delete", deleteAfter: true, beforeRead: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, base)
			instance, err := workflow.Create(t.Context(), instance)
			require.NoError(t, err)
			instance, err = workflow.Suspend(t.Context(), instance)
			require.NoError(t, err)
			read, release := make(chan struct{}), make(chan struct{})
			actors := &retryTestActors{lifecycleTestActors: base, afterRead: func(ctx context.Context) {
				close(read)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}}
			if test.beforeRead {
				actors.beforeRead, actors.afterRead = actors.afterRead, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			type outcome struct {
				instance *apiv1alpha1.AgentInstance
				err      error
			}
			result := make(chan outcome, 1)
			go func() {
				instance, err := NewActorWorkflow(store, actors).Resume(ctx, instance)
				result <- outcome{instance, err}
			}()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			ready, err := workflow.Resume(ctx, instance)
			require.NoError(t, err, "another replica can finish still-unissued work")
			suspended, err := workflow.Suspend(ctx, ready)
			require.NoError(t, err)
			if test.deleteAfter {
				_, err = workflow.Delete(ctx, suspended)
				require.NoError(t, err)
			}
			close(release)
			late := <-result
			require.ErrorIs(t, late.err, database.ErrConflict)
			require.Nil(t, late.instance)
			require.Zero(t, actors.mutations.Load(), "the delayed caller must not become another executor")
			current, err := store.GetAgentInstanceByID(ctx, instance.Id)
			if test.deleteAfter {
				require.ErrorIs(t, err, database.ErrNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, current.State)
			}
		})
	}
}

func TestDelayedCreationCannotResurrectDeletedActor(t *testing.T) {
	store, instance := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	read, release := make(chan struct{}), make(chan struct{})
	actors := &retryTestActors{lifecycleTestActors: base, afterRead: func(ctx context.Context) {
		close(read)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := NewActorWorkflow(store, actors).Create(ctx, instance)
		result <- err
	}()
	select {
	case <-read:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err := NewActorWorkflow(store, base).Delete(ctx, instance)
	require.NoError(t, err)
	close(release)
	require.ErrorIs(t, <-result, database.ErrConflict)
	require.Zero(t, actors.mutations.Load())
	_, err = base.GetActor(ctx, "team-a", substrate.ActorName(instance.Id))
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestLifecycleReadFailureCanRetryPreparation(t *testing.T) {
	store, instance := lifecycleFixture(t)
	base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	actors := &retryTestActors{lifecycleTestActors: base, readErr: status.Error(codes.Unavailable, "lookup unavailable")}
	_, err := NewActorWorkflow(store, actors).Create(t.Context(), instance)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Zero(t, actors.mutations.Load())
	ready, err := NewActorWorkflow(store, base).Create(t.Context(), instance)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, ready.State)
}

// completionTestStore injects failures at the database/runtime boundary while
// retaining real PostgreSQL instance ownership.
type completionTestStore struct {
	*lifecycleTestStore
	afterClaim func(context.Context)
	finishErr  error
}

func (s *completionTestStore) ClaimAgentInstanceOperation(ctx context.Context, instanceID string, id, executor uuid.UUID) (bool, error) {
	claimed, err := s.Client.ClaimAgentInstanceOperation(ctx, instanceID, id, executor)
	if claimed && err == nil && s.afterClaim != nil {
		s.afterClaim(ctx)
	}
	return claimed, err
}

func (s *completionTestStore) FinishAgentInstanceOperation(ctx context.Context, instanceID string, id, executor uuid.UUID, authority, failure string) (*apiv1alpha1.AgentInstance, error) {
	if s.finishErr != nil {
		return nil, s.finishErr
	}
	return s.Client.FinishAgentInstanceOperation(ctx, instanceID, id, executor, authority, failure)
}

func TestLifecycleCompletionFailureDoesNotRepeatRuntime(t *testing.T) {
	store, instance := lifecycleFixture(t)
	actors := &retryTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	failure := status.Error(codes.Unavailable, "completion database unavailable")
	_, err := NewActorWorkflow(&completionTestStore{lifecycleTestStore: store, finishErr: failure}, actors).Create(t.Context(), instance)
	require.ErrorIs(t, err, failure)
	_, err = NewActorWorkflow(store, actors).Create(t.Context(), instance)
	require.ErrorIs(t, err, database.ErrConflict)
	require.EqualValues(t, 1, actors.mutations.Load())
	operation, err := store.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	// Only the original executor with its known successful response may finish.
	_, err = store.FinishAgentInstanceOperation(t.Context(), instance.Id, operation.ID, operation.ExecutorID, substrate.ActorHost("team-a", substrate.ActorName(instance.Id), ""), "")
	require.NoError(t, err)
	ready, err := NewActorWorkflow(store, actors).Create(t.Context(), instance)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, ready.State)
	require.EqualValues(t, 1, actors.mutations.Load())
}

func TestClaimedCreationBlocksDeletionBeforeRuntimeCall(t *testing.T) {
	store, instance := lifecycleFixture(t)
	actors := &retryTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	claimed, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	delayed := &completionTestStore{lifecycleTestStore: store, afterClaim: func(ctx context.Context) {
		close(claimed)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}
	result := make(chan error, 1)
	go func() {
		_, err := NewActorWorkflow(delayed, actors).Create(ctx, instance)
		result <- err
	}()
	select {
	case <-claimed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	workflow := NewActorWorkflow(store, actors)
	_, err := workflow.Create(ctx, instance)
	require.ErrorIs(t, err, database.ErrConflict)
	_, err = workflow.Delete(ctx, instance)
	require.ErrorIs(t, err, database.ErrConflict)
	require.Zero(t, actors.mutations.Load())
	close(release)
	require.NoError(t, <-result)
	require.EqualValues(t, 1, actors.mutations.Load())
}

// Namespace provisioning belongs to the template controller. Lifecycle execution
// must work even when its caller cannot create an Atespace.
func (*retryTestActors) EnsureAtespace(context.Context, string) error {
	return status.Error(codes.PermissionDenied, "instance caller cannot create an Atespace")
}

func TestCreationUsesPreparedAtespace(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "fork"}[fork], func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			if fork {
				instance, _ = lifecycleForkFixture(t, store, base, instance)
			}
			actors := &retryTestActors{lifecycleTestActors: base}
			ready, err := NewActorWorkflow(store, actors).Create(t.Context(), instance)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, ready.State)
			require.EqualValues(t, 1, actors.mutations.Load())
		})
	}
}

func TestDelayedCreationObservesOnlyCurrentGeneration(t *testing.T) {
	for _, test := range []struct {
		name      string
		readErr   error
		supersede bool
	}{
		{name: "actor already created", supersede: true},
		{name: "lookup unavailable", readErr: status.Error(codes.Unavailable, "lookup unavailable"), supersede: true},
		{name: "same generation completed"},
		{name: "same generation completed despite local read error", readErr: status.Error(codes.Unavailable, "lookup unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			entered, release := make(chan struct{}), make(chan struct{})
			actors := &retryTestActors{lifecycleTestActors: base, readErr: test.readErr, beforeRead: func(ctx context.Context) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			type outcome struct {
				instance *apiv1alpha1.AgentInstance
				err      error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := NewActorWorkflow(store, actors).Create(ctx, instance)
				done <- outcome{result, err}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			workflow := NewActorWorkflow(store, base)
			ready, err := workflow.Create(ctx, instance)
			require.NoError(t, err)
			if test.supersede {
				_, err = workflow.Suspend(ctx, ready)
				require.NoError(t, err)
			}
			close(release)
			late := <-done
			if test.supersede {
				require.ErrorIs(t, late.err, database.ErrConflict)
				require.Nil(t, late.instance)
			} else {
				require.NoError(t, late.err)
				require.Equal(t, ready.Id, late.instance.Id)
				require.Equal(t, ready.State, late.instance.State)
			}
			require.Zero(t, actors.mutations.Load())
		})
	}
}
