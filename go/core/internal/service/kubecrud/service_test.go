package kubecrud_test

import (
	"context"
	"errors"
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	scope      apiauthorization.AuthorizationScope
	scopeVerb  auth.Verb
	scopeType  string
	checkCalls []authorizationCall
	checkErr   error
}

func (a *recordingAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.checkCalls = append(a.checkCalls, authorizationCall{verb: verb, resource: resource})
	return a.checkErr
}

func (a *recordingAuthorizer) Scope(_ context.Context, _ auth.Principal, verb auth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.scopeVerb = verb
	a.scopeType = resourceType
	return a.scope, nil
}

func TestServiceFiltersBeforeSortingAndUsesTrustedAttributes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	authorizer := &recordingAuthorizer{scope: apiauthorization.AuthorizationScope{
		Kind: apiauthorization.ScopeAnyOf,
		AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{
			Attribute: apiauthorization.AttributeName,
			Operator:  apiauthorization.ScopeIn,
			Values:    []string{"a", "b"},
		}}}},
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "b"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "denied"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "a"}},
		&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "mutable"}},
	).Build()
	service := kubecrud.NewService(kubeClient, authorizer, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate")
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
	mutableRef := types.NamespacedName{Namespace: "team", Name: "mutable"}
	mutable, err := service.GetForUpdate(ctx, mutableRef)
	if err != nil {
		t.Fatalf("GetForUpdate() error = %v", err)
	}
	mutable.Spec.Description = "updated"
	if _, err := service.SaveUpdate(ctx, mutable); err != nil {
		t.Fatalf("SaveUpdate() error = %v", err)
	}
	if err := service.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "b"}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	wantVerbs := []auth.Verb{auth.VerbGet, auth.VerbCreate, auth.VerbUpdate, auth.VerbDelete}
	wantNames := []string{"a", "created", "mutable", "b"}
	if len(authorizer.checkCalls) != len(wantVerbs) {
		t.Fatalf("Check() calls = %d, want %d", len(authorizer.checkCalls), len(wantVerbs))
	}
	for index, call := range authorizer.checkCalls {
		if call.verb != wantVerbs[index] {
			t.Errorf("Check() call %d verb = %q, want %q", index, call.verb, wantVerbs[index])
		}
		if call.resource.Type != "AgentTemplate" || call.resource.Namespace != "team" {
			t.Errorf("Check() call %d resource = %+v", index, call.resource)
		}
		if got := call.resource.Name; got != wantNames[index] {
			t.Errorf("Check() call %d name = %v, want %q", index, got, wantNames[index])
		}
	}
}

func TestHarnessServiceFiltersList(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	authorizer := &recordingAuthorizer{scope: apiauthorization.AuthorizationScope{
		Kind: apiauthorization.ScopeAnyOf,
		AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{
			Attribute: apiauthorization.AttributeName,
			Operator:  apiauthorization.ScopeIn,
			Values:    []string{"allowed"},
		}}}},
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}},
		&v1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "denied"}},
	).Build()
	service := kubecrud.NewService(kubeClient, authorizer, &v1alpha3.Harness{}, &v1alpha3.HarnessList{}, "Harness")
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
	authorizer := &recordingAuthorizer{scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf}}
	service := kubecrud.NewService(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		authorizer,
		&v1alpha3.AgentTemplate{},
		&v1alpha3.AgentTemplateList{},
		"AgentTemplate",
	)
	ctx := auth.AuthSessionTo(t.Context(), testSession{})

	_, err := service.List(ctx, "team")
	if err == nil || !serviceerrors.IsCode(err, serviceerrors.CodeInternal) {
		t.Fatalf("List() error = %v, want internal", err)
	}
}

// readRecordingClient counts the reads that reach Kubernetes.
type readRecordingClient struct {
	client.Client
	gets int
}

func (c *readRecordingClient) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	c.gets++
	return c.Client.Get(ctx, key, object, options...)
}

// A denied caller must not be able to tell an existing object from a missing one.
func TestDeniedSingleResourceOperationsDoNotRevealExistence(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha3.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	refs := map[string]types.NamespacedName{
		"existing": {Namespace: "team", Name: "existing"},
		"missing":  {Namespace: "team", Name: "missing"},
	}
	operations := map[string]func(*kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList], context.Context, types.NamespacedName) error{
		"Get": func(s *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList], ctx context.Context, ref types.NamespacedName) error {
			_, err := s.Get(ctx, ref)
			return err
		},
		"GetForUpdate": func(s *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList], ctx context.Context, ref types.NamespacedName) error {
			_, err := s.GetForUpdate(ctx, ref)
			return err
		},
		"Delete": func(s *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList], ctx context.Context, ref types.NamespacedName) error {
			return s.Delete(ctx, ref)
		},
	}
	for operation, call := range operations {
		for existence, ref := range refs {
			t.Run(operation+"/"+existence, func(t *testing.T) {
				kubeClient := &readRecordingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(
					&v1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "existing"}},
				).Build()}
				authorizer := &recordingAuthorizer{checkErr: errors.New("denied")}
				service := kubecrud.NewService(kubeClient, authorizer, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate")
				ctx := auth.AuthSessionTo(t.Context(), testSession{})

				err := call(service, ctx, ref)
				if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
					t.Fatalf("%s() error = %v, want permission denied", operation, err)
				}
				if kubeClient.gets != 0 {
					t.Fatalf("%s() denied the caller but read Kubernetes %d times", operation, kubeClient.gets)
				}
			})
		}
	}
}
