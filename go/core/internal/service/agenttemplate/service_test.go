package agenttemplate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/agenttemplate"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type denyAuthorizer struct{}

func (denyAuthorizer) Check(context.Context, pkgauth.Principal, pkgauth.Verb, pkgauth.Resource) error {
	return errors.New("denied")
}

func template(namespace, name, modelConfig string) *v1alpha3.AgentTemplate {
	return &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: v1alpha3.AgentTemplateLocalReference{Name: modelConfig},
		},
	}
}

func newService(t *testing.T, authorizer pkgauth.Authorizer, objects ...ctrlclient.Object) (*agenttemplate.Service, context.Context) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	ctx := pkgauth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{
		P: pkgauth.Principal{User: pkgauth.User{ID: "template-user"}},
	})
	return agenttemplate.NewService(kubeClient, authorizer), ctx
}

func TestList(t *testing.T) {
	service, ctx := newService(t, &authimpl.NoopAuthorizer{},
		template("team", "z-last", "gpt"),
		template("team", "a-first", "claude"),
		template("other", "elsewhere", "gpt"),
	)

	result, err := service.List(ctx, "team")
	require.NoError(t, err)
	names := make([]string, 0, len(result))
	for _, item := range result {
		names = append(names, item.Name)
	}
	assert.Equal(t, []string{"a-first", "z-last"}, names)

	_, err = service.List(ctx, "")
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
}

func TestCRUD(t *testing.T) {
	service, ctx := newService(t, &authimpl.NoopAuthorizer{})

	created, err := service.Create(ctx, template("team", "researcher", "gpt"))
	require.NoError(t, err)
	assert.Equal(t, "gpt", created.Spec.ModelConfig.Name)

	_, err = service.Create(ctx, template("team", "researcher", "gpt"))
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists), err)

	ref := types.NamespacedName{Namespace: "team", Name: "researcher"}
	fetched, err := service.Get(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "gpt", fetched.Spec.ModelConfig.Name)

	updated, err := service.Update(ctx, ref, template("team", "researcher", "claude"))
	require.NoError(t, err)
	assert.Equal(t, "claude", updated.Spec.ModelConfig.Name)

	require.NoError(t, service.Delete(ctx, ref))
	_, err = service.Get(ctx, ref)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)
}

// Update writes the incoming spec onto the stored object; controller-owned
// status must survive, or every edit would blank the admitting-harness list.
func TestUpdatePreservesStatus(t *testing.T) {
	existing := template("team", "researcher", "gpt")
	existing.Status = v1alpha3.AgentTemplateStatus{
		Harnesses: []v1alpha3.AgentTemplateHarnessStatus{{Harness: "shared", DesiredRevision: "rev-1"}},
	}
	service, ctx := newService(t, &authimpl.NoopAuthorizer{}, existing)

	updated, err := service.Update(ctx,
		types.NamespacedName{Namespace: "team", Name: "researcher"},
		template("team", "researcher", "claude"))
	require.NoError(t, err)
	assert.Equal(t, "claude", updated.Spec.ModelConfig.Name)
	require.Len(t, updated.Status.Harnesses, 1)
	assert.Equal(t, "shared", updated.Status.Harnesses[0].Harness)
}

// Create must not let a caller seed controller-owned status by round-tripping
// a Get back into a Create.
func TestCreateDropsStatus(t *testing.T) {
	service, ctx := newService(t, &authimpl.NoopAuthorizer{})
	incoming := template("team", "researcher", "gpt")
	incoming.Status = v1alpha3.AgentTemplateStatus{
		Harnesses: []v1alpha3.AgentTemplateHarnessStatus{{Harness: "forged", DesiredRevision: "rev-1"}},
	}

	created, err := service.Create(ctx, incoming)
	require.NoError(t, err)
	assert.Empty(t, created.Status.Harnesses)
}

func TestInvalidArgumentsAndNotFound(t *testing.T) {
	missing := types.NamespacedName{Namespace: "team", Name: "absent"}
	tests := []struct {
		name string
		call func(*agenttemplate.Service, context.Context) error
		code serviceerrors.Code
	}{
		{
			name: "get without namespace",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Get(ctx, types.NamespacedName{Name: "researcher"})
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "get without name",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Get(ctx, types.NamespacedName{Namespace: "team"})
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create nil resource",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Create(ctx, nil)
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create with name the apiserver would reject",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Create(ctx, template("team", "Not A Subdomain", "gpt"))
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create with namespace the apiserver would reject",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Create(ctx, template("Team", "researcher", "gpt"))
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "update nil resource",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Update(ctx, missing, nil)
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "get missing template",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Get(ctx, missing)
				return err
			},
			code: serviceerrors.CodeNotFound,
		},
		{
			name: "update missing template",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				_, err := s.Update(ctx, missing, template("team", "absent", "gpt"))
				return err
			},
			code: serviceerrors.CodeNotFound,
		},
		{
			name: "delete missing template",
			call: func(s *agenttemplate.Service, ctx context.Context) error {
				return s.Delete(ctx, missing)
			},
			code: serviceerrors.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, ctx := newService(t, &authimpl.NoopAuthorizer{})
			err := tt.call(service, ctx)
			assert.True(t, serviceerrors.IsCode(err, tt.code), err)
		})
	}
}

func TestAuthorizationDenied(t *testing.T) {
	ref := types.NamespacedName{Namespace: "team", Name: "researcher"}
	tests := []struct {
		name string
		call func(*agenttemplate.Service, context.Context) error
	}{
		{"list", func(s *agenttemplate.Service, ctx context.Context) error {
			_, err := s.List(ctx, "team")
			return err
		}},
		{"get", func(s *agenttemplate.Service, ctx context.Context) error {
			_, err := s.Get(ctx, ref)
			return err
		}},
		{"create", func(s *agenttemplate.Service, ctx context.Context) error {
			_, err := s.Create(ctx, template("team", "researcher", "gpt"))
			return err
		}},
		{"update", func(s *agenttemplate.Service, ctx context.Context) error {
			_, err := s.Update(ctx, ref, template("team", "researcher", "gpt"))
			return err
		}},
		{"delete", func(s *agenttemplate.Service, ctx context.Context) error {
			return s.Delete(ctx, ref)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, ctx := newService(t, denyAuthorizer{}, template("team", "researcher", "gpt"))
			err := tt.call(service, ctx)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		})
	}
}

func TestUnauthenticated(t *testing.T) {
	service, _ := newService(t, &authimpl.NoopAuthorizer{})
	_, err := service.List(context.Background(), "team")
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), err)
}
