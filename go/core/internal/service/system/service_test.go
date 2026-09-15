package system_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
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
	// The number of rows a page answers with, whatever the caller asked for. Zero
	// means "everything in one page". ate-api may answer with fewer rows than asked
	// for, so a fake that always fills the request would hide a caller that stops
	// following the token as soon as it has enough rows.
	pageSize int
	// How many page reads each list has taken, so a test can assert that the token
	// was followed rather than that the rows came back.
	actorReads  int
	workerReads int
}

func (client *fakeATEClient) ListActorTemplates(_ context.Context, atespace string) ([]*ateapipb.ActorTemplate, error) {
	templates := []*ateapipb.ActorTemplate{}
	for _, template := range client.templates {
		if atespace == "" || template.GetMetadata().GetAtespace() == atespace {
			templates = append(templates, template)
		}
	}
	return templates, client.err
}

func (client *fakeATEClient) ListActorsPage(_ context.Context, atespace string, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
	client.actorReads++
	if err := client.readError(client.actorReads); err != nil {
		return nil, "", err
	}
	return fakePage(actorsInAtespace(client.actors, atespace), client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) ListWorkersPage(_ context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error) {
	client.workerReads++
	if err := client.readError(client.workerReads); err != nil {
		return nil, "", err
	}
	return fakePage(client.workers, client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) readError(int) error {
	return client.err
}

// fakePage slices rows the way ate-api pages them: an opaque token, empty on the last
// page, and never more rows than its own ceiling however many were asked for.
func fakePage[T any](rows []T, ceiling int, requested int32, pageToken string) ([]T, string, error) {
	pageSize := ceiling
	if requested > 0 && (ceiling <= 0 || int(requested) < ceiling) {
		pageSize = int(requested)
	}
	start := 0
	if pageToken != "" {
		parsed, err := strconv.Atoi(strings.TrimPrefix(pageToken, "upstream:"))
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token %q", pageToken)
		}
		start = parsed
	}
	if start > len(rows) {
		start = len(rows)
	}
	if pageSize <= 0 {
		return rows[start:], "", nil
	}
	end := min(start+pageSize, len(rows))
	next := ""
	if end < len(rows) {
		next = "upstream:" + strconv.Itoa(end)
	}
	return rows[start:end], next, nil
}

func TestCurrentUser(t *testing.T) {
	service := system.NewService(nil, nil, nil, nil)
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
		service := system.NewService(kubeClient, nil, nil, nil)

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
		service := system.NewService(kubeClient, []string{"team-b", "team-a"}, nil, nil)

		result, err := service.ListNamespaces(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []system.Namespace{{Name: "team-a"}, {Name: "team-b"}}, result)
	})
}

// substrateActor builds an ate-api actor placed on a worker pod, or on none when
// workerPod is empty.
func substrateActor(name, atespace string, state ateapipb.ActorState, workerNamespace, workerPod string) *ateapipb.Actor {
	status := &ateapipb.ActorStatus{State: state}
	if workerPod != "" {
		status.WorkerAssignment = &ateapipb.WorkerAssignment{
			WorkerNamespace: workerNamespace,
			WorkerPod:       workerPod,
		}
	}
	return &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: name},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: atespace, Name: "template"},
		Status:        status,
	}
}

func TestListSubstrateActors(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	newService := func(client system.ATEClient) *system.Service {
		return system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, client)
	}

	t.Run("answers with one page and the token for the next", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_PAUSED, "", ""),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Page: &apiv1alpha1.PageRequest{Limit: 2}})
		require.NoError(t, err)
		require.Len(t, page.Actors, 2)
		// Preserve upstream order, including across different actor states.
		assert.Equal(t, []string{"actor-1", "actor-2"}, []string{page.Actors[0].GetMetadata().GetName(), page.Actors[1].GetMetadata().GetName()})
		assert.NotEmpty(t, page.NextPageToken)
		assert.Equal(t, 1, ateClient.actorReads, "one request must read only one upstream page")
		assert.False(t, page.ComputedAt.IsZero())
	})

	t.Run("continues from the token it was given", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}
		service := newService(ateClient)

		first, err := service.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Page: &apiv1alpha1.PageRequest{Limit: 2}})
		require.NoError(t, err)
		second, err := service.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{
			Page: &apiv1alpha1.PageRequest{Limit: 2, PageToken: first.NextPageToken},
		})
		require.NoError(t, err)
		require.Len(t, second.Actors, 1)
		assert.Equal(t, "actor-3", second.Actors[0].GetMetadata().GetName())
		assert.Empty(t, second.NextPageToken, "the last page offers nowhere to go")
		// The two pages together are the whole result, in order and without repeats.
		assert.Equal(t, []string{"actor-1", "actor-2", "actor-3"}, []string{
			first.Actors[0].GetMetadata().GetName(), first.Actors[1].GetMetadata().GetName(), second.Actors[0].GetMetadata().GetName(),
		})
	})

	t.Run("passes atespace filtering upstream", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 1, actors: []*ateapipb.Actor{
			substrateActor("other-1", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-0"),
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Atespace: "team", Page: &apiv1alpha1.PageRequest{Limit: 10}})
		require.NoError(t, err)
		require.Len(t, page.Actors, 1)
		assert.Equal(t, "actor-1", page.Actors[0].GetMetadata().GetName())
	})

	t.Run("an ate-api failure is an empty page beside a warning", func(t *testing.T) {
		ateClient := &fakeATEClient{err: errors.New("ate-api unreachable")}

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{})
		require.NoError(t, err)
		assert.Empty(t, page.Actors)
		assert.Equal(t, "ate-api unreachable", page.ATEAPIError)
		// A failed page offers no continuation.
		assert.Empty(t, page.NextPageToken)
	})

	t.Run("authorizes", func(t *testing.T) {
		denied := system.NewService(nil, nil, systemDenyAuthorizer{}, &fakeATEClient{})
		_, err := denied.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}

func TestListSubstrateWorkers(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	ateClient := &fakeATEClient{pageSize: 2, workers: []*ateapipb.Worker{
		{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}},
		{WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-1"},
		{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-2"},
	}}
	service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, ateClient)

	page, err := service.ListSubstrateWorkers(ctx, &apiv1alpha1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1alpha1.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, page.Workers, 1)
	assert.Equal(t, "worker-0", page.Workers[0].WorkerPod)
	assert.Equal(t, "upstream:2", page.NextPageToken)
	assert.Equal(t, 1, ateClient.workerReads)
	next, err := service.ListSubstrateWorkers(ctx, &apiv1alpha1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1alpha1.PageRequest{Limit: 2, PageToken: page.NextPageToken}})
	require.NoError(t, err)
	require.Len(t, next.Workers, 1)
	assert.Equal(t, "worker-2", next.Workers[0].WorkerPod)
	assert.Empty(t, next.NextPageToken)
	assert.Equal(t, 2, ateClient.workerReads)
}

func TestGetSubstrateSummary(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1alpha1.WorkerPool{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pool"},
			Spec:       atev1alpha1.WorkerPoolSpec{Replicas: 2, WorkerImage: "ateom:test"},
		},
	).Build()

	t.Run("counts across every page without materialising the inventory", func(t *testing.T) {
		ateClient := &fakeATEClient{
			// One row per page, so a summary that reads a single page is visible as a
			// count of one rather than as a passing test.
			pageSize: 1,
			templates: []*ateapipb.ActorTemplate{{
				Metadata:      &ateapipb.ResourceMetadata{Atespace: "team", Name: "template", Uid: "template-uid"},
				SandboxConfig: &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR},
				Containers:    []*ateapipb.Container{{Env: []*ateapipb.EnvVar{{Name: "API_KEY", Value: "secret"}}}},
				Status: &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
					GoldenSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/golden"},
				}},
			}},
			actors: []*ateapipb.Actor{
				substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
				substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
				substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_PAUSED, "", ""),
				substrateActor("actor-4", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-9"),
			},
			workers: []*ateapipb.Worker{
				{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}},
				{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-1"},
				{WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-9"},
			},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Empty(t, result.ATEAPIError)
		assert.Equal(t, int64(3), result.ActorCount)
		assert.Equal(t, int64(2), result.RunningActorCount)
		assert.Equal(t, int64(2), result.WorkerCount)
		// Two actors share worker-0, so one worker is busy rather than two: the count
		// is of workers, not of placements.
		assert.Equal(t, int64(1), result.BusyWorkerCount)
		assert.Equal(t, []system.SubstrateActorStatusCount{
			{State: ateapipb.ActorState_ACTOR_STATE_RUNNING, Count: 2},
			{State: ateapipb.ActorState_ACTOR_STATE_PAUSED, Count: 1},
		}, result.ActorStatusCounts)
		require.Len(t, result.WorkerPools, 1)
		require.Len(t, result.ActorTemplates, 1)
		template := result.ActorTemplates[0]
		assert.Equal(t, "template-uid", template.GetMetadata().GetUid())
		assert.Equal(t, "s3://snapshots/golden", template.GetStatus().GetGoldenSnapshotStatus().GetGoldenSnapshot().GetSnapshotUri())
		assert.Equal(t, ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, template.GetSandboxConfig().GetSandboxClass())
		assert.Empty(t, template.GetContainers())
		require.Len(t, ateClient.templates[0].GetContainers(), 1)
		assert.Equal(t, "secret", ateClient.templates[0].GetContainers()[0].GetEnv()[0].GetValue())
		assert.False(t, result.ComputedAt.IsZero())
	})

	t.Run("an ate-api failure leaves the Kubernetes halves complete", func(t *testing.T) {
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, &fakeATEClient{err: errors.New("ate-api unreachable")})

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api unreachable", result.ATEAPIError)
		assert.Zero(t, result.ActorCount)
		require.Len(t, result.WorkerPools, 1)
	})
}

// The summary's three ate-api reads are independent, and a database failure is not one
// of them: one failed read must not zero the other counts or be reported as ate-api's.
func TestGetSubstrateSummaryReadsAreIndependent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	actors := []*ateapipb.Actor{
		substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
		substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_PAUSED, "", ""),
	}
	workers := []*ateapipb.Worker{{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}}}

	t.Run("a failed template listing still counts the actors and the workers", func(t *testing.T) {
		// The template listing is the first ate-api read, so failing from read one
		// fails it and leaves the two walks to answer.
		ateClient := &failingTemplatesATEClient{
			fakeATEClient: fakeATEClient{actors: actors, workers: workers},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "templates unavailable", result.ATEAPIError)
		assert.Empty(t, result.ActorTemplates)
		// The counts the tiles read. Zero here is the page reporting an empty cluster.
		assert.Equal(t, int64(2), result.ActorCount)
		assert.Equal(t, int64(1), result.RunningActorCount)
		assert.Equal(t, int64(1), result.WorkerCount)
		assert.Equal(t, int64(1), result.BusyWorkerCount)
	})

	t.Run("a failed actor walk still counts busy workers", func(t *testing.T) {
		ateClient := &failingActorsATEClient{fakeATEClient: fakeATEClient{workers: workers}}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)
		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "actors unavailable", result.ATEAPIError)
		assert.Zero(t, result.ActorCount)
		assert.Equal(t, int64(1), result.WorkerCount)
		assert.Equal(t, int64(1), result.BusyWorkerCount)
	})

	t.Run("a failed worker walk cannot leave more workers busy than there are", func(t *testing.T) {
		ateClient := &failingWorkersATEClient{
			fakeATEClient: fakeATEClient{
				actors: []*ateapipb.Actor{
					substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
					substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
				},
			},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "workers unavailable", result.ATEAPIError)
		assert.Equal(t, int64(2), result.ActorCount)
		assert.Equal(t, int64(0), result.WorkerCount)
		assert.LessOrEqual(t, result.BusyWorkerCount, result.WorkerCount)
	})

	t.Run("busy workers are counted on the same footing as the workers themselves", func(t *testing.T) {
		/*
		 * An actor's scope is its own atespace; a worker's is its pod's
		 * Kubernetes namespace, and the two need not agree. Counting the actor here and
		 * not the pod it sits on renders the tile as "1/0" — more workers busy than
		 * exist.
		 */
		ateClient := &fakeATEClient{
			actors: []*ateapipb.Actor{
				substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "kagent", "worker-0"),
			},
			workers: []*ateapipb.Worker{
				{WorkerNamespace: "kagent", WorkerPool: "pool", WorkerPod: "worker-0"},
			},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.ActorCount)
		assert.Equal(t, int64(0), result.WorkerCount)
		assert.Equal(t, int64(0), result.BusyWorkerCount, "the pod is out of scope, so it is not one of this scope's busy workers")
	})
}

// failingWorkersATEClient answers every read but the worker walk, so the actors can be
// counted while the pods they sit on cannot.
type failingWorkersATEClient struct {
	fakeATEClient
}

func (client *failingWorkersATEClient) ListWorkersPage(context.Context, int32, string) ([]*ateapipb.Worker, string, error) {
	return nil, "", errors.New("workers unavailable")
}

// failingTemplatesATEClient answers every read but the template listing.
type failingTemplatesATEClient struct {
	fakeATEClient
}

func (client *failingTemplatesATEClient) ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error) {
	return nil, errors.New("templates unavailable")
}

// The point of the exercise: a row that sorts first arrives on page one however late
// ate-api mentioned it, and a match nine pages deep is still found.

// Workers get the same treatment, ordered by the pool they belong to by default.

func TestBusyWorkerWithOutOfScopeActor(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	ateClient := &fakeATEClient{
		actors: []*ateapipb.Actor{
			substrateActor("a", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "kagent", "worker-0"),
		},
		workers: []*ateapipb.Worker{
			{WorkerNamespace: "kagent", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 1}}},
		},
	}
	service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient)
	summary, err := service.GetSubstrateSummary(ctx, "kagent", "kagent")
	require.NoError(t, err)
	require.Empty(t, summary.ATEAPIError)
	require.Equal(t, int64(0), summary.ActorCount)
	require.Equal(t, int64(1), summary.WorkerCount)
	assert.Equal(t, int64(1), summary.BusyWorkerCount, "worker-0 is assigned even though its actor's template is in team")
}

type failingActorsATEClient struct{ fakeATEClient }

func (client *failingActorsATEClient) ListActorsPage(context.Context, string, int32, string) ([]*ateapipb.Actor, string, error) {
	return nil, "", errors.New("actors unavailable")
}

func actorsInAtespace(actors []*ateapipb.Actor, atespace string) []*ateapipb.Actor {
	result := []*ateapipb.Actor{}
	for _, actor := range actors {
		if atespace == "" || actor.GetMetadata().GetAtespace() == atespace {
			result = append(result, actor)
		}
	}
	return result
}

func TestSubstrateScopesAreIndependent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "workers", Name: "pool"}},
	).Build()
	actor := substrateActor("same-name", "team-a", ateapipb.ActorState_ACTOR_STATE_RUNNING, "workers", "pod")
	actor.ActorTemplate.Atespace = "shared"
	ateClient := &fakeATEClient{
		pageSize: 1,
		actors:   []*ateapipb.Actor{actor, substrateActor("same-name", "shared", ateapipb.ActorState_ACTOR_STATE_PAUSED, "", "")},
		templates: []*ateapipb.ActorTemplate{
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "shared", Name: "template"}},
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "own-template"}},
		},
		workers: []*ateapipb.Worker{{WorkerNamespace: "workers", WorkerPod: "pod"}},
	}
	service := system.NewService(kubeClient, []string{"workers"}, &authimpl.NoopAuthorizer{}, ateClient)
	for _, tc := range []struct {
		atespace string
		count    int64
		running  int64
	}{
		{atespace: "", count: 2, running: 1},
		{atespace: "team-a", count: 1, running: 1},
		{atespace: "shared", count: 1, running: 0},
		{atespace: "missing", count: 0, running: 0},
	} {
		t.Run(tc.atespace, func(t *testing.T) {
			summary, err := service.GetSubstrateSummary(ctx, "", tc.atespace)
			require.NoError(t, err)
			assert.Equal(t, tc.count, summary.ActorCount)
			assert.Equal(t, tc.running, summary.RunningActorCount)
			assert.Len(t, summary.ActorTemplates, int(tc.count))
			var counted int64
			for _, bucket := range summary.ActorStatusCounts {
				counted += bucket.Count
			}
			assert.Equal(t, tc.count, counted)
			assert.Equal(t, int64(1), summary.WorkerCount)
			require.Len(t, summary.WorkerPools, 1)
			for _, template := range summary.ActorTemplates {
				if tc.atespace != "" {
					assert.Equal(t, tc.atespace, template.GetMetadata().GetAtespace())
				}
			}
			page, err := service.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Atespace: tc.atespace, Page: &apiv1alpha1.PageRequest{Limit: 1}})
			require.NoError(t, err)
			for _, row := range page.Actors {
				if tc.atespace != "" {
					assert.Equal(t, tc.atespace, row.GetMetadata().GetAtespace())
				}
			}
		})
	}
	page, err := service.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Atespace: "team-a"})
	require.NoError(t, err)
	require.Len(t, page.Actors, 1)
	assert.Same(t, actor, page.Actors[0])
}

type emptyPageATEClient struct {
	fakeATEClient
	t *testing.T
}

func (c *emptyPageATEClient) ListActorsPage(_ context.Context, atespace string, size int32, token string) ([]*ateapipb.Actor, string, error) {
	assert.Equal(c.t, "team", atespace)
	assert.Equal(c.t, int32(7), size)
	assert.Equal(c.t, "upstream:opaque/cursor=", token)
	return nil, "upstream:next/cursor=", nil
}

func (c *emptyPageATEClient) ListWorkersPage(_ context.Context, size int32, token string) ([]*ateapipb.Worker, string, error) {
	assert.Equal(c.t, int32(7), size)
	assert.Equal(c.t, "upstream:opaque/cursor=", token)
	return []*ateapipb.Worker{{WorkerNamespace: "other"}}, "upstream:next/cursor=", nil
}

func TestSubstrateEmptyPagesPreserveUpstreamTokens(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	service := system.NewService(nil, []string{"team"}, &authimpl.NoopAuthorizer{}, &emptyPageATEClient{t: t})
	page := &apiv1alpha1.PageRequest{Limit: 7, PageToken: "upstream:opaque/cursor="}
	actors, err := service.ListSubstrateActors(ctx, &apiv1alpha1.ListSubstrateActorsRequest{Atespace: "team", Page: page})
	require.NoError(t, err)
	assert.Empty(t, actors.Actors)
	assert.Empty(t, actors.ATEAPIError)
	assert.Equal(t, "upstream:next/cursor=", actors.NextPageToken)
	workers, err := service.ListSubstrateWorkers(ctx, &apiv1alpha1.ListSubstrateWorkersRequest{Page: page})
	require.NoError(t, err)
	assert.Empty(t, workers.Workers)
	assert.Empty(t, workers.ATEAPIError)
	assert.Equal(t, "upstream:next/cursor=", workers.NextPageToken)
}
