package kubecrud

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
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

func TestServiceFiltersBeforeSortingAndUsesTrustedAttributes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	authorizer := &recordingAuthorizer{scope: auth.AuthorizationScope{
		Kind: auth.ScopeAnyOf,
		AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{
			Attribute: auth.AttributeName,
			Operator:  auth.ScopeIn,
			Values:    []string{"a", "b"},
		}}}},
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "b"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "denied"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "a"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "mutable"}},
	).Build()
	service := NewService(kubeClient, authorizer, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate")
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	listed, err := service.List(ctx, "team")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 2 || listed[0].Name != "a" || listed[1].Name != "b" {
		t.Fatalf("List() names = %v, want [a b]", []string{listed[0].Name, listed[1].Name})
	}
	if authorizer.scopeVerb != auth.VerbList || authorizer.scopeType != "AgentTemplate" {
		t.Fatalf("Scope() = (%q, %q), want (list, AgentTemplate)", authorizer.scopeVerb, authorizer.scopeType)
	}

	if _, err := service.Get(ctx, types.NamespacedName{Namespace: "team", Name: "a"}); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := service.Create(ctx, &v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "created"}}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Update(ctx, types.NamespacedName{Namespace: "team", Name: "mutable"}, func(mutable *v1alpha3.AgentTemplate) {
		mutable.Spec.Description = "updated"
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := service.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "b"}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	wantVerbs := []auth.Verb{auth.VerbGet, auth.VerbCreate, auth.VerbUpdate, auth.VerbUpdate, auth.VerbDelete}
	wantNames := []string{"a", "created", "mutable", "mutable", "b"}
	if len(authorizer.checkCalls) != len(wantVerbs) {
		t.Fatalf("Check() calls = %d, want %d", len(authorizer.checkCalls), len(wantVerbs))
	}
	for index, call := range authorizer.checkCalls {
		if call.verb != wantVerbs[index] {
			t.Errorf("Check() call %d verb = %q, want %q", index, call.verb, wantVerbs[index])
		}
		if call.resource.Type != "AgentTemplate" || len(call.resource.Attributes[auth.AttributeNamespace]) != 1 || call.resource.Attributes[auth.AttributeNamespace][0] != "team" {
			t.Errorf("Check() call %d resource = %+v", index, call.resource)
		}
		if got := call.resource.Attributes[auth.AttributeName]; len(got) != 1 || got[0] != wantNames[index] {
			t.Errorf("Check() call %d name = %v, want %q", index, got, wantNames[index])
		}
	}
}

func TestHarnessServiceFiltersList(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	authorizer := &recordingAuthorizer{scope: auth.AuthorizationScope{
		Kind: auth.ScopeAnyOf,
		AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{
			Attribute: auth.AttributeName,
			Operator:  auth.ScopeIn,
			Values:    []string{"allowed"},
		}}}},
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}},
		&v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "denied"}},
	).Build()
	service := NewService(kubeClient, authorizer, &v1alpha3.Harness{}, &v1alpha3.HarnessList{}, "Harness")
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	listed, err := service.List(ctx, "team")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "allowed" {
		t.Fatalf("List() = %v, want [allowed]", listed)
	}
	if authorizer.scopeVerb != auth.VerbList || authorizer.scopeType != "Harness" {
		t.Fatalf("Scope() = (%q, %q), want (list, Harness)", authorizer.scopeVerb, authorizer.scopeType)
	}
}

func TestServiceRejectsInvalidScope(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	authorizer := &recordingAuthorizer{scope: auth.AuthorizationScope{Kind: auth.ScopeAnyOf}}
	service := NewService(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		authorizer,
		&v1alpha3.AgentTemplate{},
		&v1alpha3.AgentTemplateList{},
		"AgentTemplate",
	)
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	_, err := service.List(ctx, "team")
	if err == nil || !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("List() error = %v, want permission denied", err)
	}
}
