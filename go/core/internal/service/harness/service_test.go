package harness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/harness"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testImage = "example.test/runtime@sha256:0000000000000000000000000000000000000000000000000000000000000000"

type denyAuthorizer struct{}

func (denyAuthorizer) Check(context.Context, pkgauth.Principal, pkgauth.Verb, pkgauth.Resource) error {
	return errors.New("denied")
}

func fixture(namespace, name, workerPool string) *v1alpha3.Harness {
	return &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha3.HarnessSpec{
			Kagent:   &v1alpha3.KagentHarness{},
			Workload: v1alpha3.HarnessWorkload{Image: testImage},
			Substrate: v1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef:  corev1.LocalObjectReference{Name: workerPool},
				SnapshotPolicy: v1alpha3.HarnessSnapshotPolicy{Location: "s3://snapshots"},
			},
		},
	}
}

func newService(t *testing.T, authorizer pkgauth.Authorizer, objects ...ctrlclient.Object) (*harness.Service, context.Context) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	ctx := pkgauth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{
		P: pkgauth.Principal{User: pkgauth.User{ID: "harness-user"}},
	})
	return harness.NewService(kubeClient, authorizer), ctx
}

func TestList(t *testing.T) {
	service, ctx := newService(t, &authimpl.NoopAuthorizer{},
		fixture("team", "z-last", "pool-a"),
		fixture("team", "a-first", "pool-b"),
		fixture("other", "elsewhere", "pool-c"),
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

	created, err := service.Create(ctx, fixture("team", "shared", "pool-a"))
	require.NoError(t, err)
	assert.Equal(t, "pool-a", created.Spec.Substrate.WorkerPoolRef.Name)

	_, err = service.Create(ctx, fixture("team", "shared", "pool-a"))
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists), err)

	ref := types.NamespacedName{Namespace: "team", Name: "shared"}
	fetched, err := service.Get(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, testImage, fetched.Spec.Workload.Image)

	require.NoError(t, service.Delete(ctx, ref))
	_, err = service.Get(ctx, ref)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), err)
}

// The capability record in status is proven by the controller for a pinned
// adapter and image, so a create must not seed it.
func TestStatusIsControllerOwned(t *testing.T) {
	service, ctx := newService(t, &authimpl.NoopAuthorizer{})

	forged := fixture("team", "forged", "pool-a")
	forged.Status = v1alpha3.HarnessStatus{
		Capabilities: &v1alpha3.HarnessCapabilities{Version: "forged", Streaming: true},
	}
	created, err := service.Create(ctx, forged)
	require.NoError(t, err)
	assert.Nil(t, created.Status.Capabilities)
}

func TestInvalidArgumentsAndNotFound(t *testing.T) {
	missing := types.NamespacedName{Namespace: "team", Name: "absent"}
	tests := []struct {
		name string
		call func(*harness.Service, context.Context) error
		code serviceerrors.Code
	}{
		{
			name: "get without namespace",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Get(ctx, types.NamespacedName{Name: "shared"})
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "get without name",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Get(ctx, types.NamespacedName{Namespace: "team"})
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create nil resource",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Create(ctx, nil)
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create with name the apiserver would reject",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Create(ctx, fixture("team", "Not A Subdomain", "pool-a"))
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "create with namespace the apiserver would reject",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Create(ctx, fixture("Team", "shared", "pool-a"))
				return err
			},
			code: serviceerrors.CodeInvalidArgument,
		},
		{
			name: "get missing harness",
			call: func(s *harness.Service, ctx context.Context) error {
				_, err := s.Get(ctx, missing)
				return err
			},
			code: serviceerrors.CodeNotFound,
		},
		{
			name: "delete missing harness",
			call: func(s *harness.Service, ctx context.Context) error {
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
	ref := types.NamespacedName{Namespace: "team", Name: "shared"}
	tests := []struct {
		name string
		call func(*harness.Service, context.Context) error
	}{
		{"list", func(s *harness.Service, ctx context.Context) error {
			_, err := s.List(ctx, "team")
			return err
		}},
		{"get", func(s *harness.Service, ctx context.Context) error {
			_, err := s.Get(ctx, ref)
			return err
		}},
		{"create", func(s *harness.Service, ctx context.Context) error {
			_, err := s.Create(ctx, fixture("team", "shared", "pool-a"))
			return err
		}},
		{"delete", func(s *harness.Service, ctx context.Context) error {
			return s.Delete(ctx, ref)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, ctx := newService(t, denyAuthorizer{}, fixture("team", "shared", "pool-a"))
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
