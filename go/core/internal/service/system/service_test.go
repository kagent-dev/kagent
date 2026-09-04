package system_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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
	// Fail from this page read onwards, counting from one. Zero never fails. A
	// mid-walk failure is the case worth covering: ate-api going down between pages
	// is ordinary, and it is what decides whether a caller loses the rest of a list.
	failFromRead int
	// The page sizes asked for, so a caller that asks for a whole page when it only
	// needs the rest of one is visible.
	actorPageSizes []int32
}

type fakeRuntimeRevisionStore struct {
	harnesses []database.ActorTemplateHarness
	err       error
}

func (store *fakeRuntimeRevisionStore) ListActorTemplateHarnesses(context.Context) ([]database.ActorTemplateHarness, error) {
	return store.harnesses, store.err
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

func (client *fakeATEClient) ListActorsPage(_ context.Context, _ string, pageSize int32, pageToken string) ([]*ateapipb.Actor, string, error) {
	client.actorReads++
	client.actorPageSizes = append(client.actorPageSizes, pageSize)
	if err := client.readError(client.actorReads); err != nil {
		return nil, "", err
	}
	return fakePage(client.actors, client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) ListWorkersPage(_ context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error) {
	client.workerReads++
	if err := client.readError(client.workerReads); err != nil {
		return nil, "", err
	}
	return fakePage(client.workers, client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) readError(read int) error {
	if client.err != nil && (client.failFromRead == 0 || read >= client.failFromRead) {
		return client.err
	}
	return nil
}

/*
fakePage slices rows the way ate-api pages them: an opaque token, and an empty one on
the last page. The token here is an offset, which ate-api's is not — nothing under test
reads it, which is the property that matters.

It answers with the smaller of what was asked for and its own ceiling, as ate-api does:
a fake that always filled the request would hide a caller asking for a whole page when
it needs only the rest of one.
*/
func fakePage[T any](rows []T, ceiling int, requested int32, pageToken string) ([]T, string, error) {
	pageSize := ceiling
	if requested > 0 && (ceiling <= 0 || int(requested) < ceiling) {
		pageSize = int(requested)
	}
	start := 0
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
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
		next = strconv.Itoa(end)
	}
	return rows[start:end], next, nil
}

func TestCurrentUser(t *testing.T) {
	service := system.NewService(nil, nil, nil, nil, nil)
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
		service := system.NewService(kubeClient, nil, nil, nil, nil)

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
		service := system.NewService(kubeClient, []string{"team-b", "team-a"}, nil, nil, nil)

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
		service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, nil, nil)
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
					GoldenSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/golden"},
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
			}},
		}
		revisions := &fakeRuntimeRevisionStore{harnesses: []database.ActorTemplateHarness{{
			Atespace: "team", Name: "template", UID: "template-uid", HarnessName: "kagent",
		}}}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient, revisions)

		result, err := service.GetSubstrateStatus(ctx, "team")
		require.NoError(t, err)
		assert.True(t, result.Enabled)
		require.Len(t, result.WorkerPools, 1)
		assert.Equal(t, int32(2), result.WorkerPools[0].Replicas)
		require.Len(t, result.ActorTemplates, 1)
		assert.Equal(t, "Ready", result.ActorTemplates[0].Phase)
		assert.Equal(t, "template-uid", result.ActorTemplates[0].GoldenActorID)
		assert.Equal(t, "s3://snapshots/golden", result.ActorTemplates[0].GoldenSnapshot)
		assert.Equal(t, "gvisor", result.ActorTemplates[0].SandboxClass)
		assert.Equal(t, "kagent", result.ActorTemplates[0].HarnessName)
		assert.True(t, result.ActorTemplates[0].ManagedByKagent)
		require.Len(t, result.Actors, 1)
		assert.Equal(t, "Running", result.Actors[0].Status)
		require.Len(t, result.Workers, 1)
		assert.Equal(t, "worker-0", result.Workers[0].WorkerPod)
		assert.Equal(t, int64(3), result.Workers[0].Version)
	})

	t.Run("validates and authorizes", func(t *testing.T) {
		service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, nil, nil)
		_, err := service.GetSubstrateStatus(ctx, "INVALID_NAMESPACE")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		service = system.NewService(nil, nil, systemDenyAuthorizer{}, nil, nil)
		_, err = service.GetSubstrateStatus(ctx, "")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
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
		Metadata:      &ateapipb.ResourceMetadata{Name: name},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: atespace, Name: "template"},
		Status:        status,
	}
}

func TestListSubstrateActors(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	// Takes the interface rather than the fake, so that "disabled" below passes an
	// untyped nil: a typed nil pointer in an interface is not nil, and the test would
	// be asserting against a fake it had accidentally kept.
	newService := func(client system.ATEClient) *system.Service {
		return system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, client, &fakeRuntimeRevisionStore{})
	}

	t.Run("answers with one page and the token for the next", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_PAUSED, "", ""),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, system.SubstrateListInput{PageSize: 2})
		require.NoError(t, err)
		assert.True(t, page.Enabled)
		require.Len(t, page.Actors, 2)
		assert.Equal(t, []string{"actor-1", "actor-2"}, []string{page.Actors[0].ActorID, page.Actors[1].ActorID})
		assert.NotEmpty(t, page.NextPageToken)
		// One page asked for is one page read: filling a page must not walk the
		// inventory, which is the whole point of the call.
		assert.Equal(t, 1, ateClient.actorReads)
		assert.False(t, page.ComputedAt.IsZero())
	})

	t.Run("follows the token it is given", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, system.SubstrateListInput{PageSize: 2, PageToken: "2"})
		require.NoError(t, err)
		require.Len(t, page.Actors, 1)
		assert.Equal(t, "actor-3", page.Actors[0].ActorID)
		assert.Empty(t, page.NextPageToken)
	})

	t.Run("drops rows outside the scope and keeps reading to fill the page", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 1, actors: []*ateapipb.Actor{
			substrateActor("other-1", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-0"),
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, system.SubstrateListInput{Namespace: "team", PageSize: 1})
		require.NoError(t, err)
		require.Len(t, page.Actors, 1)
		assert.Equal(t, "actor-1", page.Actors[0].ActorID)
		// A page of one that ate-api answered with a row from another namespace would
		// otherwise render as "no actors" on a cluster that has them.
		assert.Equal(t, 2, ateClient.actorReads)
	})

	t.Run("an ate-api failure is an empty page beside a warning", func(t *testing.T) {
		ateClient := &fakeATEClient{err: errors.New("ate-api unreachable")}

		page, err := newService(ateClient).ListSubstrateActors(ctx, system.SubstrateListInput{})
		require.NoError(t, err)
		assert.Empty(t, page.Actors)
		assert.Equal(t, "ate-api unreachable", page.ATEAPIError)
		// The token comes back as it went in, so the caller retries this page rather
		// than skipping it.
		assert.Empty(t, page.NextPageToken)
	})

	t.Run("disabled substrate is an empty page, not an error", func(t *testing.T) {
		page, err := newService(nil).ListSubstrateActors(ctx, system.SubstrateListInput{})
		require.NoError(t, err)
		assert.False(t, page.Enabled)
		assert.Empty(t, page.Actors)
	})

	t.Run("refuses a page size above the maximum rather than clamping it", func(t *testing.T) {
		_, err := newService(&fakeATEClient{}).ListSubstrateActors(ctx, system.SubstrateListInput{PageSize: 101})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
	})

	t.Run("validates and authorizes", func(t *testing.T) {
		_, err := newService(&fakeATEClient{}).ListSubstrateActors(ctx, system.SubstrateListInput{Namespace: "INVALID_NAMESPACE"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		denied := system.NewService(nil, nil, systemDenyAuthorizer{}, &fakeATEClient{}, nil)
		_, err = denied.ListSubstrateActors(ctx, system.SubstrateListInput{})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}

func TestListSubstrateWorkers(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})

	ateClient := &fakeATEClient{pageSize: 2, workers: []*ateapipb.Worker{
		{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0"},
		{WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-1"},
		{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-2"},
	}}
	service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{})

	page, err := service.ListSubstrateWorkers(ctx, system.SubstrateListInput{Namespace: "team", PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page.Workers, 2)
	assert.Equal(t, []string{"worker-0", "worker-2"}, []string{page.Workers[0].WorkerPod, page.Workers[1].WorkerPod})
	assert.Empty(t, page.NextPageToken)
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
				Metadata: &ateapipb.ResourceMetadata{Atespace: "team", Name: "template", Uid: "template-uid"},
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
				{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0"},
				{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-1"},
				{WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-9"},
			},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{harnesses: []database.ActorTemplateHarness{{
			Atespace: "team", Name: "template", UID: "template-uid", HarnessName: "kagent",
		}}})

		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)
		assert.True(t, result.Enabled)
		assert.Empty(t, result.ATEAPIError)
		assert.Equal(t, int64(3), result.ActorCount)
		assert.Equal(t, int64(2), result.RunningActorCount)
		assert.Equal(t, int64(2), result.WorkerCount)
		// Two actors share worker-0, so one worker is busy rather than two: the count
		// is of workers, not of placements.
		assert.Equal(t, int64(1), result.BusyWorkerCount)
		assert.Equal(t, []system.SubstrateActorStatusCount{
			{Status: "Paused", Count: 1},
			{Status: "Running", Count: 2},
		}, result.ActorStatusCounts)
		require.Len(t, result.WorkerPools, 1)
		require.Len(t, result.ActorTemplates, 1)
		assert.Equal(t, "kagent", result.ActorTemplates[0].HarnessName)
		assert.False(t, result.ComputedAt.IsZero())
	})

	t.Run("an ate-api failure leaves the Kubernetes halves complete", func(t *testing.T) {
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, &fakeATEClient{err: errors.New("ate-api unreachable")}, &fakeRuntimeRevisionStore{})

		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api unreachable", result.ATEAPIError)
		assert.Zero(t, result.ActorCount)
		require.Len(t, result.WorkerPools, 1)
	})

	t.Run("disabled substrate does not read Kubernetes", func(t *testing.T) {
		service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, nil, nil)
		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)
		assert.False(t, result.Enabled)
		assert.Empty(t, result.WorkerPools)
	})
}

// A read that dies between pages must not look like the end of the list: the rows
// already collected are kept, and the token names the page that failed so a retry
// resumes there instead of losing everything behind it.
func TestListSubstrateActorsKeepsRowsWhenAPageFailsMidway(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	ateClient := &fakeATEClient{
		pageSize:     1,
		err:          errors.New("ate-api unreachable"),
		failFromRead: 2,
		actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("other-1", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-1"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-2"),
		},
	}
	service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{})

	page, err := service.ListSubstrateActors(ctx, system.SubstrateListInput{Namespace: "team", PageSize: 10})
	require.NoError(t, err)
	require.Len(t, page.Actors, 1)
	assert.Equal(t, "actor-1", page.Actors[0].ActorID)
	assert.Equal(t, "ate-api unreachable", page.ATEAPIError)
	// The token of the page that failed. Empty here would hide "actor-2" behind a
	// missing Next button, with the summary beside it still counting three actors.
	assert.Equal(t, "1", page.NextPageToken)
}

// With nothing collected there is no page to continue after: the token names the page
// the caller already asked for, and offering it as "next" is a control that goes nowhere.
func TestListSubstrateActorsOffersNoNextPageWhenTheFirstReadFails(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	ateClient := &fakeATEClient{pageSize: 1, err: errors.New("ate-api unreachable")}
	service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{})

	page, err := service.ListSubstrateActors(ctx, system.SubstrateListInput{PageToken: "2", PageSize: 10})
	require.NoError(t, err)
	assert.Empty(t, page.Actors)
	assert.Empty(t, page.NextPageToken)
}

// A page must not come back larger than it was asked for: a caller sizes a table or a
// buffer by the number it sent, and the proto caps it at 100.
func TestListSubstrateActorsNeverOverfillsAPage(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
	actors := []*ateapipb.Actor{
		substrateActor("other-1", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-0"),
	}
	for index := range 6 {
		actors = append(actors, substrateActor(
			fmt.Sprintf("actor-%d", index), "team",
			ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0",
		))
	}
	// Three rows a page: the first page contributes one in-scope row, so a second read
	// asking for the full three again would return four rows for a page of three.
	ateClient := &fakeATEClient{pageSize: 3, actors: actors}
	service := system.NewService(nil, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{})

	page, err := service.ListSubstrateActors(ctx, system.SubstrateListInput{Namespace: "team", PageSize: 3})
	require.NoError(t, err)
	assert.Len(t, page.Actors, 3)
	// Each read asks only for what is still missing.
	assert.Equal(t, []int32{3, 1}, ateClient.actorPageSizes)
}

/*
The summary's three ate-api reads are independent, and a database failure is not one
of them.

Both halves of this were wrong together: the harnesses were read inside the template
listing, so a PostgreSQL outage reached the reader as "ate-api answered with an error";
and every count was gated on that same field, so one failed read reported a cluster of
410,110 actors as running none.
*/
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
	workers := []*ateapipb.Worker{{WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0"}}

	t.Run("a failed template listing still counts the actors and the workers", func(t *testing.T) {
		// The template listing is the first ate-api read, so failing from read one
		// fails it and leaves the two walks to answer.
		ateClient := &failingTemplatesATEClient{
			fakeATEClient: fakeATEClient{actors: actors, workers: workers},
		}
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient, &fakeRuntimeRevisionStore{})

		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)
		assert.Equal(t, "templates unavailable", result.ATEAPIError)
		assert.Empty(t, result.ActorTemplates)
		// The counts the tiles read. Zero here is the page reporting an empty cluster.
		assert.Equal(t, int64(2), result.ActorCount)
		assert.Equal(t, int64(1), result.RunningActorCount)
		assert.Equal(t, int64(1), result.WorkerCount)
		assert.Equal(t, int64(1), result.BusyWorkerCount)
	})

	t.Run("a database failure is an internal error, not a warning about ate-api", func(t *testing.T) {
		service := system.NewService(kubeClient, nil, &authimpl.NoopAuthorizer{}, &fakeATEClient{actors: actors, workers: workers}, &fakeRuntimeRevisionStore{err: errors.New("connection refused")})

		_, err := service.GetSubstrateSummary(ctx, "team")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal), err)
	})
}

// failingTemplatesATEClient answers every read but the template listing.
type failingTemplatesATEClient struct {
	fakeATEClient
}

func (client *failingTemplatesATEClient) ListActorTemplates(context.Context, string) ([]*ateapipb.ActorTemplate, error) {
	return nil, errors.New("templates unavailable")
}
