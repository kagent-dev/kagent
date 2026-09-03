package agent

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testSession struct{}

func (testSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: "test-user"}}
}

type authorizationCall struct {
	verb     auth.Verb
	resource auth.Resource
}

type recordingAuthorizer struct {
	scope      auth.AuthorizationScope
	scopeVerb  auth.Verb
	scopeType  string
	checkCalls []authorizationCall
}

func (a *recordingAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.checkCalls = append(a.checkCalls, authorizationCall{verb: verb, resource: resource})
	return nil
}

func (a *recordingAuthorizer) Scope(_ context.Context, _ auth.Principal, verb auth.Verb, resourceType string) (auth.AuthorizationScope, error) {
	a.scopeVerb = verb
	a.scopeType = resourceType
	return a.scope, nil
}

func TestListFiltersOnlyAgentHarnesses(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.SandboxAgent{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "sandbox"}},
		&v1alpha3.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"},
			Spec:       v1alpha3.AgentHarnessSpec{Backend: v1alpha3.AgentHarnessBackendOpenClaw},
		},
		&v1alpha3.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "denied"},
			Spec:       v1alpha3.AgentHarnessSpec{Backend: v1alpha3.AgentHarnessBackendHermes},
		},
	).Build()
	authorizer := &recordingAuthorizer{scope: auth.AuthorizationScope{
		Kind: auth.ScopeAnyOf,
		AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{
			Attribute: auth.AttributeName,
			Operator:  auth.ScopeIn,
			Values:    []string{"allowed"},
		}}}},
	}}
	service := NewService(kubeClient, authorizer, "default")
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	views, err := service.List(ctx, ListRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(views) != 2 || views[0].Kind != KindSandboxAgent || views[1].Kind != KindAgentHarness || views[1].Ref.Name != "allowed" {
		t.Fatalf("List() = %+v, want sandbox plus allowed AgentHarness", views)
	}
	if authorizer.scopeVerb != auth.VerbList || authorizer.scopeType != string(KindAgentHarness) {
		t.Fatalf("Scope() = (%q, %q), want (list, AgentHarness)", authorizer.scopeVerb, authorizer.scopeType)
	}
	if len(authorizer.checkCalls) != 1 || authorizer.checkCalls[0].resource.Type != "Agent" {
		t.Fatalf("SandboxAgent collection Check() calls = %+v", authorizer.checkCalls)
	}

	authorizer.scope = auth.AuthorizationScope{Kind: auth.ScopeNone}
	views, err = service.List(ctx, ListRequest{Namespace: "team"})
	if err != nil {
		t.Fatalf("List(ScopeNone) error = %v", err)
	}
	if len(views) != 1 || views[0].Kind != KindSandboxAgent {
		t.Fatalf("List(ScopeNone) = %+v, want only SandboxAgent", views)
	}
}

func TestAgentHarnessCRUDUsesTrustedAttributes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	existing := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "existing"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: v1alpha3.AgentHarnessBackendOpenClaw},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	authorizer := &recordingAuthorizer{scope: auth.AuthorizationScope{Kind: auth.ScopeAll}}
	service := NewService(kubeClient, authorizer, "default")
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	if _, err := service.GetAgentHarness(ctx, GetRequest{Ref: types.NamespacedName{Namespace: "team", Name: "existing"}}); err != nil {
		t.Fatalf("GetAgentHarness() error = %v", err)
	}
	if _, err := service.CreateAgentHarness(ctx, CreateAgentHarnessRequest{AgentHarness: &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "created"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: v1alpha3.AgentHarnessBackendHermes},
	}}); err != nil {
		t.Fatalf("CreateAgentHarness() error = %v", err)
	}
	if err := service.DeleteAgentHarness(ctx, DeleteRequest{Ref: types.NamespacedName{Namespace: "team", Name: "existing"}}); err != nil {
		t.Fatalf("DeleteAgentHarness() error = %v", err)
	}

	wantVerbs := []auth.Verb{auth.VerbGet, auth.VerbCreate, auth.VerbDelete}
	wantNames := []string{"existing", "created", "existing"}
	if len(authorizer.checkCalls) != len(wantVerbs) {
		t.Fatalf("Check() calls = %d, want %d", len(authorizer.checkCalls), len(wantVerbs))
	}
	for index, call := range authorizer.checkCalls {
		if call.verb != wantVerbs[index] || call.resource.Type != string(KindAgentHarness) {
			t.Errorf("Check() call %d = %+v", index, call)
		}
		if got := call.resource.Attributes[auth.AttributeNamespace]; len(got) != 1 || got[0] != "team" {
			t.Errorf("Check() call %d namespace = %v", index, got)
		}
		if got := call.resource.Attributes[auth.AttributeName]; len(got) != 1 || got[0] != wantNames[index] {
			t.Errorf("Check() call %d name = %v, want %q", index, got, wantNames[index])
		}
	}
}
