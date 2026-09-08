package agentinstance

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestActorWorkflowLifecycle(t *testing.T) {
	instance := &apiv1alpha1.AgentInstance{
		Id:               "8bd650a8-9775-488f-8bc1-0d52bf7bdcab",
		PreparedRevision: "revision-1", State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
	}
	store := &lifecycleTestStore{
		instance: instance,
		revision: &database.RuntimeRevision{
			Revision: "revision-1", ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		},
	}
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors)

	created, err := workflow.Create(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || created.GetA2AAuthority() == "" {
		t.Fatalf("created instance = %+v", created)
	}
	if len(actors.actors) != 1 {
		t.Fatalf("actors = %v", actors.actors)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("created Actor status = %s", actor.GetStatus().GetState())
	}
	boundary, err := workflow.Quiesce(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || boundary.URI != "s3://snapshots/snapshot-1" {
		t.Fatalf("quiesced instance = %+v, boundary = %+v", created, boundary)
	}

	suspended, err := workflow.Suspend(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED || suspended.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("suspended instance = %+v", suspended)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("suspended Actor status = %s", actor.GetStatus().GetState())
	}

	resumed, err := workflow.Resume(context.Background(), suspended)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || resumed.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("resumed instance = %+v", resumed)
	}

	deleted, err := workflow.Delete(context.Background(), resumed)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED || store.instance != nil || len(actors.actors) != 0 {
		t.Fatalf("deleted instance = %+v, actors = %v", deleted, actors.actors)
	}
}

func TestActorWorkflowForkCreatesSuspendedActorFromCheckpoint(t *testing.T) {
	instance := &apiv1alpha1.AgentInstance{
		Id: "fork-1", PreparedRevision: "revision-1",
		State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
	}
	store := &lifecycleTestStore{
		instance: instance,
		revision: &database.RuntimeRevision{
			Revision: "revision-1", ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		},
	}
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	snapshot := &database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}
	fork, err := NewActorWorkflow(store, actors).Fork(context.Background(), instance, snapshot, "checkpoint-018f47a2-4efb-7c21-a848-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]
	if fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		actor.GetSourceTag().GetName() != "checkpoint-018f47a2-4efb-7c21-a848-123456789abc" {
		t.Fatalf("fork = %+v, actor = %+v", fork, actor)
	}
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
	store.instance = instance
	actor.Status.ExternalSnapshot.SnapshotUri = "s3://snapshots/later-turn"
	if _, err := NewActorWorkflow(store, actors).Fork(context.Background(), instance, snapshot, "checkpoint-018f47a2-4efb-7c21-a848-123456789abc"); err == nil {
		t.Fatal("Fork() accepted a different external snapshot")
	}
	actor.Status.ExternalSnapshot.SnapshotUri = snapshot.URI
	actor.SourceTag.Name = "wrong-tag"
	if _, err := NewActorWorkflow(store, actors).Fork(context.Background(), instance, snapshot, "checkpoint-018f47a2-4efb-7c21-a848-123456789abc"); err == nil {
		t.Fatal("Fork() accepted an existing Actor with the wrong snapshot tag")
	}
}

type lifecycleTestStore struct {
	instance *apiv1alpha1.AgentInstance
	revision *database.RuntimeRevision
}

func (s *lifecycleTestStore) GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error) {
	return s.revision, nil
}

func (s *lifecycleTestStore) MarkAgentInstanceReady(_ context.Context, _ string, authority string) (*apiv1alpha1.AgentInstance, error) {
	s.instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	s.instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	s.instance.A2AAuthority = authority
	return s.instance, nil
}

func (s *lifecycleTestStore) TransitionAgentInstance(_ context.Context, instance *apiv1alpha1.AgentInstance, expectedState apiv1alpha1.AgentInstanceState, expectedOperation apiv1alpha1.AgentInstanceOperation) (*apiv1alpha1.AgentInstance, error) {
	if s.instance.GetState() != expectedState || s.instance.GetOperation() != expectedOperation {
		return s.instance, database.ErrAgentInstanceConflict
	}
	s.instance = proto.Clone(instance).(*apiv1alpha1.AgentInstance)
	return s.instance, nil
}

func (s *lifecycleTestStore) DeleteAgentInstance(context.Context, string) error {
	s.instance = nil
	return nil
}

type lifecycleTestActors struct {
	actors map[string]*ateapipb.Actor
}

func actorKey(atespace, name string) string { return atespace + "/" + name }

func (*lifecycleTestActors) EnsureAtespace(context.Context, string) error { return nil }

func (a *lifecycleTestActors) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	actor := a.actors[actorKey(atespace, name)]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return actor, nil
}

func (a *lifecycleTestActors) CreateActor(_ context.Context, atespace, name, templateNamespace, templateName string) (*ateapipb.Actor, error) {
	actor := &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateNamespace, Name: templateName},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
	}
	a.actors[actorKey(atespace, name)] = actor
	return actor, nil
}

func (a *lifecycleTestActors) CreateActorFromTag(_ context.Context, atespace, name, templateNamespace, templateName, tagAtespace, tagName string) (*ateapipb.Actor, error) {
	actor := &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateNamespace, Name: templateName},
		SourceTag:     &ateapipb.ObjectRef{Atespace: tagAtespace, Name: tagName},
		Status: &ateapipb.ActorStatus{
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA},
		},
	}
	a.actors[actorKey(atespace, name)] = actor
	return actor, nil
}

func (a *lifecycleTestActors) ResumeActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	return actor, nil
}

func (a *lifecycleTestActors) SuspendActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}
	return actor, nil
}

func (a *lifecycleTestActors) DeleteActor(_ context.Context, atespace, name string) error {
	delete(a.actors, actorKey(atespace, name))
	return nil
}

func TestQuiesceRejectsWrongActorIdentity(t *testing.T) {
	instance := &apiv1alpha1.AgentInstance{Id: "instance-1", PreparedRevision: "revision-1"}
	store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}}
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{
		actorKey("team-a", substrate.ActorName(instance.Id)): {
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "different-actor", Uid: "actor-uid"},
			Status:   &ateapipb.ActorStatus{},
		},
	}}
	if _, err := NewActorWorkflow(store, actors).Quiesce(t.Context(), instance); err == nil {
		t.Fatal("Quiesce() accepted the wrong Actor")
	}
}
