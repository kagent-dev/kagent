package system_test

import (
	"context"
	"errors"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type systemDenyAuthorizer struct{}

func (systemDenyAuthorizer) Check(context.Context, pkgAuth.Principal, pkgAuth.Verb, pkgAuth.Resource) error {
	return errors.New("denied")
}

type fakeATEClient struct {
	templates []*ateapipb.ActorTemplate
	actors    []*ateapipb.Actor
	workers   []*ateapipb.Worker
	err       error
}

type fakeRuntimeRevisionStore struct {
	harnesses []dbpkg.ActorTemplateHarness
}

func (store *fakeRuntimeRevisionStore) ListActorTemplateHarnesses(context.Context) ([]dbpkg.ActorTemplateHarness, error) {
	return store.harnesses, nil
}

func (client *fakeATEClient) ListActors(context.Context, string) ([]*ateapipb.Actor, error) {
	if client.err != nil {
		return nil, client.err
	}
	return client.actors, nil
}

func (client *fakeATEClient) ListWorkers(context.Context) ([]*ateapipb.Worker, error) {
	return client.workers, client.err
}

func (client *fakeATEClient) ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error) {
	return client.templates, client.err
}

func TestCurrentUser(t *testing.T) {
	service := system.NewService()
	claims := map[string]any{"sub": "user-1", "groups": []any{"admins"}}
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{
		User:   pkgAuth.User{ID: "user-1"},
		Claims: claims,
	}})

	result, err := service.GetCurrentUser(ctx)
	require.NoError(t, err)
	assert.Equal(t, claims, result)

	ctx = pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{
		User: pkgAuth.User{ID: "fallback-user"},
	}})
	result, err = service.GetCurrentUser(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"sub": "fallback-user"}, result)

	_, err = service.GetCurrentUser(t.Context())
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), err)
}

func TestListNamespaces(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	t.Run("lists all and sorts case insensitively", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "Zoo"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
		).Build()
		service := system.NewService(system.WithInventory(kubeClient, nil, nil, nil))

		result, err := service.ListNamespaces(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []system.Namespace{
			{Name: "alpha", Status: "Terminating"},
			{Name: "Zoo", Status: "Active"},
		}, result)
	})

	t.Run("falls back to watched names when reads are forbidden", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectKey, ctrlclient.Object, ...ctrlclient.GetOption) error {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "", nil)
			},
		}).Build()
		service := system.NewService(system.WithInventory(kubeClient, []string{"team-b", "team-a"}, nil, nil))

		result, err := service.ListNamespaces(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []system.Namespace{{Name: "team-a"}, {Name: "team-b"}}, result)
	})
}

func TestGetSubstrateStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	t.Run("disabled does not read Kubernetes", func(t *testing.T) {
		service := system.NewService(system.WithInventory(nil, nil, &authimpl.NoopAuthorizer{}, nil))
		result, err := service.GetSubstrateStatus(ctx, "team")
		require.NoError(t, err)
		assert.False(t, result.Enabled)
		assert.Empty(t, result.WorkerPools)
	})

	t.Run("lists and filters typed inventory", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&atev1alpha1.WorkerPool{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pool"},
				Spec:       atev1alpha1.WorkerPoolSpec{Replicas: 2, WorkerImage: "ateom:test"},
			},
		).Build()
		ateClient := &fakeATEClient{
			templates: []*ateapipb.ActorTemplate{{
				Metadata:      &ateapipb.ResourceMetadata{Atespace: "team", Name: "template", Uid: "template-uid"},
				SandboxConfig: &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR},
				Status: &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
					GoldenSnapshot: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
				}},
			}},
			actors: []*ateapipb.Actor{{
				Metadata:      &ateapipb.ResourceMetadata{Name: "actor-1"},
				ActorTemplate: &ateapipb.ObjectRef{Atespace: "team", Name: "template"},
				Status: &ateapipb.ActorStatus{
					State: ateapipb.ActorState_ACTOR_STATE_RUNNING,
				},
			}},
			workers: []*ateapipb.Worker{{
				Metadata:        &ateapipb.ResourceMetadata{Version: 3},
				WorkerNamespace: "team",
				WorkerPool:      "pool",
				WorkerPod:       "worker-0",
				Status: &ateapipb.WorkerStatus{Assignment: &ateapipb.ActorAssignment{
					ActorTemplateRef: &ateapipb.ObjectRef{Atespace: "team", Name: "template"},
					Actor:            &ateapipb.ObjectRef{Name: "actor-1"},
				}},
			}},
		}
		revisions := &fakeRuntimeRevisionStore{harnesses: []dbpkg.ActorTemplateHarness{{
			Atespace: "team", Name: "template", UID: "template-uid", HarnessName: "kagent",
		}}}
		service := system.NewService(
			system.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient),
			system.WithRuntimeRevisions(revisions),
		)

		result, err := service.GetSubstrateStatus(ctx, "team")
		require.NoError(t, err)
		assert.True(t, result.Enabled)
		require.Len(t, result.WorkerPools, 1)
		assert.Equal(t, int32(2), result.WorkerPools[0].Replicas)
		require.Len(t, result.ActorTemplates, 1)
		assert.Equal(t, "Ready", result.ActorTemplates[0].Phase)
		assert.Equal(t, "template-uid", result.ActorTemplates[0].GoldenActorID)
		assert.Equal(t, "golden", result.ActorTemplates[0].GoldenSnapshot)
		assert.Equal(t, "gvisor", result.ActorTemplates[0].SandboxClass)
		assert.Equal(t, "kagent", result.ActorTemplates[0].HarnessName)
		assert.True(t, result.ActorTemplates[0].ManagedByKagent)
		require.Len(t, result.Actors, 1)
		assert.Equal(t, "Running", result.Actors[0].Status)
		require.Len(t, result.Workers, 1)
		assert.Equal(t, "template", result.Workers[0].ActorTemplate)
		assert.Equal(t, "actor-1", result.Workers[0].ActorID)
		assert.Equal(t, int64(3), result.Workers[0].Version)
	})

	t.Run("validates and authorizes", func(t *testing.T) {
		service := system.NewService(system.WithInventory(nil, nil, &authimpl.NoopAuthorizer{}, nil))
		_, err := service.GetSubstrateStatus(ctx, "INVALID_NAMESPACE")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		service = system.NewService(system.WithInventory(nil, nil, systemDenyAuthorizer{}, nil))
		_, err = service.GetSubstrateStatus(ctx, "")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}
