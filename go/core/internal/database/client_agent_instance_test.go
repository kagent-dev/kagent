package database

import (
	"context"
	"errors"
	"testing"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
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

	request := dbpkg.AgentInstanceCreateParams{
		ID: "instance-1", Namespace: "team-a", Creator: "alice",
		HarnessName: "kagent", AgentTemplateName: "assistant", RequestID: "request-1",
	}
	created, wasCreated, err := client.CreateAgentInstance(ctx, request)
	if err != nil || !wasCreated {
		t.Fatalf("first CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	request.ID = "instance-2"
	replayed, wasCreated, err := client.CreateAgentInstance(ctx, request)
	if err != nil || wasCreated {
		t.Fatalf("replayed CreateAgentInstance() = created %v, error %v", wasCreated, err)
	}
	if replayed.GetId() != created.GetId() || replayed.GetPreparedRevision() != revision.Revision {
		t.Fatalf("replayed instance = %+v, want id %q revision %q", replayed, created.GetId(), revision.Revision)
	}
	if replayed.GetLabels()["team"] != "platform" {
		t.Fatalf("labels = %v", replayed.GetLabels())
	}

	request.AgentTemplateName = "different"
	if _, _, err := client.CreateAgentInstance(ctx, request); !errors.Is(err, dbpkg.ErrIdempotencyConflict) {
		t.Fatalf("conflicting request error = %v", err)
	}
}
