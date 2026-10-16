package system_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

func pagingContext(t *testing.T) context.Context {
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
}

// pagingATEClient holds the paging tests' data: three tenants whose rows mix in
// ate-api's order. Every tenant-b name holds "tenant-b", so a test can look for it in
// any text.
func pagingATEClient() *fakeATEClient {
	client := &fakeATEClient{}
	for _, tenant := range []struct {
		atespace string
		actors   int
	}{{"tenant-a", 5}, {"tenant-b", 4}, {"tenant-c", 3}} {
		for i := 1; i <= tenant.actors; i++ {
			client.actors = append(client.actors, namedActor(tenant.atespace, tenant.atespace+"-actor-"+strconv.Itoa(i)))
		}
		client.templates = append(client.templates, &ateapipb.ActorTemplate{
			Metadata: &ateapipb.ResourceMetadata{Atespace: tenant.atespace, Name: tenant.atespace + "-template"},
		})
	}
	for _, pod := range pagingPods() {
		if pod.Labels[system.WorkerPoolLabel] != "" {
			client.workers = append(client.workers, ateWorker(pod.Namespace, string(pod.UID), busyPods[string(pod.UID)]))
		}
	}
	return client
}

// The actors placed on each busy worker pod, by pod UID.
var busyPods = map[string]int32{"u1-tenant-a": 2, "u2-tenant-b": 1, "u3-tenant-c": 1}

// pagingPods are the worker pods of the paging tests. Their UIDs mix the tenants in
// UID order, which is ate-api's worker order. One tenant-a pod is not a worker pod.
func pagingPods() []*corev1.Pod {
	return []*corev1.Pod{
		workerPod("tenant-a", "", "u0-tenant-a"),
		workerPod("tenant-a", "pool-a", "u1-tenant-a"),
		workerPod("tenant-b", "pool-b", "u2-tenant-b"),
		workerPod("tenant-c", "pool-c", "u3-tenant-c"),
		workerPod("tenant-a", "pool-a", "u4-tenant-a"),
		workerPod("tenant-b", "pool-b", "u5-tenant-b"),
		workerPod("tenant-a", "pool-a", "u6-tenant-a"),
	}
}

// ownWorkerUIDs are the workers of tenant-a and tenant-c, in UID order.
var ownWorkerUIDs = []string{"u1-tenant-a", "u3-tenant-c", "u4-tenant-a", "u6-tenant-a"}

// workerPod is a running worker pod of the pool, or a plain pod when pool is empty.
func workerPod(namespace, pool, uid string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "pod-" + uid, UID: types.UID(uid)},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	if pool != "" {
		pod.Labels = map[string]string{system.WorkerPoolLabel: pool}
	}
	return pod
}

// ateWorker is the Worker ate-api holds for a worker pod: it is named after the pod UID.
func ateWorker(namespace, uid string, allocatedActors int32) *ateapipb.Worker {
	return &ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: uid},
		WorkerNamespace: namespace,
		WorkerPod:       "pod-" + uid,
		WorkerPodUid:    uid,
		Status:          &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: allocatedActors}},
	}
}

// pagingKubeClient holds the paging pods, and a WorkerPool in each tenant.
func pagingKubeClient(t *testing.T, funcs interceptor.Funcs, pods ...*corev1.Pod) ctrlclient.WithWatch {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	objects := []ctrlclient.Object{}
	for _, pod := range append(pagingPods(), pods...) {
		objects = append(objects, pod)
	}
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		objects = append(objects, &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: tenant, Name: "pool"}})
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

// walkWorkers reads every page of workers and fails the test on a hard or soft error.
func walkWorkers(t *testing.T, ctx context.Context, service *system.Service, namespace string, limit int32) []system.SubstrateWorkerPage {
	t.Helper()
	pages := []system.SubstrateWorkerPage{}
	pageToken := ""
	for range 100 {
		page, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: namespace, Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		pages = append(pages, page)
		if pageToken = page.NextPageToken; pageToken == "" {
			return pages
		}
	}
	t.Fatal("the walk did not end")
	return nil
}

func walkedWorkerUIDs(pages []system.SubstrateWorkerPage) []string {
	uids := []string{}
	for _, page := range pages {
		for _, worker := range page.Workers {
			uids = append(uids, worker.GetWorkerPodUid())
		}
	}
	return uids
}

// ownActorNames is every actor of the watched atespaces, in the order a walk returns them.
func ownActorNames(client *fakeATEClient, watched []string) []string {
	names := []string{}
	for _, atespace := range slices.Sorted(slices.Values(watched)) {
		for _, actor := range client.actors {
			if actor.GetMetadata().GetAtespace() == atespace && !slices.Contains(names, actor.GetMetadata().GetName()) {
				names = append(names, actor.GetMetadata().GetName())
			}
		}
	}
	return names
}

// walkActors reads every page of actors and fails the test on a hard or soft error.
func walkActors(t *testing.T, ctx context.Context, service *system.Service, atespace string, limit int32) []system.SubstrateActorPage {
	t.Helper()
	pages := []system.SubstrateActorPage{}
	pageToken := ""
	for range 100 {
		page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		pages = append(pages, page)
		if pageToken = page.NextPageToken; pageToken == "" {
			return pages
		}
	}
	t.Fatal("the walk did not end")
	return nil
}

func walkedActorNames(pages []system.SubstrateActorPage) []string {
	names := []string{}
	for _, page := range pages {
		names = append(names, actorNames(page.Actors)...)
	}
	return names
}

func namedActor(atespace, name string) *ateapipb.Actor {
	return substrateActor(name, atespace, ateapipb.ActorState_ACTOR_STATE_RUNNING, "", "")
}

// tokenAtespace reads the atespace a kagent actor page token names.
func tokenAtespace(t *testing.T, token string) string {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(t, err)
	atespace, _, _ := strings.Cut(string(raw), "/")
	return atespace
}

// A page that runs out of rows in one watched atespace goes on in the next, and its
// token names where it stopped.
func TestListSubstrateActorsFanOut(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := &fakeATEClient{actors: []*ateapipb.Actor{
		namedActor("tenant-a", "a-1"), namedActor("tenant-b", "b-1"), namedActor("tenant-c", "c-1"),
		namedActor("tenant-a", "a-2"), namedActor("tenant-c", "c-2"), namedActor("tenant-a", "a-3"),
	}}
	service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	first, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a-1", "a-2"}, actorNames(first.Actors))
	assert.Equal(t, []ateCall{{method: "ListActorsPage", scope: "tenant-a", size: 2}}, ateClient.calls)

	second, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2, PageToken: first.NextPageToken}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a-3", "c-1"}, actorNames(second.Actors))
	assert.Equal(t, []ateCall{
		{method: "ListActorsPage", scope: "tenant-a", token: "upstream:tenant-a:tenant-a/a-2", size: 2},
		{method: "ListActorsPage", scope: "tenant-c", size: 1},
	}, ateClient.calls[1:])
	assert.Equal(t, "tenant-c", tokenAtespace(t, second.NextPageToken))
}

func token(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// A Substrate actor token, as the released controller passed it through. It names a
// tenant-b session.
const substrateActorToken = `{"v":1,"kind":"actor","scope":"","last":["tenant-b","session-1"]}`

// badActorTokens are tokens a controller that watches tenant-a and tenant-c did not
// issue. None of them may start a walk again at page one.
var badActorTokens = map[string]string{
	"no separator":                `tenant-a`,
	"empty atespace":              `/x`,
	"unwatched atespace":          `tenant-b/x`,
	"worker token":                `u1-tenant-a`,
	"substrate token, raw base64": substrateActorToken,
}

// rejectsToken asserts that a read with the token is a hard InvalidArgument that
// reaches neither ate-api nor the log or the error with a tenant-b name.
func rejectsToken(t *testing.T, ateClient *fakeATEClient, read func(context.Context) error) {
	t.Helper()
	var log bytes.Buffer
	ctx := logging.IntoContext(pagingContext(t), slog.New(slog.NewTextHandler(&log, nil)))
	err := read(ctx)
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), "err = %v", err)
	assert.NotContains(t, err.Error(), "tenant-b")
	assert.NotContains(t, log.String(), "tenant-b")
	assert.Empty(t, ateClient.calls, "a bad token must not reach ate-api")
}

func listActorsWithToken(service *system.Service, pageToken string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2, PageToken: pageToken}})
		return err
	}
}

func TestListSubstrateActorsStrictToken(t *testing.T) {
	tokens := map[string]string{
		"not base64":                    "not base64!",
		"substrate token, std base64":   base64.StdEncoding.EncodeToString([]byte(substrateActorToken)),
		"longer than 1 KiB, else valid": token("tenant-a/" + strings.Repeat("x", 800)),
	}
	for name, raw := range badActorTokens {
		tokens[name] = token(raw)
	}
	for name, pageToken := range tokens {
		t.Run(name, func(t *testing.T) {
			ateClient := &fakeATEClient{}
			service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
			rejectsToken(t, ateClient, listActorsWithToken(service, pageToken))
		})
	}
}

// ate-api checks the token inside a kagent token: a forged one cannot read another
// atespace, and its refusal names nothing.
func TestListSubstrateActorsForeignInnerToken(t *testing.T) {
	ateClient := pagingATEClient()
	service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	page, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{
		PageToken: token("tenant-a/upstream:tenant-b:tenant-b/tenant-b-actor-1"),
	}})
	require.NoError(t, err)
	assert.Empty(t, page.Actors)
	assert.Empty(t, page.NextPageToken)
	assert.NotEmpty(t, page.ATEAPIError)
	assert.NotContains(t, page.ATEAPIError, "tenant-b")
	assert.Equal(t, []ateCall{{method: "ListActorsPage", scope: "tenant-a", token: "upstream:tenant-b:tenant-b/tenant-b-actor-1", size: 50}}, ateClient.calls)
}

// The token names its atespace, not its place in the list, so a token survives a
// change to the watched list or fails loudly, and never reads a new neighbour.
func TestListSubstrateActorsTokenHoldsAtespaceName(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	first := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
	// tenant-a has five actors, so a page of six ends one row into tenant-c.
	page, err := first.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 6}})
	require.NoError(t, err)
	require.Equal(t, "tenant-c", tokenAtespace(t, page.NextPageToken))

	t.Run("a wider list resumes in the same atespace", func(t *testing.T) {
		ateClient := pagingATEClient()
		wider := system.NewService(nil, nil, []string{"tenant-a", "tenant-b", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
		next, err := wider.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 6, PageToken: page.NextPageToken}})
		require.NoError(t, err)
		assert.Equal(t, []string{"tenant-c-actor-2", "tenant-c-actor-3"}, actorNames(next.Actors))
		for _, call := range ateClient.calls {
			assert.Equal(t, "tenant-c", call.scope)
		}
	})

	t.Run("a narrower list refuses it", func(t *testing.T) {
		ateClient := pagingATEClient()
		narrower := system.NewService(nil, nil, []string{"tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)
		rejectsToken(t, ateClient, listActorsWithToken(narrower, page.NextPageToken))
	})
}

// A page that fills at the end of one atespace offers the next one, which may turn out
// to be empty. That last page is accepted: knowing in advance costs a read per page.
func TestListSubstrateActorsEmptyLastPage(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := &fakeATEClient{actors: []*ateapipb.Actor{namedActor("tenant-a", "a-1"), namedActor("tenant-a", "a-2")}}
	service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	pages := walkActors(t, ctx, service, "", 2)
	require.Len(t, pages, 2)
	assert.Equal(t, []string{"a-1", "a-2"}, actorNames(pages[0].Actors))
	assert.Empty(t, pages[1].Actors)
	assert.Len(t, ateClient.calls, 2, "one read for each page")
}

func TestListSubstrateActorsEmptyWatchedAtespace(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	watched := []string{"tenant-a", "empty", "tenant-c"}
	service := system.NewService(nil, nil, watched, &pkgAuth.NoopAuthorizer{}, ateClient)

	assert.Equal(t, ownActorNames(ateClient, watched), walkedActorNames(walkActors(t, ctx, service, "", 2)))
	assert.Equal(t, []string{"empty", "tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
}

// ate-api scopes each list, and the controller still keeps only that atespace's rows.
func TestListSubstrateActorsKeepsRowFilter(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	ateClient.ignoreAtespace = true
	watched := []string{"tenant-a", "tenant-c"}
	service := system.NewService(nil, nil, watched, &pkgAuth.NoopAuthorizer{}, ateClient)

	assert.Equal(t, ownActorNames(ateClient, watched), walkedActorNames(walkActors(t, ctx, service, "", 100)))
	assert.Equal(t, watched, calledScopes(ateClient, "ListActorsPage"))
}

func TestListSubstrateActorsNamedAtespace(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	pages := walkActors(t, ctx, service, "tenant-c", 1)
	assert.Equal(t, ownActorNames(ateClient, []string{"tenant-c"}), walkedActorNames(pages))
	for _, call := range ateClient.calls {
		assert.Equal(t, "tenant-c", call.scope)
	}
	for _, page := range pages[:len(pages)-1] {
		assert.Equal(t, "tenant-c", tokenAtespace(t, page.NextPageToken))
	}
}

// A page whose second read fails returns none of the first read's rows: a short page
// beside a warning would look like the end of the list.
func TestListSubstrateActorsLaterCallFails(t *testing.T) {
	ateClient := &fakeATEClient{
		actors:   []*ateapipb.Actor{namedActor("tenant-a", "a-1"), namedActor("tenant-c", "c-1")},
		failCall: 2,
		failErr:  errors.New("ate-api unreachable"),
	}
	service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	page, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
	require.NoError(t, err)
	assert.Equal(t, "ate-api unreachable", page.ATEAPIError)
	assert.Empty(t, page.Actors)
	assert.Empty(t, page.NextPageToken)
}

// A watched list with a repeat must not read an atespace twice, or offer a token that
// leads back to a page already read.
func TestNewServiceDeduplicatesWatched(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	service := system.NewService(nil, nil, []string{"tenant-c", "tenant-a", "tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	assert.Equal(t, ownActorNames(ateClient, []string{"tenant-a", "tenant-c"}), walkedActorNames(walkActors(t, ctx, service, "", 1)))
	assert.Equal(t, []string{"tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorsPage"), "sorted")
}

// The app passes an empty list for an install that watches every namespace.
func TestNewServiceWatchAllStaysWatchAll(t *testing.T) {
	for name, watched := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			ateClient := pagingATEClient()
			service := system.NewService(nil, nil, watched, &pkgAuth.NoopAuthorizer{}, ateClient)

			page, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 100}})
			require.NoError(t, err)
			assert.Len(t, page.Actors, len(ateClient.actors))
			assert.Equal(t, []ateCall{{method: "ListActorsPage", size: 100}}, ateClient.calls)
		})
	}
}

// The ate-api reads of a full walk, counted by hand from the paging data: tenant-a
// has five actors and tenant-c three.
func TestListSubstrateActorsCallCounts(t *testing.T) {
	ctx := pagingContext(t)
	for limit, want := range map[int32]int{1: 8, 2: 5, 3: 4, 5: 2, 50: 2} {
		t.Run(strconv.Itoa(int(limit)), func(t *testing.T) {
			ateClient := pagingATEClient()
			service := system.NewService(nil, nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

			calls, pageToken := 0, ""
			for range 100 {
				page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
				require.NoError(t, err)
				pageCalls := ateClient.calls[calls:]
				calls += len(pageCalls)
				read := map[string]bool{}
				for _, call := range pageCalls {
					assert.False(t, read[call.scope], "page %v read %s twice", actorNames(page.Actors), call.scope)
					read[call.scope] = true
				}
				if pageToken = page.NextPageToken; pageToken == "" {
					break
				}
			}
			assert.Equal(t, want, calls)
		})
	}
}

// Workers are read one by one, by the UID of a worker pod in a watched namespace.
func TestListSubstrateWorkersFromOwnPods(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	kubeClient := pagingKubeClient(t, interceptor.Funcs{})
	service := system.NewService(kubeClient, kubeClient, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	pages := walkWorkers(t, ctx, service, "", 1)
	require.Len(t, pages, len(ownWorkerUIDs), "the last worker ends the walk")
	assert.Equal(t, ownWorkerUIDs, walkedWorkerUIDs(pages))
	for i, page := range pages[:len(pages)-1] {
		assert.Equal(t, ownWorkerUIDs[i], page.NextPageToken)
	}
	for _, call := range ateClient.calls {
		assert.Equal(t, "GetWorker", call.method)
		assert.Contains(t, ownWorkerUIDs, call.scope)
	}
}

func newPagingWorkerService(t *testing.T, ateClient *fakeATEClient, funcs interceptor.Funcs, pods ...*corev1.Pod) *system.Service {
	kubeClient := pagingKubeClient(t, funcs, pods...)
	return system.NewService(kubeClient, kubeClient, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
}

// A worker pod that is not Ready has no Worker yet. The page fills from later pods.
func TestListSubstrateWorkersSkipsUnregisteredPod(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{}, workerPod("tenant-a", "pool-a", "u2a-tenant-a"))

	pages := walkWorkers(t, pagingContext(t), service, "", 2)
	assert.Equal(t, []string{"u1-tenant-a", "u3-tenant-c"}, walkedWorkerUIDs(pages[:1]))
	assert.Equal(t, ownWorkerUIDs, walkedWorkerUIDs(pages))
}

// A Worker whose pod is gone is not shown, and not read.
func TestListSubstrateWorkersHidesStaleWorker(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.workers = append(ateClient.workers, ateWorker("tenant-a", "u9-tenant-a", 0))
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	assert.Equal(t, ownWorkerUIDs, walkedWorkerUIDs(walkWorkers(t, pagingContext(t), service, "", 100)))
	assert.NotContains(t, ateClient.calls, ateCall{method: "GetWorker", scope: "u9-tenant-a"})
}

// A Worker that names another namespace or another pod than the pod it was read for
// is not that pod's Worker.
func TestListSubstrateWorkersChecksWorkerMatchesPod(t *testing.T) {
	ateClient := pagingATEClient()
	for _, worker := range ateClient.workers {
		switch worker.GetWorkerPodUid() {
		case "u4-tenant-a":
			worker.WorkerNamespace = "tenant-b"
		case "u6-tenant-a":
			worker.WorkerPodUid = "u5-tenant-b"
		}
	}
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	assert.Equal(t, []string{"u1-tenant-a", "u3-tenant-c"}, walkedWorkerUIDs(walkWorkers(t, pagingContext(t), service, "", 100)))
}

// A worker token holds a UID only as a cursor. It is never sent to ate-api.
func TestListSubstrateWorkersForgedToken(t *testing.T) {
	t.Run("a foreign UID only moves the cursor", func(t *testing.T) {
		ateClient := pagingATEClient()
		service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

		page, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{PageToken: "u2-tenant-b"}})
		require.NoError(t, err)
		assert.Equal(t, ownWorkerUIDs[1:], walkedWorkerUIDs([]system.SubstrateWorkerPage{page}))
		for _, call := range ateClient.calls {
			assert.Contains(t, ownWorkerUIDs, call.scope)
		}
	})

	t.Run("longer than 1 KiB", func(t *testing.T) {
		ateClient := pagingATEClient()
		service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})
		rejectsToken(t, ateClient, func(ctx context.Context) error {
			_, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{PageToken: "u2-tenant-b" + strings.Repeat("x", 1024)}})
			return err
		})
	})
}

func TestListSubstrateWorkersNamedNamespace(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	tenantA := []string{"u1-tenant-a", "u4-tenant-a", "u6-tenant-a"}
	assert.Equal(t, tenantA, walkedWorkerUIDs(walkWorkers(t, pagingContext(t), service, "tenant-a", 1)))
	assert.Equal(t, tenantA, calledScopes(ateClient, "GetWorker"))
	assert.Empty(t, calledScopes(ateClient, "ListWorkersPage"))
}

// ate-api error text can name a worker of another namespace. The user sees a fixed text.
func TestListSubstrateWorkersFixedErrorText(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.failCall = 2
	ateClient.failErr = status.Error(codes.Internal, "worker u2-tenant-b is on a node that failed")
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	page, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ate-api could not read a worker: Internal", page.ATEAPIError)
	assert.Empty(t, page.Workers, "no partial page")
	assert.Empty(t, page.NextPageToken)
}

func TestListSubstrateWorkersPodListFails(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{
		List: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectList, ...ctrlclient.ListOption) error {
			return errors.New("etcd is down")
		},
	})

	page, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	assert.NotEmpty(t, page.ATEAPIError)
	assert.Empty(t, page.Workers)
	assert.Empty(t, page.NextPageToken)
	assert.Empty(t, ateClient.calls)
}

// ownSummary is the summary of tenant-a and tenant-c in the paging data.
func assertOwnSummary(t *testing.T, summary system.SubstrateSummary) {
	t.Helper()
	assert.Equal(t, int64(8), summary.ActorCount)
	assert.Equal(t, int64(8), summary.RunningActorCount)
	assert.Equal(t, []string{"tenant-a-template", "tenant-c-template"}, templateNames(summary.ActorTemplates))
	assert.Equal(t, int64(4), summary.WorkerCount)
	assert.Equal(t, int64(2), summary.BusyWorkerCount)
}

// calledScopes lists the atespace or worker of each call to the method.
func calledScopes(client *fakeATEClient, method string) []string {
	scopes := []string{}
	for _, call := range client.calls {
		if call.method == method && !slices.Contains(scopes, call.scope) {
			scopes = append(scopes, call.scope)
		}
	}
	return scopes
}

func TestGetSubstrateSummaryReadsOnlyWatched(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Empty(t, summary.ATEAPIError)
	assertOwnSummary(t, summary)
	assert.Equal(t, []string{"tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
	assert.Equal(t, []string{"tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorTemplates"))
	assert.ElementsMatch(t, ownWorkerUIDs, calledScopes(ateClient, "GetWorker"))
	assert.Empty(t, calledScopes(ateClient, "ListWorkersPage"))
}

func TestGetSubstrateSummaryInputs(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	summary, err := service.GetSubstrateSummary(pagingContext(t), "tenant-a", "tenant-c")
	require.NoError(t, err)
	assert.Equal(t, int64(3), summary.ActorCount)
	assert.Equal(t, int64(3), summary.WorkerCount)
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorTemplates"))
	assert.ElementsMatch(t, []string{"u1-tenant-a", "u4-tenant-a", "u6-tenant-a"}, calledScopes(ateClient, "GetWorker"))
}

// An ate-api that drops the atespace filter cannot show another tenant's template,
// nor one template twice.
func TestGetSubstrateSummaryKeepsTemplateFilter(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.ignoreAtespace = true
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant-a-template", "tenant-c-template"}, templateNames(summary.ActorTemplates))

	summary, err = service.GetSubstrateSummary(pagingContext(t), "", "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant-a-template"}, templateNames(summary.ActorTemplates))
}

// One failed scoped read is a warning. The reads that did not fail still count.
func TestGetSubstrateSummaryPartialFailure(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.failCall = 2 // the tenant-c template list
	ateClient.failErr = errors.New("templates unavailable")
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "templates unavailable", summary.ATEAPIError)
	assert.Empty(t, summary.ActorTemplates)
	assert.Equal(t, int64(8), summary.ActorCount)
	assert.Equal(t, int64(4), summary.WorkerCount)
	assert.Equal(t, int64(2), summary.BusyWorkerCount)
}

// A failed Worker read is a warning. Templates and actors still count.
func TestGetSubstrateSummaryWorkerUnavailable(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.failCall = 5 // the first GetWorker, after two template and two actor lists
	ateClient.failErr = status.Error(codes.Unavailable, "worker u1-tenant-a is unreachable")
	service := newPagingWorkerService(t, ateClient, interceptor.Funcs{})

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "ate-api could not read a worker: Unavailable", summary.ATEAPIError)
	assert.Equal(t, []string{"tenant-a-template", "tenant-c-template"}, templateNames(summary.ActorTemplates))
	assert.Equal(t, int64(8), summary.ActorCount)
	assert.Equal(t, int64(8), summary.RunningActorCount)
	assert.Zero(t, summary.WorkerCount)
}

// A failed worker pod list is a warning too. WorkerPools come from the cached client
// and still list.
func TestGetSubstrateSummaryPodListFails(t *testing.T) {
	ateClient := pagingATEClient()
	podReader := pagingKubeClient(t, interceptor.Funcs{
		List: func(context.Context, ctrlclient.WithWatch, ctrlclient.ObjectList, ...ctrlclient.ListOption) error {
			return errors.New("etcd is down")
		},
	})
	service := system.NewService(pagingKubeClient(t, interceptor.Funcs{}), podReader, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "failed to list worker pods", summary.ATEAPIError)
	assert.Len(t, summary.WorkerPools, 2)
	assert.Equal(t, []string{"tenant-a-template", "tenant-c-template"}, templateNames(summary.ActorTemplates))
	assert.Equal(t, int64(8), summary.ActorCount)
	assert.Zero(t, summary.WorkerCount)
	assert.Empty(t, calledScopes(ateClient, "GetWorker"))
}

// Walks every RPC with each watched list and page size of the paging data. A walk
// reads, and its tokens name, only watched data. It returns each own row once, and no
// page is empty beside a token: that would tell the caller a hidden row exists.
func TestSubstrateWalksStayInWatchedNamespaces(t *testing.T) {
	for _, watched := range [][]string{{"tenant-a", "tenant-c"}, {"tenant-a"}} {
		for _, limit := range []int32{1, 2, 3, 4, 5, 6, 7, 100} {
			t.Run(fmt.Sprintf("%v/%d", watched, limit), func(t *testing.T) {
				ctx := pagingContext(t)
				ateClient := pagingATEClient()
				kubeClient := pagingKubeClient(t, interceptor.Funcs{})
				service := system.NewService(kubeClient, kubeClient, watched, &pkgAuth.NoopAuthorizer{}, ateClient)
				ownWorkerIDs := []string{}
				for _, uid := range ownWorkerUIDs {
					if slices.ContainsFunc(watched, func(namespace string) bool { return strings.HasSuffix(uid, namespace) }) {
						ownWorkerIDs = append(ownWorkerIDs, uid)
					}
				}
				actorPages := walkActors(t, ctx, service, "", limit)
				workerPages := walkWorkers(t, ctx, service, "", limit)
				summary, err := service.GetSubstrateSummary(ctx, "", "")
				require.NoError(t, err)

				for _, call := range ateClient.calls {
					switch call.method {
					case "ListActorsPage", "ListActorTemplates":
						assert.Contains(t, watched, call.scope, "%+v", call)
					case "GetWorker":
						assert.Contains(t, ownWorkerIDs, call.scope, "%+v", call)
					default:
						t.Errorf("unexpected call %+v", call)
					}
				}
				checkPage := func(rows int, pageToken string) {
					// An actor token holds ate-api's inner token as base64 text.
					raw, _ := base64.RawURLEncoding.DecodeString(pageToken)
					assert.NotContains(t, pageToken+string(raw), "tenant-b")
					if rows == 0 {
						assert.Empty(t, pageToken)
					}
				}
				for _, page := range actorPages {
					checkPage(len(page.Actors), page.NextPageToken)
				}
				for _, page := range workerPages {
					checkPage(len(page.Workers), page.NextPageToken)
				}
				assert.Equal(t, ownActorNames(ateClient, watched), walkedActorNames(actorPages))
				assert.Equal(t, ownWorkerIDs, walkedWorkerUIDs(workerPages))
				assert.Equal(t, int64(len(ownActorNames(ateClient, watched))), summary.ActorCount)
				assert.Equal(t, int64(len(ownWorkerIDs)), summary.WorkerCount)
			})
		}
	}
}

// A controller that watches every namespace answers as it did before: ate-api's page,
// and ate-api's token unchanged.
func TestSubstratePassThroughWhenWatchingAll(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	podLists := 0
	kubeClient := pagingKubeClient(t, interceptor.Funcs{
		List: func(ctx context.Context, c ctrlclient.WithWatch, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
			if _, ok := list.(*corev1.PodList); ok {
				podLists++
			}
			return c.List(ctx, list, opts...)
		},
	})
	service := system.NewService(kubeClient, kubeClient, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

	for _, atespace := range []string{"", "tenant-b"} {
		page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: 1}})
		require.NoError(t, err)
		calls := ateClient.calls
		assert.Equal(t, ateCall{method: "ListActorsPage", scope: atespace, size: 1}, calls[len(calls)-1])
		assert.Equal(t, "upstream:"+atespace+":"+page.Actors[0].GetMetadata().GetAtespace()+"/"+page.Actors[0].GetMetadata().GetName(), page.NextPageToken)
	}

	workers, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	assert.Equal(t, "upstream:1", workers.NextPageToken)
	_, err = service.GetSubstrateSummary(ctx, "", "")
	require.NoError(t, err)
	assert.Empty(t, calledScopes(ateClient, "GetWorker"))
	assert.Zero(t, podLists)
}

func TestSubstrateRowsChangeBetweenPages(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	kubeClient := pagingKubeClient(t, interceptor.Funcs{})
	service := system.NewService(kubeClient, kubeClient, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	actors, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Equal(t, []string{"tenant-a-actor-1", "tenant-a-actor-2"}, actorNames(actors.Actors))
	ateClient.actors = slices.DeleteFunc(ateClient.actors, func(actor *ateapipb.Actor) bool { return actor.GetMetadata().GetName() == "tenant-a-actor-4" })
	ateClient.actors = append(ateClient.actors, namedActor("tenant-a", "tenant-a-actor-0"))
	rest := []string{}
	for pageToken := actors.NextPageToken; pageToken != ""; {
		page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		rest, pageToken = append(rest, actorNames(page.Actors)...), page.NextPageToken
	}
	assert.Equal(t, []string{"tenant-a-actor-3", "tenant-a-actor-5", "tenant-c-actor-1", "tenant-c-actor-2", "tenant-c-actor-3"}, rest)

	workers, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Equal(t, ownWorkerUIDs[:1], walkedWorkerUIDs([]system.SubstrateWorkerPage{workers}))
	require.NoError(t, kubeClient.Delete(ctx, workerPod("tenant-a", "pool-a", "u4-tenant-a")))
	require.NoError(t, kubeClient.Create(ctx, workerPod("tenant-a", "pool-a", "u0a-tenant-a")))
	rest = []string{}
	for pageToken := workers.NextPageToken; pageToken != ""; {
		page, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		rest, pageToken = append(rest, walkedWorkerUIDs([]system.SubstrateWorkerPage{page})...), page.NextPageToken
	}
	assert.Equal(t, []string{"u3-tenant-c", "u6-tenant-a"}, rest)
}
