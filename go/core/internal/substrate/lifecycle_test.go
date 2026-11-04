package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// Unexpected calls use the embedded nil interface and fail the test.
type transitionActors struct {
	LifecycleClient
	actor           *ateapipb.Actor
	calls           []string
	policyErr       error
	checkErr        error
	resumeUID       string
	suspendSnapshot *ateapipb.Snapshot
	revertUID       string
	revertSnapshot  *ateapipb.Snapshot
}

func (a *transitionActors) GetActor(context.Context, string, string) (*ateapipb.Actor, error) {
	if a.actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(a.actor), nil
}

func (a *transitionActors) CreateActor(_ context.Context, space, name, templateSpace, templateName string) (*ateapipb.Actor, error) {
	a.calls = append(a.calls, "create")
	a.actor = &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateSpace, Name: templateName},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
	}
	return proto.CloneOf(a.actor), nil
}

func (a *transitionActors) CheckActorEgressPolicy(context.Context, string, string) error {
	return a.checkErr
}

func (a *transitionActors) EnsureActorEgressPolicy(context.Context, string, string, string) error {
	a.calls = append(a.calls, "policy")
	return a.policyErr
}

func (a *transitionActors) ResumeActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.calls = append(a.calls, "resume")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	if a.resumeUID != "" {
		a.actor.Metadata.Uid = a.resumeUID
	}
	return proto.CloneOf(a.actor), nil
}

func (a *transitionActors) SuspendActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.calls = append(a.calls, "suspend")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	a.actor.Status.Snapshots = []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/data"}}}}
	if a.suspendSnapshot != nil {
		a.actor.Status.Snapshots = []*ateapipb.Snapshot{proto.CloneOf(a.suspendSnapshot)}
	}
	return proto.CloneOf(a.actor), nil
}

// RevertActor restores the latest durable snapshot unless the test overrides
// the identity or snapshot it returns.
func (a *transitionActors) RevertActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.calls = append(a.calls, "revert")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	snapshot := proto.CloneOf(LatestDurableActorSnapshot(a.actor))
	if a.revertSnapshot != nil {
		snapshot = proto.CloneOf(a.revertSnapshot)
	}
	a.actor.Status.Snapshots = nil
	if snapshot != nil {
		snapshot.Locality = ""
		a.actor.Status.Snapshots = []*ateapipb.Snapshot{snapshot}
	}
	if a.revertUID != "" {
		a.actor.Metadata.Uid = a.revertUID
	}
	return proto.CloneOf(a.actor), nil
}

func (a *transitionActors) DeleteActor(context.Context, string, string, string) error {
	a.calls = append(a.calls, "delete")
	a.actor = nil
	return nil
}

func TestActorCreationStartupPolicy(t *testing.T) {
	for _, test := range []struct {
		name      string
		resume    bool
		policyErr error
		resumeUID string
		wantCalls []string
		wantState ateapipb.ActorState
		wantError bool
	}{
		{name: "agent waits for A2A", wantCalls: []string{"create", "policy"}, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "sandbox starts guest", resume: true, wantCalls: []string{"create", "policy", "resume"}, wantState: ateapipb.ActorState_ACTOR_STATE_RUNNING},
		{name: "policy failure prevents startup", resume: true, policyErr: errors.New("unavailable"), wantCalls: []string{"create", "policy"}, wantState: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, wantError: true},
		{name: "replacement during startup rejected", resume: true, resumeUID: "replacement", wantCalls: []string{"create", "policy", "resume"}, wantState: ateapipb.ActorState_ACTOR_STATE_RUNNING, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actors := &transitionActors{policyErr: test.policyErr, resumeUID: test.resumeUID}
			binding := ActorBinding{Revision: "revision", Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
			transition, err := PrepareActorTransition(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE,
				&ActorCreation{Resume: test.resume})
			require.NoError(t, err)
			require.Empty(t, actors.calls, "preparation must not mutate the backend")
			err = ApplyActorTransition(t.Context(), actors, transition)
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantCalls, actors.calls)
			require.Equal(t, test.wantState, actors.actor.Status.State)
		})
	}
}

func TestActorRetryContinuesExistingCreation(t *testing.T) {
	for _, state := range []ateapipb.ActorState{
		ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
		ateapipb.ActorState_ACTOR_STATE_RESUMING,
		ateapipb.ActorState_ACTOR_STATE_RUNNING,
	} {
		t.Run(state.String(), func(t *testing.T) {
			actors := &transitionActors{actor: &ateapipb.Actor{
				Metadata:      &ateapipb.ResourceMetadata{Atespace: "team", Name: "instance", Uid: "original"},
				ActorTemplate: &ateapipb.ObjectRef{Atespace: "team", Name: "revision"},
				Status:        &ateapipb.ActorStatus{State: state},
			}}
			transition, err := PrepareActorRetry(t.Context(), actors,
				ActorBinding{Revision: "revision", Atespace: "team", Name: "instance", TemplateAtespace: "team", TemplateName: "revision"},
				apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE, &ActorCreation{Resume: true})
			require.NoError(t, err)
			require.NoError(t, ApplyActorTransition(t.Context(), actors, transition))
			require.Equal(t, "original", actors.actor.Metadata.Uid)
			want := []string{"policy", "resume"}
			if state == ateapipb.ActorState_ACTOR_STATE_RUNNING {
				want = []string{"policy"}
			}
			require.Equal(t, want, actors.calls)
		})
	}
}

func TestActorTransitionAdmission(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation apiv1.RuntimeOperation
		mutate    func(*ateapipb.Actor)
	}{
		{name: "never adopt existing actor", operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE},
		{name: "wrong template", operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, mutate: func(a *ateapipb.Actor) { a.ActorTemplate.Name = "other" }},
		{name: "wrong identity", operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, mutate: func(a *ateapipb.Actor) { a.Metadata.Name = "other" }},
		{name: "missing UID", operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, mutate: func(a *ateapipb.Actor) { a.Metadata.Uid = "" }},
		{name: "crashed actor cannot resume", operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, mutate: func(a *ateapipb.Actor) { a.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED }},
	} {
		t.Run(test.name, func(t *testing.T) {
			actors := &transitionActors{}
			_, err := actors.CreateActor(t.Context(), "team-a", "instance", "team-a", "revision")
			require.NoError(t, err)
			actors.calls = nil
			if test.mutate != nil {
				test.mutate(actors.actor)
			}
			binding := ActorBinding{Revision: "revision", Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
			_, err = PrepareActorTransition(t.Context(), actors, binding, test.operation, &ActorCreation{})
			require.Error(t, err)
			require.Empty(t, actors.calls)
		})
	}
}

func TestActorTransitionChecksEgressPolicyBeforeStartup(t *testing.T) {
	binding := ActorBinding{Revision: "revision", Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
	actors := &transitionActors{checkErr: errors.New("target not bound")}
	_, err := PrepareActorTransition(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE, &ActorCreation{})
	require.ErrorContains(t, err, "target not bound")
	_, err = actors.CreateActor(t.Context(), "team-a", "instance", "team-a", "revision")
	require.NoError(t, err)
	_, err = PrepareActorTransition(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, nil)
	require.ErrorContains(t, err, "target not bound")
	for _, operation := range []apiv1.RuntimeOperation{apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE} {
		_, err = PrepareActorTransition(t.Context(), actors, binding, operation, nil)
		require.NoError(t, err, "%s must not depend on the egress policy", operation)
	}

	// An issued resume may already be running; its retry must reach Ensure.
	actors.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	actors.calls, actors.policyErr = nil, errors.New("denied actor egress after extension failure")
	transition, err := PrepareActorRetry(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, nil)
	require.NoError(t, err)
	require.Error(t, ApplyActorTransition(t.Context(), actors, transition))
	require.Equal(t, []string{"policy"}, actors.calls)
}

func TestActorDeletionDoesNotSuspend(t *testing.T) {
	for _, state := range []ateapipb.ActorState{
		ateapipb.ActorState_ACTOR_STATE_RESUMING,
		ateapipb.ActorState_ACTOR_STATE_RUNNING,
		ateapipb.ActorState_ACTOR_STATE_SUSPENDING,
		ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
		ateapipb.ActorState_ACTOR_STATE_CRASHED,
		ateapipb.ActorState_ACTOR_STATE_DELETING,
	} {
		t.Run("Given an actor in "+state.String()+", when deletion runs, assert that it deletes without suspension", func(t *testing.T) {
			actors := &transitionActors{}
			_, err := actors.CreateActor(t.Context(), "team-a", "instance", "team-a", "revision")
			require.NoError(t, err)
			actors.actor.Status.State = state
			actors.calls = nil
			binding := ActorBinding{Revision: "revision", Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
			transition, err := PrepareActorTransition(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, nil)
			require.NoError(t, err)
			require.NoError(t, ApplyActorTransition(t.Context(), actors, transition))
			require.Equal(t, []string{"delete"}, actors.calls)
			require.Nil(t, actors.actor)

			actors.calls = nil
			transition, err = PrepareActorTransition(t.Context(), actors, binding, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, nil)
			require.NoError(t, err)
			require.NoError(t, ApplyActorTransition(t.Context(), actors, transition))
			require.Empty(t, actors.calls, "a missing Actor already satisfies deletion")
		})
	}
}

func TestActorSuspendRequiresDurableVolumesSnapshot(t *testing.T) {
	for _, test := range []struct {
		name     string
		snapshot *ateapipb.Snapshot
	}{
		{name: "missing URI", snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES}},
		{name: "MEMORY rejected", snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY,
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/memory"}}}},
		{name: "ROOTFS rejected", snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS,
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/rootfs"}}}},
		{name: "pending upload", snapshot: &ateapipb.Snapshot{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_PENDING, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/pending"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actors := &transitionActors{suspendSnapshot: test.snapshot}
			_, err := actors.CreateActor(t.Context(), "team", "instance", "team", "revision")
			require.NoError(t, err)
			transition, err := PrepareActorTransition(t.Context(), actors, ActorBinding{Revision: "revision", Atespace: "team", Name: "instance", TemplateAtespace: "team", TemplateName: "revision"}, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, nil)
			require.NoError(t, err)
			require.ErrorContains(t, ApplyActorTransition(t.Context(), actors, transition), "durable VOLUMES snapshot")
		})
	}
}

func TestActorRevertRestoresTheObservedSnapshotInPlace(t *testing.T) {
	durable := func(generation int32, uri string) *ateapipb.Snapshot {
		return &ateapipb.Snapshot{Generation: generation, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, Locality: "lost-node",
			DurableSnapshot: &ateapipb.SnapshotStorage{State: ateapipb.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: uri}}}
	}
	for _, test := range []struct {
		name           string
		missing        bool
		state          ateapipb.ActorState
		revertUID      string
		revertSnapshot *ateapipb.Snapshot
		wantPrepareErr bool
		wantApplyErr   bool
		wantCalls      []string
	}{
		{name: "crashed", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, wantCalls: []string{"revert"}},
		{name: "revert in progress", state: ateapipb.ActorState_ACTOR_STATE_REVERTING, wantCalls: []string{"revert"}},
		{name: "earlier revert response lost", state: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
		{name: "running actor", state: ateapipb.ActorState_ACTOR_STATE_RUNNING, wantPrepareErr: true},
		{name: "actor gone", missing: true, wantPrepareErr: true},
		{name: "replaced actor", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, revertUID: "another-actor", wantApplyErr: true, wantCalls: []string{"revert"}},
		{name: "different snapshot", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, revertSnapshot: durable(1, "s3://snapshots/older"), wantApplyErr: true, wantCalls: []string{"revert"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actors := &transitionActors{revertUID: test.revertUID, revertSnapshot: test.revertSnapshot}
			if !test.missing {
				actors.actor = &ateapipb.Actor{
					Metadata:      &ateapipb.ResourceMetadata{Atespace: "team", Name: "instance", Uid: "actor-uid"},
					ActorTemplate: &ateapipb.ObjectRef{Atespace: "team", Name: "revision"},
					Status:        &ateapipb.ActorStatus{State: test.state, Snapshots: []*ateapipb.Snapshot{durable(2, "s3://snapshots/latest")}},
				}
				if test.state == ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
					actors.actor.Status.Snapshots[0].Locality = ""
				}
			}
			binding := ActorBinding{Revision: "revision", Atespace: "team", Name: "instance", TemplateAtespace: "team", TemplateName: "revision"}
			transition, err := PrepareActorRevert(t.Context(), actors, binding)
			if test.wantPrepareErr {
				require.Error(t, err)
				require.Empty(t, actors.calls, "a failed read issues nothing")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "actor-uid", transition.ActorUID())
			err = ApplyActorTransition(t.Context(), actors, transition)
			if test.wantApplyErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantCalls, actors.calls)
		})
	}
}
