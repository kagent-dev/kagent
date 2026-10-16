package system_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func pagingContext(t *testing.T) context.Context {
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
}

// pagingATEClient holds the paging tests' data: three tenants whose rows mix in
// ate-api's order. Every tenant-b name holds "tenant-b", so a test can look for it in
// any text.
func pagingATEClient() *fakeATEClient {
	client := &fakeATEClient{
		workers: []*ateapipb.Worker{
			ateWorker("tenant-a", "u1-tenant-a", 2),
			ateWorker("tenant-b", "u2-tenant-b", 1),
			ateWorker("tenant-c", "u3-tenant-c", 1),
			ateWorker("tenant-a", "u4-tenant-a", 0),
			ateWorker("tenant-b", "u5-tenant-b", 0),
			ateWorker("tenant-a", "u6-tenant-a", 0),
		},
		// Two workers a page, so a drain of the workers takes three reads.
		workerPageSize: 2,
	}
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
	return client
}

// ownWorkerUIDs are the workers of tenant-a and tenant-c, in name order.
var ownWorkerUIDs = []string{"u1-tenant-a", "u3-tenant-c", "u4-tenant-a", "u6-tenant-a"}

// ateWorker is a Worker as ate-api holds it: named after its pod's UID.
func ateWorker(namespace, uid string, allocatedActors int32) *ateapipb.Worker {
	return &ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: uid},
		WorkerNamespace: namespace,
		WorkerPod:       "pod-" + uid,
		WorkerPodUid:    uid,
		Status:          &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: allocatedActors}},
	}
}

// pagingKubeClient holds a WorkerPool in each tenant.
func pagingKubeClient(t *testing.T, funcs interceptor.Funcs) ctrlclient.WithWatch {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	objects := []ctrlclient.Object{}
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		objects = append(objects, &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: tenant, Name: "pool"}})
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

// ownWorkerService watches tenant-a and tenant-c. It has no Kubernetes client: the
// workers view reads ate-api only.
func ownWorkerService(ateClient system.ATEClient) *system.Service {
	return system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
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

// listWorkers reads one page of workers in every watched namespace.
func listWorkers(t *testing.T, service *system.Service, limit int32, pageToken string) system.SubstrateWorkerPage {
	t.Helper()
	page, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
	require.NoError(t, err)
	return page
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
	service := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

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

// A token this controller did not issue is a hard InvalidArgument. It never reaches
// ate-api, never starts a walk again at page one, and its error names no tenant-b row.
func TestListSubstrateActorsStrictToken(t *testing.T) {
	for name, pageToken := range map[string]string{
		"not base64":         "not base64!",
		"no separator":       token("tenant-a"),
		"empty atespace":     token("/x"),
		"unwatched atespace": token("tenant-b/x"),
		// A Substrate actor token, as the released controller passed it through.
		"substrate token":               token(`{"v":1,"kind":"actor","scope":"","last":["tenant-b","session-1"]}`),
		"longer than 1 KiB, else valid": token("tenant-a/" + strings.Repeat("x", 800)),
	} {
		t.Run(name, func(t *testing.T) {
			ateClient := &fakeATEClient{}
			service := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
			_, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2, PageToken: pageToken}})
			require.Error(t, err)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), "err = %v", err)
			assert.NotContains(t, err.Error(), "tenant-b")
			assert.Empty(t, ateClient.calls, "a bad token must not reach ate-api")
		})
	}
}

// The token names its atespace, not its place in the watched list, so it resumes in the
// same atespace after the list grows.
func TestListSubstrateActorsTokenHoldsAtespaceName(t *testing.T) {
	ctx := pagingContext(t)
	first := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, pagingATEClient())
	// tenant-a has five actors, so a page of six ends one row into tenant-c.
	page, err := first.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 6}})
	require.NoError(t, err)

	ateClient := pagingATEClient()
	wider := system.NewService(nil, []string{"tenant-a", "tenant-b", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
	next, err := wider.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 6, PageToken: page.NextPageToken}})
	require.NoError(t, err)
	assert.Equal(t, []string{"tenant-c-actor-2", "tenant-c-actor-3"}, actorNames(next.Actors))
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
}

func TestListSubstrateActorsNamedAtespace(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	service := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	pages := walkActors(t, ctx, service, "tenant-c", 1)
	assert.Equal(t, []string{"tenant-c-actor-1", "tenant-c-actor-2", "tenant-c-actor-3"}, walkedActorNames(pages))
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
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
	service := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	page, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
	require.NoError(t, err)
	assert.Equal(t, "ate-api unreachable", page.ATEAPIError)
	assert.Empty(t, page.Actors)
	assert.Empty(t, page.NextPageToken)
}

// The ate-api reads of a full walk, counted by hand from the paging data: tenant-a
// has five actors and tenant-c three.
func TestListSubstrateActorsCallCounts(t *testing.T) {
	for limit, want := range map[int32]int{1: 8, 2: 5, 3: 4, 5: 2, 50: 2} {
		t.Run(strconv.Itoa(int(limit)), func(t *testing.T) {
			ateClient := pagingATEClient()
			service := system.NewService(nil, []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
			walkActors(t, pagingContext(t), service, "", limit)
			assert.Len(t, ateClient.calls, want)
		})
	}
}

// ate-api error text can name a worker of another namespace. The user sees a fixed text.
func TestListSubstrateWorkersFixedErrorText(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.failCall = 2 // the second page of the drain
	ateClient.failErr = status.Error(codes.Internal, "worker u2-tenant-b is on a node that failed")

	page := listWorkers(t, ownWorkerService(ateClient), 0, "")
	assert.Equal(t, "ate-api could not list workers: Internal", page.ATEAPIError)
	assert.Empty(t, page.Workers, "no partial page")
	assert.Empty(t, page.NextPageToken)
}

// A named namespace narrows the view. A watched namespace with no worker is one empty
// page with no token.
func TestListSubstrateWorkersNamedNamespace(t *testing.T) {
	for namespace, want := range map[string][]string{
		"tenant-a": {"u1-tenant-a", "u4-tenant-a", "u6-tenant-a"},
		"tenant-d": {},
	} {
		t.Run(namespace, func(t *testing.T) {
			service := system.NewService(nil, []string{"tenant-a", "tenant-c", "tenant-d"}, &pkgAuth.NoopAuthorizer{}, pagingATEClient())
			pages := walkWorkers(t, pagingContext(t), service, namespace, 1)
			assert.Equal(t, want, walkedWorkerUIDs(pages))
			assert.Len(t, pages, max(1, len(want)))
		})
	}
}

// endlessATEClient answers each worker page with no rows and the next token it is given.
type endlessATEClient struct {
	fakeATEClient
	next func(read int) string
}

func (client *endlessATEClient) ListWorkersPage(context.Context, int32, string) ([]*ateapipb.Worker, string, error) {
	client.workerReads++
	return nil, client.next(client.workerReads), nil
}

// ate-api faults that stop a drain: a repeated token, which the drain error quotes, and
// a token that never ends.
func TestListSubstrateWorkersEndlessPages(t *testing.T) {
	for name, tc := range map[string]struct {
		next  func(read int) string
		reads int
	}{
		"repeated token": {func(int) string { return "upstream:u2-tenant-b" }, 2},
		"endless tokens": {func(read int) string { return "upstream:" + strconv.Itoa(read) }, 10_000},
	} {
		t.Run(name, func(t *testing.T) {
			ateClient := &endlessATEClient{next: tc.next}
			page := listWorkers(t, ownWorkerService(ateClient), 0, "")
			assert.Equal(t, "ate-api could not list workers: Unknown", page.ATEAPIError)
			assert.Empty(t, page.NextPageToken)
			assert.Equal(t, tc.reads, ateClient.workerReads)
		})
	}
}

// A page holds 50 workers by default, in name order even when ate-api's is not.
func TestListSubstrateWorkersDefaultPageSize(t *testing.T) {
	ateClient := &fakeATEClient{workerPageSize: 2}
	for i := 59; i >= 0; i-- {
		ateClient.workers = append(ateClient.workers, ateWorker("tenant-a", fmt.Sprintf("w%02d-tenant-a", i), 0))
	}
	page := listWorkers(t, ownWorkerService(ateClient), 0, "")
	require.Len(t, page.Workers, 50)
	assert.Equal(t, "w00-tenant-a", page.Workers[0].GetWorkerPodUid())
	assert.Equal(t, "w49-tenant-a", page.NextPageToken)
}

// Any token is a cursor: an actor token, a foreign worker name or a name past the end
// moves it, and no row, token or error names tenant-b. A worker added or deleted
// between pages moves it the same way.
func TestListSubstrateWorkersTokenIsCursor(t *testing.T) {
	for name, tc := range map[string]struct {
		pageToken string
		want      []string
	}{
		"actor token":     {token("tenant-a/upstream:tenant-b:tenant-b/tenant-b-actor-1"), ownWorkerUIDs},
		"foreign worker":  {"u2-tenant-b", []string{"u3-tenant-c", "u4-tenant-a", "u6-tenant-a"}},
		"after every row": {"zzz", []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			page := listWorkers(t, ownWorkerService(pagingATEClient()), 50, tc.pageToken)
			assert.Equal(t, tc.want, walkedWorkerUIDs([]system.SubstrateWorkerPage{page}))
			assert.Empty(t, page.NextPageToken)
			assert.Empty(t, page.ATEAPIError)
		})
	}
}

// newPagingWorkerService is ownWorkerService with WorkerPools for the summary.
func newPagingWorkerService(t *testing.T, ateClient system.ATEClient) *system.Service {
	return system.NewService(pagingKubeClient(t, interceptor.Funcs{}), []string{"tenant-a", "tenant-c"}, &pkgAuth.NoopAuthorizer{}, ateClient)
}

// calledScopes lists the atespace of each call to the method, once each.
func calledScopes(client *fakeATEClient, method string) []string {
	scopes := []string{}
	for _, call := range client.calls {
		if call.method == method && !slices.Contains(scopes, call.scope) {
			scopes = append(scopes, call.scope)
		}
	}
	return scopes
}

// The summary and the actor list read only watched atespaces, and keep only their rows
// even from an ate-api that drops the atespace filter.
func TestSubstrateReadsOnlyWatched(t *testing.T) {
	for _, ignoreAtespace := range []bool{false, true} {
		t.Run("ignoreAtespace="+strconv.FormatBool(ignoreAtespace), func(t *testing.T) {
			ctx := pagingContext(t)
			ateClient := pagingATEClient()
			ateClient.ignoreAtespace = ignoreAtespace
			service := newPagingWorkerService(t, ateClient)

			summary, err := service.GetSubstrateSummary(ctx, "", "")
			require.NoError(t, err)
			assert.Empty(t, summary.ATEAPIError)
			assert.Equal(t, int64(8), summary.ActorCount)
			assert.Equal(t, int64(8), summary.RunningActorCount)
			assert.Equal(t, []string{"tenant-a-template", "tenant-c-template"}, templateNames(summary.ActorTemplates))
			assert.Equal(t, int64(4), summary.WorkerCount)
			assert.Equal(t, int64(2), summary.BusyWorkerCount)
			assert.Equal(t, []string{"tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
			assert.Equal(t, []string{"tenant-a", "tenant-c"}, calledScopes(ateClient, "ListActorTemplates"))
			assert.Equal(t, 3, ateClient.workerReads, "one drain")

			assert.Equal(t, ownActorNames(ateClient, []string{"tenant-a", "tenant-c"}), walkedActorNames(walkActors(t, ctx, service, "", 100)))
		})
	}
}

func TestGetSubstrateSummaryInputs(t *testing.T) {
	ateClient := pagingATEClient()
	service := newPagingWorkerService(t, ateClient)

	summary, err := service.GetSubstrateSummary(pagingContext(t), "tenant-a", "tenant-c")
	require.NoError(t, err)
	assert.Equal(t, int64(3), summary.ActorCount)
	assert.Equal(t, int64(3), summary.WorkerCount)
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorsPage"))
	assert.Equal(t, []string{"tenant-c"}, calledScopes(ateClient, "ListActorTemplates"))

	summary, err = service.GetSubstrateSummary(pagingContext(t), "tenant-c", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.WorkerCount)
	assert.Equal(t, int64(1), summary.BusyWorkerCount)
}

// A failed second template list drops the first one's templates too. The other reads
// still count.
func TestGetSubstrateSummaryPartialFailure(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.failCall = 2 // the tenant-c template list
	ateClient.failErr = errors.New("templates unavailable")
	service := newPagingWorkerService(t, ateClient)

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "templates unavailable", summary.ATEAPIError)
	assert.Empty(t, summary.ActorTemplates)
	assert.Equal(t, int64(8), summary.ActorCount)
	assert.Equal(t, int64(4), summary.WorkerCount)
}

// Walks every RPC with each watched list and page size of the paging data. A walk
// reads, and its tokens name, only watched data. It returns each own row once, and no
// page is empty beside a token: that would tell the caller a hidden row exists. The
// lists cover a repeat, which must not read an atespace twice, and a watched atespace
// with no rows.
func TestSubstrateWalksStayInWatchedNamespaces(t *testing.T) {
	for _, watched := range [][]string{{"tenant-a", "tenant-c"}, {"tenant-a"}, {"tenant-c", "tenant-a", "tenant-a"}, {"tenant-a", "empty", "tenant-c"}} {
		for _, limit := range []int32{1, 2, 3, 4, 5, 6, 7, 100} {
			t.Run(fmt.Sprintf("%v/%d", watched, limit), func(t *testing.T) {
				ctx := pagingContext(t)
				ateClient := pagingATEClient()
				kubeClient := pagingKubeClient(t, interceptor.Funcs{})
				service := system.NewService(kubeClient, watched, &pkgAuth.NoopAuthorizer{}, ateClient)
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
					case "ListWorkersPage":
						// A drain sends only ate-api's own tokens.
						assert.Contains(t, []string{"", "upstream:2", "upstream:4"}, call.token, "%+v", call)
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

// A controller that watches every namespace (the app passes an empty list) answers as
// it did before: ate-api's page, token and error text unchanged.
func TestSubstratePassThroughWhenWatchingAll(t *testing.T) {
	ctx := pagingContext(t)
	ateClient := pagingATEClient()
	service := system.NewService(nil, []string{}, &pkgAuth.NoopAuthorizer{}, ateClient)

	for _, atespace := range []string{"", "tenant-b"} {
		page, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: 1}})
		require.NoError(t, err)
		calls := ateClient.calls
		assert.Equal(t, ateCall{method: "ListActorsPage", scope: atespace, size: 1}, calls[len(calls)-1])
		assert.Equal(t, "upstream:"+atespace+":"+page.Actors[0].GetMetadata().GetAtespace()+"/"+page.Actors[0].GetMetadata().GetName(), page.NextPageToken)
	}

	page := listWorkers(t, service, 1, "upstream:2")
	assert.Equal(t, []string{"u3-tenant-c"}, walkedWorkerUIDs([]system.SubstrateWorkerPage{page}))
	assert.Equal(t, "upstream:3", page.NextPageToken)
	assert.Equal(t, ateCall{method: "ListWorkersPage", token: "upstream:2", size: 1}, ateClient.calls[len(ateClient.calls)-1])

	ateClient.err = errors.New("worker u2-tenant-b is down")
	assert.Equal(t, "worker u2-tenant-b is down", listWorkers(t, service, 1, "").ATEAPIError)
}
