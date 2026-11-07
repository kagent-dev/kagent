package system_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type systemDenyAuthorizer struct{}

func (systemDenyAuthorizer) Scope(context.Context, pkgAuth.Principal, pkgAuth.Verb, string) (apiauthorization.AuthorizationScope, error) {
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}, nil
}

func (systemDenyAuthorizer) Check(context.Context, pkgAuth.Principal, pkgAuth.Verb, pkgAuth.Resource) error {
	return errors.New("denied")
}

type fakeATEClient struct {
	templates     []*ateapipb.ActorTemplate
	templateReads int
	actors        []*ateapipb.Actor
	workers       []*ateapipb.Worker
	err           error
	// The number of rows a page answers with, whatever the caller asked for. Zero
	// means "everything in one page". ate-api may answer with fewer rows than asked
	// for, so a fake that always fills the request would hide a caller that stops
	// following the token as soon as it has enough rows.
	pageSize int
	// How many page reads each list has taken, so a test can assert that the token
	// was followed rather than that the rows came back.
	actorTokens       []string
	workerTokens      []string
	actorSizes        []int32
	actorReads        int
	workerReads       int
	actorAtespaces    []string
	templateAtespaces []string
}

func (client *fakeATEClient) ListActorTemplates(_ context.Context, atespace string) ([]*ateapipb.ActorTemplate, error) {
	client.templateReads++
	client.templateAtespaces = append(client.templateAtespaces, atespace)
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
	client.actorSizes = append(client.actorSizes, pageSize)
	client.actorTokens = append(client.actorTokens, pageToken)
	client.actorAtespaces = append(client.actorAtespaces, atespace)
	if err := client.readError(client.actorReads); err != nil {
		return nil, "", err
	}
	return fakePage(actorsInAtespace(client.actors, atespace), actorKey, "actor", atespace, client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) ListWorkersPage(_ context.Context, pageSize int32, pageToken string) ([]*ateapipb.Worker, string, error) {
	client.workerReads++
	client.workerTokens = append(client.workerTokens, pageToken)
	if err := client.readError(client.workerReads); err != nil {
		return nil, "", err
	}
	return fakePage(client.workers, workerKey, "worker", "", client.pageSize, pageSize, pageToken)
}

func (client *fakeATEClient) readError(int) error {
	return client.err
}

// Matches the pinned Substrate atepg token format, including scope-specific keys.
type fakeCursor struct {
	Version int      `json:"v"`
	Kind    string   `json:"kind"`
	Scope   string   `json:"scope"`
	Last    []string `json:"last"`
}

func fakePage[T any](rows []T, key func(T) string, kind, scope string, ceiling int, requested int32, pageToken string) ([]T, string, error) {
	rows = slices.Clone(rows)
	slices.SortFunc(rows, func(a, b T) int { return strings.Compare(key(a), key(b)) })
	pageSize := ceiling
	if requested > 0 && (ceiling <= 0 || int(requested) < ceiling) {
		pageSize = int(requested)
	}
	start := 0
	if pageToken != "" {
		var cursor fakeCursor
		raw, err := base64.StdEncoding.DecodeString(pageToken)
		parts := 1
		if kind == "actor" && scope == "" {
			parts = 2
		}
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Kind != kind || cursor.Scope != scope || len(cursor.Last) != parts {
			return nil, "", errors.New("invalid page token")
		}
		after := strings.Join(cursor.Last, "/")
		if kind == "actor" && scope != "" {
			after = scope + "/" + after
		}
		for start < len(rows) && key(rows[start]) <= after {
			start++
		}
	}
	if pageSize <= 0 {
		return rows[start:], "", nil
	}
	end := min(start+pageSize, len(rows))
	next := ""
	if end < len(rows) {
		last := []string{key(rows[end-1])}
		if kind == "actor" {
			location, name, _ := strings.Cut(last[0], "/")
			last = []string{name}
			if scope == "" {
				last = []string{location, name}
			}
		}
		raw, _ := json.Marshal(fakeCursor{Version: 1, Kind: kind, Scope: scope, Last: last})
		next = base64.StdEncoding.EncodeToString(raw)
	}
	return rows[start:end], next, nil
}

// Malformed/foreign token fixtures used by the public cursor rejection tests.
func fakePageToken(last string, _ int) string {
	raw, _ := json.Marshal(fakeCursor{Version: 1, Kind: "worker", Last: []string{last}})
	return base64.StdEncoding.EncodeToString(raw)
}

func actorKey(actor *ateapipb.Actor) string {
	return actor.GetMetadata().GetAtespace() + "/" + actor.GetMetadata().GetName()
}

func workerKey(worker *ateapipb.Worker) string {
	return worker.GetMetadata().GetName()
}

func TestCurrentUser(t *testing.T) {
	service := system.NewService(nil, nil, nil, nil)
	claims := map[string]any{"sub": "user-1", "groups": []any{"admins"}}
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{
		ID:     "user-1",
		Claims: claims,
	}})

	result, err := service.GetCurrentUser(ctx)
	require.NoError(t, err)
	assert.Equal(t, claims, result)

	ctx = pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{
		ID: "fallback-user",
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
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})

	t.Run("lists all and sorts case insensitively", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "Zoo"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
		).Build()
		service := system.NewService(kubeClient, nil, pkgAuth.NoopAuthorizer{}, nil)

		result, err := service.ListNamespaces(ctx)
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
		service := system.NewService(kubeClient, []string{"team-b", "team-a"}, pkgAuth.NoopAuthorizer{}, nil)

		result, err := service.ListNamespaces(ctx)
		require.NoError(t, err)
		assert.Equal(t, []system.Namespace{{Name: "team-a"}, {Name: "team-b"}}, result)
	})
	t.Run("denied before inventory reads", func(t *testing.T) {
		authorizer := &summaryAuthorizer{denyType: "SystemNamespaces"}
		service := system.NewService(nil, nil, authorizer, nil)
		_, err := service.ListNamespaces(ctx)
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
		require.Equal(t, []string{"get SystemNamespaces"}, authorizer.calls)
	})
}

// substrateActor builds an ate-api actor placed on a worker pod, or on none when
// workerPod is empty.
func substrateActor(name, atespace string, state ateapipb.ActorState, workerNamespace, workerPod string) *ateapipb.Actor {
	status := &ateapipb.ActorStatus{State: state}
	if workerPod != "" {
		status.WorkerAssignment = &ateapipb.WorkerAssignment{
			Worker:          &ateapipb.ObjectRef{Name: "worker-identity"},
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

func TestGetSubstrateSummary(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1.WorkerPool{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pool"},
			Spec:       atev1.WorkerPoolSpec{Replicas: 2, WorkerImage: "ateom:test"},
		},
	).Build()

	t.Run("counts across every page without materialising the inventory", func(t *testing.T) {
		ateClient := &fakeATEClient{
			// One row per page, so a summary that reads a single page is visible as a
			// count of one rather than as a passing test.
			pageSize: 1,
			templates: []*ateapipb.ActorTemplate{{
				Metadata:   &ateapipb.ResourceMetadata{Atespace: "team", Name: "template", Uid: "template-uid"},
				Containers: []*ateapipb.Container{{Env: []*ateapipb.EnvVar{{Name: "API_KEY", Value: "secret"}}}},
				Status: &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
					GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
				}},
			}},
			actors: []*ateapipb.Actor{
				substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
				substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
				substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", ""),
				substrateActor("actor-4", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "other", "worker-9"),
			},
			workers: []*ateapipb.Worker{
				{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-0"}, WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}},
				{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-1"}, WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-1"},
				{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-2"}, WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-9"},
			},
		}
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

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
			{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, Count: 1},
		}, result.ActorStatusCounts)
		require.Len(t, result.WorkerPools, 1)
		require.Len(t, result.ActorTemplates, 1)
		template := result.ActorTemplates[0]
		assert.Equal(t, "template-uid", template.GetMetadata().GetUid())
		assert.Equal(t, "golden", template.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag().GetName())
		assert.Empty(t, template.GetContainers())
		require.Len(t, ateClient.templates[0].GetContainers(), 1)
		assert.Equal(t, "secret", ateClient.templates[0].GetContainers()[0].GetEnv()[0].GetValue())
		assert.False(t, result.ComputedAt.IsZero())
	})

	t.Run("an ate-api failure leaves the Kubernetes halves complete", func(t *testing.T) {
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, &fakeATEClient{err: errors.New("ate-api unreachable")})

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api could not read inventory: Unknown", result.ATEAPIError)
		assert.Zero(t, result.ActorCount)
		require.Len(t, result.WorkerPools, 1)
	})
}

// The summary's three ate-api reads are independent, and a database failure is not one
// of them: one failed read must not zero the other counts or be reported as ate-api's.
func TestGetSubstrateSummaryReadsAreIndependent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	actors := []*ateapipb.Actor{
		substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
		substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", ""),
	}
	workers := []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity"}, WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}}}

	t.Run("a failed template listing still counts the actors and the workers", func(t *testing.T) {
		// The template listing is the first ate-api read, so failing from read one
		// fails it and leaves the two walks to answer.
		ateClient := &failingTemplatesATEClient{
			fakeATEClient: fakeATEClient{actors: actors, workers: workers},
		}
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api could not read inventory: Unknown", result.ATEAPIError)
		assert.Empty(t, result.ActorTemplates)
		// The counts the tiles read. Zero here is the page reporting an empty cluster.
		assert.Equal(t, int64(2), result.ActorCount)
		assert.Equal(t, int64(1), result.RunningActorCount)
		assert.Equal(t, int64(1), result.WorkerCount)
		assert.Equal(t, int64(1), result.BusyWorkerCount)
	})

	t.Run("a failed actor walk still counts busy workers", func(t *testing.T) {
		ateClient := &failingActorsATEClient{fakeATEClient: fakeATEClient{workers: workers}}
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)
		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api could not read inventory: Unknown", result.ATEAPIError)
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
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

		result, err := service.GetSubstrateSummary(ctx, "team", "team")
		require.NoError(t, err)
		assert.Equal(t, "ate-api could not read inventory: Unknown", result.ATEAPIError)
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
				{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity"}, WorkerNamespace: "kagent", WorkerPool: "pool", WorkerPod: "worker-0"},
			},
		}
		service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

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
	workerErr error
}

func (client *failingWorkersATEClient) ListWorkersPage(context.Context, int32, string) ([]*ateapipb.Worker, string, error) {
	if client.workerErr != nil {
		return nil, "", client.workerErr
	}
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
	require.NoError(t, atev1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})
	ateClient := &fakeATEClient{
		actors: []*ateapipb.Actor{
			substrateActor("a", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "kagent", "worker-0"),
		},
		workers: []*ateapipb.Worker{
			{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity"}, WorkerNamespace: "kagent", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 1}}},
		},
	}
	service := system.NewService(kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)
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
	require.NoError(t, atev1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "workers", Name: "pool"}},
	).Build()
	actor := substrateActor("same-name", "team-a", ateapipb.ActorState_ACTOR_STATE_RUNNING, "workers", "pod")
	actor.ActorTemplate.Atespace = "shared"
	ateClient := &fakeATEClient{
		pageSize: 1,
		actors:   []*ateapipb.Actor{actor, substrateActor("same-name", "shared", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", "")},
		templates: []*ateapipb.ActorTemplate{
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "shared", Name: "template"}},
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "own-template"}},
		},
		workers: []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity"}, WorkerNamespace: "workers", WorkerPod: "pod"}},
	}
	service := system.NewService(kubeClient, []string{"workers", "team-a", "shared"}, &pkgAuth.NoopAuthorizer{}, ateClient)
	for _, tc := range []struct {
		atespace string
		count    int64
		running  int64
	}{
		{atespace: "", count: 2, running: 1},
		{atespace: "team-a", count: 1, running: 1},
		{atespace: "shared", count: 1, running: 0},
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
			page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: tc.atespace, Page: &apiv1.PageRequest{Limit: 1}})
			require.NoError(t, err)
			for _, row := range page.Actors {
				if tc.atespace != "" {
					assert.Equal(t, tc.atespace, row.GetMetadata().GetAtespace())
				}
			}
		})
	}
	page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: "team-a"})
	require.NoError(t, err)
	require.Len(t, page.Actors, 1)
	assert.Same(t, actor, page.Actors[0])
}

type summaryAuthorizer struct {
	pkgAuth.NoopAuthorizer
	pools     apiauthorization.AuthorizationScope
	denyType  string
	failScope bool
	calls     []string
}

func (a *summaryAuthorizer) Check(_ context.Context, _ pkgAuth.Principal, verb pkgAuth.Verb, resource pkgAuth.Resource) error {
	a.calls = append(a.calls, string(verb)+" "+resource.Type)
	if resource.Type == a.denyType {
		return errors.New("denied")
	}
	if resource.Namespace != "" || resource.Name != "" {
		return errors.New("capability must not have an object identity")
	}
	return nil
}

func (a *summaryAuthorizer) Scope(_ context.Context, _ pkgAuth.Principal, verb pkgAuth.Verb, resource string) (apiauthorization.AuthorizationScope, error) {
	a.calls = append(a.calls, string(verb)+" "+resource)
	if a.failScope {
		return apiauthorization.AuthorizationScope{}, errors.New("policy unavailable")
	}
	if resource != "WorkerPool" {
		return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, nil
	}
	return a.pools, nil
}

func TestSummaryRequiresCapabilityAndScopesPools(t *testing.T) {
	all := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}
	none := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}
	constrained := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
		{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team"}},
		{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"allowed"}},
	}}}}
	for _, test := range []struct {
		name      string
		pools     apiauthorization.AuthorizationScope
		observed  []string
		requested string
		failScope bool
		wantPools int
		code      serviceerrors.Code
	}{
		{name: "scoped pools", pools: constrained, wantPools: 1},
		{name: "all pools", pools: all, wantPools: 3},
		{name: "no pools", pools: none},
		{name: "configured intersection", pools: all, observed: []string{"team"}, wantPools: 2},
		{name: "request cannot escape configuration", pools: all, observed: []string{"team"}, requested: "other", code: serviceerrors.CodePermissionDenied},
		{name: "request cannot escape policy", pools: constrained, requested: "other"},
		{name: "pool lookup failure", failScope: true, code: serviceerrors.CodeUnavailable},
		{name: "invalid pool scope", pools: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf}, code: serviceerrors.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, atev1.AddToScheme(scheme))
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
				&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "allowed"}},
				&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "hidden"}},
				&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "allowed"}},
			).Build()
			// Summary access is independent from the dedicated Substrate endpoints.
			authorizer := &summaryAuthorizer{pools: test.pools, denyType: "Substrate", failScope: test.failScope}
			ate := &fakeATEClient{actors: []*ateapipb.Actor{substrateActor("actor", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker")}}
			service := system.NewService(kube, test.observed, authorizer, ate)
			ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
			result, err := service.GetSubstrateSummary(ctx, test.requested, "team")
			wantCalls := []string{"get SystemSummary", "list WorkerPool"}
			if test.code == "" {
				wantCalls = append(wantCalls, "list SubstrateActor", "list SubstrateActorTemplate", "list SubstrateWorker")
			}
			require.Equal(t, wantCalls, authorizer.calls)
			if test.code != "" {
				require.True(t, serviceerrors.IsCode(err, test.code))
				require.Zero(t, ate.actorReads)
				require.Zero(t, ate.workerReads)
				require.Zero(t, ate.templateReads)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.WorkerPools, test.wantPools)
			require.EqualValues(t, 1, result.ActorCount)
			require.Positive(t, ate.actorReads)
			require.Positive(t, ate.workerReads)
			require.Positive(t, ate.templateReads)
		})
	}
}

func TestSummaryDeniedBeforeResourceReads(t *testing.T) {
	authorizer := &summaryAuthorizer{denyType: "SystemSummary"}
	// Nil clients ensure a denied request cannot read Kubernetes or ATE inventory.
	service := system.NewService(nil, nil, authorizer, nil)
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
	_, err := service.GetSubstrateSummary(ctx, "", "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	require.Equal(t, []string{"get SystemSummary"}, authorizer.calls)
	// The separate inventory capability is still allowed.
	_, err = system.NewService(nil, nil, authorizer, &fakeATEClient{}).ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.NoError(t, err)
}

func TestSummaryDeniedPoolsDoNotRequireKubernetes(t *testing.T) {
	authorizer := &summaryAuthorizer{pools: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}}
	service := system.NewService(nil, nil, authorizer, &fakeATEClient{})
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
	result, err := service.GetSubstrateSummary(ctx, "", "")
	require.NoError(t, err)
	require.Empty(t, result.WorkerPools)
	_, err = service.GetSubstrateSummary(t.Context(), "", "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
}

func TestSubstrateTenantIsolation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, atev1.AddToScheme(scheme))
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})
	ateClient := &fakeATEClient{
		pageSize: 1,
		actors: []*ateapipb.Actor{
			substrateActor("session-b", "tenant-b", ateapipb.ActorState_ACTOR_STATE_RUNNING, "tenant-b", "pod-b"),
			substrateActor("session-a", "tenant-a", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", ""),
			substrateActor("session-c", "tenant-c", ateapipb.ActorState_ACTOR_STATE_RUNNING, "tenant-c", "pod-c"),
			substrateActor("session-b2", "tenant-b", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", ""),
		},
		templates: []*ateapipb.ActorTemplate{
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "tenant-a", Name: "agent-a"}},
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "tenant-b", Name: "agent-b"}},
			{Metadata: &ateapipb.ResourceMetadata{Atespace: "tenant-c", Name: "agent-c"}},
		},
		workers: []*ateapipb.Worker{
			{Metadata: &ateapipb.ResourceMetadata{Name: "uid-a"}, WorkerNamespace: "tenant-a", WorkerPod: "pod-a"},
			{Metadata: &ateapipb.ResourceMetadata{Name: "uid-b"}, WorkerNamespace: "tenant-b", WorkerPod: "pod-b"},
			{Metadata: &ateapipb.ResourceMetadata{Name: "uid-c"}, WorkerNamespace: "tenant-c", WorkerPod: "pod-c"},
			{Metadata: &ateapipb.ResourceMetadata{Name: "uid-unlabelled"}, WorkerPod: "pod-unlabelled"},
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "pool-a"}},
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-b", Name: "pool-b"}},
		&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-c", Name: "pool-c"}},
	).Build()
	newService := func(observedNamespaces []string) *system.Service {
		return system.NewService(kubeClient, observedNamespaces, &pkgAuth.NoopAuthorizer{}, ateClient)
	}
	tenantA := newService([]string{"tenant-a"})
	tenantsAC := newService([]string{"tenant-a", "tenant-c"})
	tenantsACDuplicated := newService([]string{"tenant-a", "tenant-c", "tenant-a"})
	clusterWide := newService(nil)

	listAllActors := func(t *testing.T, service *system.Service, atespace string) []string {
		var names []string
		request := &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: 1}}
		for {
			page, err := service.ListSubstrateActors(ctx, request)
			require.NoError(t, err)
			for _, actor := range page.Actors {
				names = append(names, actor.GetMetadata().GetAtespace()+"/"+actor.GetMetadata().GetName())
			}
			if page.NextPageToken == "" {
				return names
			}
			request.Page.PageToken = page.NextPageToken
		}
	}
	workerPods := func(t *testing.T, service *system.Service) []string {
		var pods []string
		request := &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{}}
		for {
			page, err := service.ListSubstrateWorkers(ctx, request)
			require.NoError(t, err)
			for _, worker := range page.Workers {
				pods = append(pods, worker.GetWorkerPod())
			}
			if page.NextPageToken == "" {
				return pods
			}
			request.Page.PageToken = page.NextPageToken
		}
	}
	poolNames := func(summary system.SubstrateSummary) []string {
		var names []string
		for _, pool := range summary.WorkerPools {
			names = append(names, pool.Namespace+"/"+pool.Name)
		}
		return names
	}
	templateNames := func(summary system.SubstrateSummary) []string {
		var names []string
		for _, template := range summary.ActorTemplates {
			names = append(names, template.GetMetadata().GetName())
		}
		return names
	}

	t.Run("lists only its own actors", func(t *testing.T) {
		assert.Equal(t, []string{"tenant-a/session-a"}, listAllActors(t, tenantA, ""))
	})

	t.Run("summarises only its own atespace", func(t *testing.T) {
		summary, err := tenantA.GetSubstrateSummary(ctx, "", "")
		require.NoError(t, err)
		require.Empty(t, summary.ATEAPIError)
		assert.Equal(t, []string{"agent-a"}, templateNames(summary))
		assert.Equal(t, []string{"tenant-a/pool-a"}, poolNames(summary))
		assert.Equal(t, int64(1), summary.ActorCount)
		assert.Equal(t, int64(0), summary.RunningActorCount)
		assert.Equal(t, int64(1), summary.WorkerCount)
	})

	t.Run("hides a worker that reports no namespace", func(t *testing.T) {
		assert.Equal(t, []string{"pod-a"}, workerPods(t, tenantA))
		assert.Equal(t, []string{"pod-a", "pod-c"}, workerPods(t, tenantsAC))
	})

	t.Run("a controller watching two namespaces lists only those atespaces", func(t *testing.T) {
		assert.Equal(t, []string{"tenant-a/session-a", "tenant-c/session-c"}, listAllActors(t, tenantsAC, ""))
	})

	t.Run("a controller watching two namespaces asks ate-api for only those atespaces", func(t *testing.T) {
		ateClient.actorAtespaces, ateClient.templateAtespaces = nil, nil
		summary, err := tenantsAC.GetSubstrateSummary(ctx, "", "")
		require.NoError(t, err)
		require.Empty(t, summary.ATEAPIError)
		assert.Equal(t, int64(2), summary.ActorCount)
		assert.Equal(t, int64(1), summary.RunningActorCount)
		assert.ElementsMatch(t, []system.SubstrateActorStatusCount{
			{State: ateapipb.ActorState_ACTOR_STATE_RUNNING, Count: 1},
			{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, Count: 1},
		}, summary.ActorStatusCounts)
		assert.Equal(t, []string{"agent-a", "agent-c"}, templateNames(summary))
		assert.Equal(t, []string{"tenant-a/pool-a", "tenant-c/pool-c"}, poolNames(summary))
		assert.Equal(t, int64(2), summary.WorkerCount)

		assert.Equal(t, []string{"tenant-a", "tenant-c"}, slices.Compact(slices.Sorted(slices.Values(ateClient.actorAtespaces))))
		assert.ElementsMatch(t, []string{"tenant-a", "tenant-c"}, ateClient.templateAtespaces)
	})

	t.Run("a namespace listed twice in the config is counted once", func(t *testing.T) {
		summary, err := tenantsACDuplicated.GetSubstrateSummary(ctx, "", "")
		require.NoError(t, err)
		require.Empty(t, summary.ATEAPIError)
		assert.Equal(t, int64(2), summary.ActorCount)
		assert.Equal(t, []string{"agent-a", "agent-c"}, templateNames(summary))
		assert.Equal(t, []string{"tenant-a/pool-a", "tenant-c/pool-c"}, poolNames(summary))
		assert.Equal(t, int64(2), summary.WorkerCount)
	})

	t.Run("refuses another tenant's atespace or namespace", func(t *testing.T) {
		_, err := tenantA.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: "tenant-b"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		assert.Contains(t, serviceerrors.MessageOf(err), "not watched by this controller", "the caller sees only the message, not the cause")
		_, err = tenantA.GetSubstrateSummary(ctx, "", "tenant-b")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		_, err = tenantA.GetSubstrateSummary(ctx, "tenant-b", "")
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
		_, err = tenantA.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: "tenant-b"})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})

	t.Run("a cluster-wide install still sees every tenant", func(t *testing.T) {
		summary, err := clusterWide.GetSubstrateSummary(ctx, "", "")
		require.NoError(t, err)
		assert.Equal(t, int64(4), summary.ActorCount)
		assert.Len(t, summary.ActorTemplates, 3)
		assert.Equal(t, []string{"tenant-a/pool-a", "tenant-b/pool-b", "tenant-c/pool-c"}, poolNames(summary))
		assert.Equal(t, int64(3), summary.WorkerCount)
		assert.Equal(t, []string{"pod-a", "pod-b", "pod-c"}, workerPods(t, clusterWide))
		assert.Equal(t, []string{"tenant-b/session-b", "tenant-b/session-b2"}, listAllActors(t, clusterWide, "tenant-b"))
	})

	t.Run("a cluster-wide install counts nothing in an atespace with no rows", func(t *testing.T) {
		summary, err := clusterWide.GetSubstrateSummary(ctx, "", "missing")
		require.NoError(t, err)
		assert.Empty(t, summary.ATEAPIError)
		assert.Zero(t, summary.ActorCount)
		assert.Zero(t, summary.RunningActorCount)
		assert.Empty(t, summary.ActorStatusCounts)
		assert.Empty(t, summary.ActorTemplates)
	})
}

type inventoryAuthorizer struct {
	pkgAuth.NoopAuthorizer
	scopes map[string]apiauthorization.AuthorizationScope
	fail   string
}

func (a inventoryAuthorizer) Scope(_ context.Context, _ pkgAuth.Principal, verb pkgAuth.Verb, resource string) (apiauthorization.AuthorizationScope, error) {
	if verb != pkgAuth.VerbList || resource == a.fail {
		return apiauthorization.AuthorizationScope{}, errors.New("scope unavailable")
	}
	if scope, ok := a.scopes[resource]; ok {
		return scope, nil
	}
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}, nil
}
func inventoryScope(location, value, name string) apiauthorization.AuthorizationScope {
	predicates := []apiauthorization.ScopePredicate{{Attribute: location, Operator: apiauthorization.ScopeIn, Values: []string{value}}}
	if name != "" {
		predicates = append(predicates, apiauthorization.ScopePredicate{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{name}})
	}
	return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: predicates}}}
}

func TestInventoryFamiliesHaveIndependentTrustedScopes(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, atev1.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "workers", Name: "pool"}}).Build()
	for _, tc := range []struct {
		name, resource                    string
		scope                             apiauthorization.AuthorizationScope
		actors, templates, workers, pools int
	}{
		{name: "summary grants no rows"},
		{name: "actors", resource: "SubstrateActor", scope: inventoryScope(apiauthorization.AttributeAtespace, "team", "allowed"), actors: 1},
		{name: "templates", resource: "SubstrateActorTemplate", scope: inventoryScope(apiauthorization.AttributeAtespace, "team", "allowed"), templates: 1},
		{name: "workers use metadata name", resource: "SubstrateWorker", scope: inventoryScope(apiauthorization.AttributeNamespace, "workers", "allowed-worker"), workers: 1},
		{name: "pool grant does not grant workers", resource: "WorkerPool", scope: inventoryScope(apiauthorization.AttributeNamespace, "workers", "pool"), pools: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ate := &fakeATEClient{pageSize: 1,
				actors:    []*ateapipb.Actor{substrateActor("allowed", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "workers", "pod"), substrateActor("allowed", "other", ateapipb.ActorState_ACTOR_STATE_RUNNING, "workers", "pod"), substrateActor("hidden", "team", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", "")},
				templates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Atespace: "team", Name: "allowed"}, Containers: []*ateapipb.Container{{Env: []*ateapipb.EnvVar{{Name: "SECRET", Value: "hidden"}}}}}, {Metadata: &ateapipb.ResourceMetadata{Atespace: "other", Name: "allowed"}}},
				workers:   []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "allowed-worker"}, WorkerNamespace: "workers", WorkerPod: "different-pod", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 1}}}, {Metadata: &ateapipb.ResourceMetadata{Name: "hidden"}, WorkerNamespace: "workers", WorkerPod: "allowed-worker"}, {Metadata: &ateapipb.ResourceMetadata{Name: "allowed-worker"}, WorkerNamespace: "other"}},
			}
			scopes := map[string]apiauthorization.AuthorizationScope{}
			if tc.resource != "" {
				scopes[tc.resource] = tc.scope
			}
			svc := system.NewService(kube, nil, inventoryAuthorizer{scopes: scopes}, ate)
			ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
			result, err := svc.GetSubstrateSummary(ctx, "", "")
			require.NoError(t, err)
			require.EqualValues(t, tc.actors, result.ActorCount)
			require.EqualValues(t, tc.actors, result.RunningActorCount)
			require.Len(t, result.ActorTemplates, tc.templates)
			require.EqualValues(t, tc.workers, result.WorkerCount)
			require.EqualValues(t, tc.workers, result.BusyWorkerCount)
			require.Len(t, result.WorkerPools, tc.pools)
			if tc.actors == 0 {
				require.Empty(t, result.ActorStatusCounts)
				require.Zero(t, ate.actorReads)
			} else {
				require.EqualValues(t, 1, result.ActorStatusCounts[0].Count)
			}
			if tc.templates == 0 {
				require.Zero(t, ate.templateReads)
			} else {
				require.Empty(t, result.ActorTemplates[0].Containers)
			}
			if tc.workers == 0 {
				require.Zero(t, ate.workerReads)
			}
			narrowed, err := svc.GetSubstrateSummary(ctx, "other", "other")
			require.NoError(t, err)
			require.Zero(t, narrowed.ActorCount)
			require.Empty(t, narrowed.ActorTemplates)
			require.Zero(t, narrowed.WorkerCount)
		})
	}
}

func TestInventoryMissingIdentitiesAndScopeFailuresFailClosed(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
	ate := &fakeATEClient{actors: []*ateapipb.Actor{nil, {Metadata: &ateapipb.ResourceMetadata{Name: "missing-space"}}, {Metadata: &ateapipb.ResourceMetadata{Atespace: "team"}}}, templates: []*ateapipb.ActorTemplate{{Metadata: &ateapipb.ResourceMetadata{Name: "missing-space"}}}, workers: []*ateapipb.Worker{nil, {WorkerNamespace: "workers"}, {Metadata: &ateapipb.ResourceMetadata{Name: "missing-namespace"}}}}
	svc := system.NewService(nil, nil, inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{"SubstrateActor": {Kind: apiauthorization.ScopeAll}, "SubstrateActorTemplate": {Kind: apiauthorization.ScopeAll}, "SubstrateWorker": {Kind: apiauthorization.ScopeAll}}}, ate)
	result, err := svc.GetSubstrateSummary(ctx, "", "")
	require.NoError(t, err)
	require.Zero(t, result.ActorCount)
	require.Zero(t, result.WorkerCount)
	require.Empty(t, result.ActorTemplates)
	for _, resource := range []string{"SubstrateActor", "SubstrateActorTemplate", "SubstrateWorker"} {
		broken := system.NewService(nil, nil, inventoryAuthorizer{fail: resource}, nil)
		_, err := broken.GetSubstrateSummary(ctx, "", "")
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable))
	}
	malformed := system.NewService(nil, nil, inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{"SubstrateWorker": inventoryScope(apiauthorization.AttributeAtespace, "forged", "")}}, nil)
	_, err = malformed.GetSubstrateSummary(ctx, "", "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
}
