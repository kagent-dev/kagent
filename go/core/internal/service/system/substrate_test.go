package system_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// substrateScheme is the scheme the substrate CRs are registered against, shared
// by every test below.
func substrateScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	return scheme
}

func substrateContext(t *testing.T) context.Context {
	t.Helper()
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
}

// actorsNamed builds n actors in one namespace, alternating status so that the
// grouping the service sorts by is actually exercised rather than assumed.
func actorsNamed(namespace string, n int) []*ateapipb.Actor {
	actors := make([]*ateapipb.Actor, 0, n)
	for i := range n {
		state := ateapipb.ActorState_ACTOR_STATE_RUNNING
		if i%2 == 1 {
			state = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
		}
		actors = append(actors, &ateapipb.Actor{
			Metadata:               &ateapipb.ResourceMetadata{Name: fmt.Sprintf("actor-%03d", i)},
			Status:                 &ateapipb.ActorStatus{State: state},
			ActorTemplateNamespace: namespace,
			ActorTemplateName:      "template",
		})
	}
	return actors
}

func workersNamed(namespace string, n int, busy int) []*ateapipb.Worker {
	workers := make([]*ateapipb.Worker, 0, n)
	for i := range n {
		worker := &ateapipb.Worker{
			Metadata:        &ateapipb.ResourceMetadata{Version: int64(i)},
			WorkerNamespace: namespace,
			WorkerPool:      "pool",
			WorkerPod:       fmt.Sprintf("worker-%03d", i),
			Status:          &ateapipb.WorkerStatus{},
		}
		if i < busy {
			worker.Status.Assignment = &ateapipb.ActorAssignment{
				ActorTemplate: &ateapipb.KubeNamespacedObjectRef{Namespace: namespace, Name: "template"},
				Actor:         &ateapipb.ObjectRef{Name: fmt.Sprintf("actor-%03d", i)},
			}
		}
		workers = append(workers, worker)
	}
	return workers
}

// TestGetSubstrateSummary is the guard on the tiles: these counts are the only
// honest total a caller has, because every other read is now a page.
func TestGetSubstrateSummary(t *testing.T) {
	ctx := substrateContext(t)

	t.Run("counts everything in scope and carries the small lists inline", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).WithObjects(
			&atev1alpha1.WorkerPool{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pool"},
				Spec:       atev1alpha1.WorkerPoolSpec{Replicas: 8, AteomImage: "ateom:test"},
			},
			&atev1alpha1.ActorTemplate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "template"},
				Status:     atev1alpha1.ActorTemplateStatus{Phase: atev1alpha1.PhaseReady},
			},
		).Build()
		ateClient := &fakeATEClient{
			actors:  actorsNamed("team", 10),
			workers: workersNamed("team", 8, 3),
		}
		service := system.NewService(system.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient))

		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)

		assert.True(t, result.Enabled)
		assert.Empty(t, result.ATEAPIError)
		require.Len(t, result.WorkerPools, 1)
		assert.Equal(t, int32(8), result.WorkerPools[0].Replicas)
		require.Len(t, result.ActorTemplates, 1)

		assert.Equal(t, int32(10), result.ActorCount)
		assert.Equal(t, int32(5), result.RunningActorCount, "half the actors are Running")
		assert.Equal(t, int32(8), result.WorkerCount)
		assert.Equal(t, int32(3), result.BusyWorkerCount, "a worker is busy when an actor is placed on it")

		// The whole distribution, not only the running tally: a caller that knows 5
		// of 10 are running still cannot say what the other 5 are doing.
		assert.Equal(t, []system.SubstrateStatusCount{
			{Status: "Running", Count: 5},
			{Status: "Suspended", Count: 5},
		}, result.ActorStatusCounts)
	})

	t.Run("reports a partial ate-api read rather than failing", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).WithObjects(
			&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pool"}},
		).Build()
		ateClient := &fakeATEClient{err: assert.AnError}
		service := system.NewService(system.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, ateClient))

		result, err := service.GetSubstrateSummary(ctx, "team")

		// The Kubernetes half is complete, so failing the call would hide data that
		// arrived intact.
		require.NoError(t, err)
		assert.NotEmpty(t, result.ATEAPIError)
		assert.Len(t, result.WorkerPools, 1)
		assert.Equal(t, int32(0), result.ActorCount)
	})

	t.Run("disabled does not read Kubernetes", func(t *testing.T) {
		service := system.NewService(system.WithInventory(nil, nil, &authimpl.NoopAuthorizer{}, nil))
		result, err := service.GetSubstrateSummary(ctx, "team")
		require.NoError(t, err)
		assert.False(t, result.Enabled)
		assert.Empty(t, result.WorkerPools)
	})

	t.Run("validates and authorizes exactly as GetSubstrateStatus does", func(t *testing.T) {
		service := system.NewService(system.WithInventory(nil, nil, &authimpl.NoopAuthorizer{}, nil))
		_, err := service.GetSubstrateSummary(ctx, "INVALID_NAMESPACE")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		service = system.NewService(system.WithInventory(nil, nil, systemDenyAuthorizer{}, nil))
		_, err = service.GetSubstrateSummary(ctx, "")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}

func TestListSubstrateActors(t *testing.T) {
	ctx := substrateContext(t)
	kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).Build()

	newService := func(actors []*ateapipb.Actor) *system.Service {
		return system.NewService(system.WithInventory(
			kubeClient, nil, &authimpl.NoopAuthorizer{},
			&fakeATEClient{actors: actors},
		))
	}

	t.Run("pages through every actor exactly once", func(t *testing.T) {
		service := newService(actorsNamed("team", 25))

		seen := map[string]int{}
		pageToken := ""
		pages := 0
		for {
			result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: pageToken})
			require.NoError(t, err)
			// The total is of everything matching, not of this page — which is what
			// lets a caller say "10 of 25" instead of implying the page is the lot.
			assert.Equal(t, int32(25), result.TotalSize)
			for _, actor := range result.Actors {
				seen[actor.ActorID]++
			}
			pages++
			require.Less(t, pages, 10, "paging did not terminate")
			if result.NextPageToken == "" {
				// An empty token is the last page, so a caller never fetches an empty
				// one to discover it has finished.
				assert.LessOrEqual(t, len(result.Actors), 10)
				break
			}
			pageToken = result.NextPageToken
		}

		assert.Equal(t, 3, pages, "25 actors at 10 a page")
		assert.Len(t, seen, 25, "every actor appeared")
		for id, count := range seen {
			assert.Equal(t, 1, count, "%s appeared more than once", id)
		}
	})

	t.Run("filters server-side across the whole list, not one page", func(t *testing.T) {
		service := newService(actorsNamed("team", 25))

		// actor-019 sorts well past the first page, so a client-side filter over a
		// fetched page would report no matches for it. That is the failure this
		// exists to prevent.
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "actor-019", Limit: 10, PageToken: ""})
		require.NoError(t, err)
		require.Len(t, result.Actors, 1)
		assert.Equal(t, "actor-019", result.Actors[0].ActorID)
		assert.Equal(t, int32(1), result.TotalSize)
		assert.Empty(t, result.NextPageToken)
	})

	t.Run("matches case-insensitively on status too", func(t *testing.T) {
		service := newService(actorsNamed("team", 10))
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "SUSPEND", Limit: 100, PageToken: ""})
		require.NoError(t, err)
		assert.Equal(t, int32(5), result.TotalSize)
	})

	t.Run("groups by status so a page is stable between reads", func(t *testing.T) {
		service := newService(actorsNamed("team", 10))
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 100, PageToken: ""})
		require.NoError(t, err)
		require.Len(t, result.Actors, 10)

		// Sorted by status then id: every Running actor precedes every Suspended one.
		for i := range 5 {
			assert.Equal(t, "Running", result.Actors[i].Status)
		}
		for i := 5; i < 10; i++ {
			assert.Equal(t, "Suspended", result.Actors[i].Status)
		}
	})

	t.Run("refuses a page size it cannot honour rather than clamping", func(t *testing.T) {
		service := newService(actorsNamed("team", 5))

		_, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 5000, PageToken: ""})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		_, err = service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: -1, PageToken: ""})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		// Zero is "no preference" and takes the default.
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 0, PageToken: ""})
		require.NoError(t, err)
		assert.Len(t, result.Actors, 5)
	})

	t.Run("rejects a page token that is not one", func(t *testing.T) {
		service := newService(actorsNamed("team", 5))
		_, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: "not base64!!"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
	})

	t.Run("answers empty when ate-api is not configured", func(t *testing.T) {
		service := system.NewService(system.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, nil))
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: ""})
		require.NoError(t, err)
		assert.Empty(t, result.Actors)
		assert.Equal(t, int32(0), result.TotalSize)
	})

	t.Run("fails the call when ate-api does", func(t *testing.T) {
		// Unlike the summary there is no complete half to salvage: this call answers
		// with actors or it answers with nothing.
		service := system.NewService(system.WithInventory(
			kubeClient, nil, &authimpl.NoopAuthorizer{}, &fakeATEClient{err: assert.AnError},
		))
		_, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: ""})
		require.Error(t, err)
	})

	t.Run("validates and authorizes", func(t *testing.T) {
		service := newService(nil)
		_, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "INVALID_NAMESPACE", Filter: "", Limit: 10, PageToken: ""})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)

		denied := system.NewService(system.WithInventory(kubeClient, nil, systemDenyAuthorizer{}, &fakeATEClient{}))
		_, err = denied.ListSubstrateActors(ctx, system.ListActorsRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: ""})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}

func TestListSubstrateWorkers(t *testing.T) {
	ctx := substrateContext(t)
	kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).Build()

	newService := func(workers []*ateapipb.Worker) *system.Service {
		return system.NewService(system.WithInventory(
			kubeClient, nil, &authimpl.NoopAuthorizer{},
			&fakeATEClient{workers: workers},
		))
	}

	t.Run("pages through every worker exactly once", func(t *testing.T) {
		service := newService(workersNamed("team", 12, 4))

		seen := map[string]int{}
		pageToken := ""
		for pages := 0; ; pages++ {
			require.Less(t, pages, 10, "paging did not terminate")
			result, err := service.ListSubstrateWorkers(ctx, system.ListWorkersRequest{Namespace: "team", Filter: "", Limit: 5, PageToken: pageToken})
			require.NoError(t, err)
			assert.Equal(t, int32(12), result.TotalSize)
			for _, worker := range result.Workers {
				seen[worker.WorkerPod]++
			}
			if result.NextPageToken == "" {
				break
			}
			pageToken = result.NextPageToken
		}

		assert.Len(t, seen, 12)
		for pod, count := range seen {
			assert.Equal(t, 1, count, "%s appeared more than once", pod)
		}
	})

	t.Run("filters on the placed actor", func(t *testing.T) {
		service := newService(workersNamed("team", 12, 4))
		result, err := service.ListSubstrateWorkers(ctx, system.ListWorkersRequest{Namespace: "team", Filter: "actor-002", Limit: 100, PageToken: ""})
		require.NoError(t, err)
		require.Len(t, result.Workers, 1)
		assert.Equal(t, "worker-002", result.Workers[0].WorkerPod)
	})

	t.Run("answers empty when ate-api is not configured", func(t *testing.T) {
		service := system.NewService(system.WithInventory(kubeClient, nil, &authimpl.NoopAuthorizer{}, nil))
		result, err := service.ListSubstrateWorkers(ctx, system.ListWorkersRequest{Namespace: "team", Filter: "", Limit: 10, PageToken: ""})
		require.NoError(t, err)
		assert.Empty(t, result.Workers)
	})
}

// TestListSubstrateActorsSorting is the guard on server-side ordering.
//
// The property that matters is not "the rows came back sorted" — it is that
// **paging through a sorted result yields every row exactly once**. A selector
// whose direction and whose page token disagree drops rows at a page boundary,
// and it does so silently: each page looks correctly ordered on its own.
func TestListSubstrateActorsSorting(t *testing.T) {
	ctx := substrateContext(t)
	kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).Build()
	service := system.NewService(system.WithInventory(
		kubeClient, nil, &authimpl.NoopAuthorizer{},
		&fakeATEClient{actors: actorsNamed("team", 25)},
	))

	// Every order the API offers, in both directions.
	fields := []system.ActorSortField{
		system.ActorSortDefault,
		system.ActorSortStatus,
		system.ActorSortID,
		system.ActorSortTemplate,
		system.ActorSortWorker,
	}
	orders := []system.SortOrder{system.SortAscending, system.SortDescending}

	for _, field := range fields {
		for _, order := range orders {
			t.Run(string(field)+"/"+string(order), func(t *testing.T) {
				seen := map[string]int{}
				var ordered []string
				pageToken := ""

				for pages := 0; ; pages++ {
					require.Less(t, pages, 12, "paging did not terminate")
					result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{
						Namespace: "team",
						Limit:     7,
						PageToken: pageToken,
						SortField: field,
						SortOrder: order,
					})
					require.NoError(t, err)

					// The order applied is reported, not assumed — a caller says how its
					// rows are sorted rather than trusting the request was honoured.
					assert.Equal(t, field, result.SortField)
					assert.Equal(t, order, result.SortOrder)
					assert.Equal(t, int32(25), result.TotalSize)

					for _, actor := range result.Actors {
						seen[actor.ActorID]++
						ordered = append(ordered, actor.ActorID)
					}
					if result.NextPageToken == "" {
						break
					}
					pageToken = result.NextPageToken
				}

				// Every row, exactly once, across the whole walk.
				assert.Len(t, seen, 25, "paging lost or repeated rows")
				for id, count := range seen {
					assert.Equal(t, 1, count, "%s appeared more than once", id)
				}

				// And the concatenated pages are themselves in order: a page that
				// sorted only within itself would satisfy the count above.
				sorted := append([]string(nil), ordered...)
				slices.Sort(sorted)
				if order == system.SortDescending {
					slices.Reverse(sorted)
				}
				if field == system.ActorSortID {
					// Only the id sort is a total order on the id alone; the others tie
					// on their column and break it with the id, so the ids themselves
					// are not monotonic.
					assert.Equal(t, sorted, ordered, "pages were not in the requested order")
				}
			})
		}
	}
}

// TestListSubstrateActorsSortDirectionIsHonoured checks the two directions
// actually differ — a selector that ignored the order would pass every
// completeness assertion above.
func TestListSubstrateActorsSortDirectionIsHonoured(t *testing.T) {
	ctx := substrateContext(t)
	kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).Build()
	service := system.NewService(system.WithInventory(
		kubeClient, nil, &authimpl.NoopAuthorizer{},
		&fakeATEClient{actors: actorsNamed("team", 25)},
	))

	first := func(order system.SortOrder) string {
		result, err := service.ListSubstrateActors(ctx, system.ListActorsRequest{
			Namespace: "team",
			Limit:     1,
			SortField: system.ActorSortID,
			SortOrder: order,
		})
		require.NoError(t, err)
		require.Len(t, result.Actors, 1)
		return result.Actors[0].ActorID
	}

	assert.Equal(t, "actor-000", first(system.SortAscending))
	assert.Equal(t, "actor-024", first(system.SortDescending))
}

// TestListSubstrateWorkersSorting is the same property for the worker list,
// which pages through the same selector.
func TestListSubstrateWorkersSorting(t *testing.T) {
	ctx := substrateContext(t)
	kubeClient := fake.NewClientBuilder().WithScheme(substrateScheme(t)).Build()
	service := system.NewService(system.WithInventory(
		kubeClient, nil, &authimpl.NoopAuthorizer{},
		&fakeATEClient{workers: workersNamed("team", 12, 4)},
	))

	for _, field := range []system.WorkerSortField{
		system.WorkerSortDefault,
		system.WorkerSortPool,
		system.WorkerSortPod,
		system.WorkerSortActor,
	} {
		for _, order := range []system.SortOrder{system.SortAscending, system.SortDescending} {
			seen := map[string]int{}
			pageToken := ""
			for pages := 0; ; pages++ {
				require.Less(t, pages, 12, "paging did not terminate")
				result, err := service.ListSubstrateWorkers(ctx, system.ListWorkersRequest{
					Namespace: "team",
					Limit:     5,
					PageToken: pageToken,
					SortField: field,
					SortOrder: order,
				})
				require.NoError(t, err)
				assert.Equal(t, field, result.SortField)
				assert.Equal(t, int32(12), result.TotalSize)
				for _, worker := range result.Workers {
					seen[worker.WorkerPod]++
				}
				if result.NextPageToken == "" {
					break
				}
				pageToken = result.NextPageToken
			}
			assert.Len(t, seen, 12, "%s/%s lost or repeated rows", field, order)
		}
	}
}
