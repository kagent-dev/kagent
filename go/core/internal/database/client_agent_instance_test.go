package database

import (
	"context"
	"errors"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func TestCreateAgentInstanceIsIdempotent(t *testing.T) {
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
		AgentTemplateLabels: map[string]string{"team": "platform"},
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
	if replayed.GetLabels()["team"] != "platform" {
		t.Fatalf("labels = %v", replayed.GetLabels())
	}

	request.AgentTemplate.Name = "different"
	if _, _, err := client.CreateAgentInstance(ctx, request, "request-1"); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting request error = %v", err)
	}
}
