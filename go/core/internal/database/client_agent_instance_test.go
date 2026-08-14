package database

import (
	"context"
	"errors"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
)

func TestAgentInstanceCreateAndTransitions(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := context.Background()
	revision := dbpkg.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a",
		AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid",
		SourceSnapshot: []byte("{}"), EgressDestinations: []string{},
		ActorTemplateNamespace: "team-a", ActorTemplateName: "assistant-kagent-revision",
		ActorTemplateUID: "actor-template-uid", Phase: "Ready",
	}
	if err := client.UpsertRuntimeRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	pair := dbpkg.AgentTemplateHarnessPair{
		Namespace: "team-a", AgentTemplateName: "assistant", AgentTemplateUID: "template-uid",
		HarnessName: "kagent", HarnessUID: "harness-uid", DesiredRevision: revision.Revision,
	}
	if err := client.UpsertAgentTemplateHarnessPair(ctx, pair); err != nil {
		t.Fatal(err)
	}
	if err := client.MarkRuntimeRevisionSuccessful(ctx, pair); err != nil {
		t.Fatal(err)
	}

	request := &apiv1alpha1.AgentInstance{
		Id: "instance-1", Namespace: "team-a", Creator: "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
	}
	created, wasCreated, err := client.CreateAgentInstance(ctx, request, "request-1")
	if err != nil || !wasCreated {
		t.Fatalf("first CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	request.Id = "instance-2"
	replayed, wasCreated, err := client.CreateAgentInstance(ctx, request, "request-1")
	if err != nil || wasCreated {
		t.Fatalf("replayed CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	if replayed.GetId() != created.GetId() || replayed.GetPreparedRevision() != revision.Revision {
		t.Fatalf("replayed instance = %+v, want id %q revision %q", replayed, created.GetId(), revision.Revision)
	}
	if len(replayed.GetLabels()) != 0 {
		t.Fatalf("labels = %v", replayed.GetLabels())
	}
	instances, err := client.ListAgentInstances(ctx, "team-a", "alice", false, nil, "", 10)
	if err != nil || len(instances) != 1 {
		t.Fatalf("ListAgentInstances() = %v, error %v", instances, err)
	}
	ready, err := client.MarkAgentInstanceReady(ctx, created.GetId(), "actor.example")
	if err != nil {
		t.Fatal(err)
	}
	suspending := proto.Clone(ready).(*apiv1alpha1.AgentInstance)
	suspending.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
	if _, err := client.TransitionAgentInstance(ctx, suspending, ready.GetState(), ready.GetOperation()); err != nil {
		t.Fatal(err)
	}
	resuming := proto.Clone(ready).(*apiv1alpha1.AgentInstance)
	resuming.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME
	current, err := client.TransitionAgentInstance(ctx, resuming, ready.GetState(), ready.GetOperation())
	if !errors.Is(err, dbpkg.ErrAgentInstanceConflict) || current.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND {
		t.Fatalf("conflicting transition = instance %v, error %v", current, err)
	}
	suspended := proto.Clone(suspending).(*apiv1alpha1.AgentInstance)
	suspended.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
	suspended.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	if _, err := client.TransitionAgentInstance(ctx, suspended, ready.GetState(), suspending.GetOperation()); err != nil {
		t.Fatal(err)
	}

	request.AgentTemplate.Name = "different"
	if _, _, err := client.CreateAgentInstance(ctx, request, "request-1"); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting request error = %v", err)
	}
}
