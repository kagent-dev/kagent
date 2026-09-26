package substrate

import (
	"context"
	"errors"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Unexpected calls use the embedded nil interface and fail the test.
type transitionActors struct {
	LifecycleClient
	actor     *ateapipb.Actor
	calls     []string
	policyErr error
	resumeUID string
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

func (a *transitionActors) EnsureActorEgressPolicy(context.Context, string, string, *ateapipb.EgressPolicy) error {
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
	return proto.CloneOf(a.actor), nil
}

func (a *transitionActors) DeleteActor(context.Context, string, string) error {
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
			binding := ActorBinding{Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
			transition, err := PrepareActorTransition(t.Context(), actors, binding, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE,
				&ActorCreation{EgressPolicy: &ateapipb.EgressPolicy{}, Resume: test.resume})
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
				ActorBinding{Atespace: "team", Name: "instance", TemplateAtespace: "team", TemplateName: "revision"},
				apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE, &ActorCreation{EgressPolicy: &ateapipb.EgressPolicy{}, Resume: true})
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
		operation apiv1alpha1.RuntimeOperation
		mutate    func(*ateapipb.Actor)
	}{
		{name: "never adopt existing actor", operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE},
		{name: "wrong template", operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE, mutate: func(a *ateapipb.Actor) { a.ActorTemplate.Name = "other" }},
		{name: "wrong identity", operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, mutate: func(a *ateapipb.Actor) { a.Metadata.Name = "other" }},
		{name: "missing UID", operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, mutate: func(a *ateapipb.Actor) { a.Metadata.Uid = "" }},
		{name: "crashed actor cannot resume", operation: apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, mutate: func(a *ateapipb.Actor) { a.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED }},
	} {
		t.Run(test.name, func(t *testing.T) {
			actors := &transitionActors{}
			_, err := actors.CreateActor(t.Context(), "team-a", "instance", "team-a", "revision")
			require.NoError(t, err)
			actors.calls = nil
			if test.mutate != nil {
				test.mutate(actors.actor)
			}
			binding := ActorBinding{Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
			_, err = PrepareActorTransition(t.Context(), actors, binding, test.operation, &ActorCreation{EgressPolicy: &ateapipb.EgressPolicy{}})
			require.Error(t, err)
			require.Empty(t, actors.calls)
		})
	}
}

func TestActorDeleteStopsRunningCompute(t *testing.T) {
	actors := &transitionActors{}
	_, err := actors.CreateActor(t.Context(), "team-a", "instance", "team-a", "revision")
	require.NoError(t, err)
	actors.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	actors.calls = nil
	binding := ActorBinding{Atespace: "team-a", Name: "instance", TemplateAtespace: "team-a", TemplateName: "revision"}
	transition, err := PrepareActorTransition(t.Context(), actors, binding, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE, nil)
	require.NoError(t, err)
	require.NoError(t, ApplyActorTransition(t.Context(), actors, transition))
	require.Equal(t, []string{"suspend", "delete"}, actors.calls)

	actors.calls = nil
	transition, err = PrepareActorTransition(t.Context(), actors, binding, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE, nil)
	require.NoError(t, err)
	require.NoError(t, ApplyActorTransition(t.Context(), actors, transition))
	require.Empty(t, actors.calls, "a missing Actor already satisfies deletion")
}
