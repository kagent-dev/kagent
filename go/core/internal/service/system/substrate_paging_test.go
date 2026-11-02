package system_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func pagingContext(t *testing.T) context.Context {
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})
}

func pagingATEClient() *fakeATEClient {
	client := &fakeATEClient{pageSize: 2}
	for _, actor := range []string{"tenant-a/a-1", "tenant-b/b-1", "tenant-c/c-1", "tenant-a/a-2", "tenant-b/b-2", "tenant-c/c-2", "tenant-a/a-3"} {
		atespace, name, _ := strings.Cut(actor, "/")
		client.actors = append(client.actors, substrateActor(name, atespace, ateapipb.ActorState_ACTOR_STATE_RUNNING, "", ""))
	}
	for _, worker := range []string{"tenant-a/u4", "tenant-b/u2", "tenant-a/u1", "tenant-c/u3", "tenant-a/u6", "tenant-b/u5"} {
		namespace, uid, _ := strings.Cut(worker, "/")
		client.workers = append(client.workers, &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: uid + "-" + namespace}, WorkerNamespace: namespace})
	}
	return client
}

func pagingService(ateClient system.ATEClient) *system.Service {
	return system.NewService(nil, []string{"tenant-c", "tenant-a", "tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)
}

// tokenText is a page token with every base64 layer under it decoded, so a row key
// nested in an ate-api cursor inside a kagent cursor is still visible.
func tokenText(token string) string {
	text := token
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.StdEncoding} {
		if raw, err := encoding.DecodeString(token); err == nil && len(raw) > 0 {
			text += "\n" + tokenText(string(raw))
		}
	}
	if _, inner, ok := strings.Cut(token, "/"); ok {
		text += "\n" + tokenText(inner)
	}
	return text
}

func assertOwnToken(t *testing.T, rows int, pageToken string) {
	t.Helper()
	assert.NotContains(t, tokenText(pageToken), "tenant-b")
	if rows == 0 {
		assert.Empty(t, pageToken, "an empty page must not lead on")
	}
}

func walkActorNames(t *testing.T, service *system.Service, atespace string, limit int32) []string {
	t.Helper()
	names := []string{}
	pageToken := ""
	for range 100 {
		page, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		assertOwnToken(t, len(page.Actors), page.NextPageToken)
		names = append(names, actorNames(page.Actors)...)
		if pageToken = page.NextPageToken; pageToken == "" {
			return names
		}
	}
	t.Fatal("the walk did not end")
	return nil
}

func TestListSubstrateActorsPagesWatchedAtespaces(t *testing.T) {
	for atespace, want := range map[string][]string{
		"":         {"a-1", "a-2", "a-3", "c-1", "c-2"},
		"tenant-c": {"c-1", "c-2"},
	} {
		for _, limit := range []int32{1, 2, 3, 4, 100} {
			t.Run(fmt.Sprintf("%q/%d", atespace, limit), func(t *testing.T) {
				ateClient := pagingATEClient()
				assert.Equal(t, want, walkActorNames(t, pagingService(ateClient), atespace, limit))
				assert.Subset(t, []string{"tenant-a", "tenant-c"}, ateClient.actorAtespaces, "every ate-api read is scoped to a watched atespace")
			})
		}
	}
}

func actorNames(actors []*ateapipb.Actor) []string {
	names := []string{}
	for _, actor := range actors {
		names = append(names, actor.GetMetadata().GetName())
	}
	return names
}

func scopedPageToken(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func TestListSubstrateActorsRefusesForeignTokens(t *testing.T) {
	for name, tc := range map[string]struct {
		atespace  string
		pageToken string
	}{
		"not base64":            {pageToken: "upstream:2"},
		"no separator":          {pageToken: scopedPageToken("tenant-a")},
		"empty atespace":        {pageToken: scopedPageToken("/upstream:2")},
		"unwatched atespace":    {pageToken: scopedPageToken("tenant-b/upstream:2")},
		"outside the request":   {atespace: "tenant-c", pageToken: scopedPageToken("tenant-a/upstream:2")},
		"ate-api actor token":   {pageToken: scopedPageToken(`{"v":1,"kind":"actor","last":["tenant-b","b-1"]}`)},
		"over 1 KiB, else good": {pageToken: scopedPageToken("tenant-a/" + strings.Repeat("x", 800))},
	} {
		t.Run(name, func(t *testing.T) {
			ateClient := pagingATEClient()
			_, err := pagingService(ateClient).ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Atespace: tc.atespace, Page: &apiv1.PageRequest{Limit: 2, PageToken: tc.pageToken}})
			require.Error(t, err)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
			assert.NotContains(t, err.Error(), "tenant-b")
			assert.NotContains(t, err.Error(), tc.pageToken)
			assert.Zero(t, ateClient.actorReads)
		})
	}
}

func TestListSubstrateWorkersRefusesForeignTokens(t *testing.T) {
	for name, tc := range map[string]struct {
		namespace string
		pageToken string
	}{
		"not base64":            {pageToken: "upstream:2"},
		"ate-api worker token":  {pageToken: fakePageToken("u2-tenant-b", 2)},
		"bare worker name":      {pageToken: scopedPageToken("u1-tenant-a")},
		"no worker name":        {pageToken: scopedPageToken("tenant-a/")},
		"unwatched namespace":   {pageToken: scopedPageToken("tenant-b/u2-tenant-b")},
		"outside the request":   {namespace: "tenant-c", pageToken: scopedPageToken("tenant-a/u1-tenant-a")},
		"over 1 KiB, else good": {pageToken: scopedPageToken("tenant-a/" + strings.Repeat("x", 800))},
	} {
		t.Run(name, func(t *testing.T) {
			ateClient := pagingATEClient()
			_, err := pagingService(ateClient).ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Namespace: tc.namespace, Page: &apiv1.PageRequest{Limit: 2, PageToken: tc.pageToken}})
			require.Error(t, err)
			assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
			assert.NotContains(t, err.Error(), "tenant-b")
			assert.NotContains(t, err.Error(), tc.pageToken)
			assert.Zero(t, ateClient.workerReads)
		})
	}
}

func walkWorkerNames(t *testing.T, service *system.Service, namespace string, limit int32) []string {
	t.Helper()
	names := []string{}
	pageToken := ""
	for range 100 {
		page, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Namespace: namespace, Page: &apiv1.PageRequest{Limit: limit, PageToken: pageToken}})
		require.NoError(t, err)
		require.Empty(t, page.ATEAPIError)
		assertOwnToken(t, len(page.Workers), page.NextPageToken)
		for _, worker := range page.Workers {
			names = append(names, worker.GetMetadata().GetName())
		}
		if pageToken = page.NextPageToken; pageToken == "" {
			return names
		}
	}
	t.Fatal("the walk did not end")
	return nil
}

func TestListSubstrateWorkersPagesWatchedNamespaces(t *testing.T) {
	for namespace, want := range map[string][]string{
		"":         {"u1-tenant-a", "u3-tenant-c", "u4-tenant-a", "u6-tenant-a"},
		"tenant-a": {"u1-tenant-a", "u4-tenant-a", "u6-tenant-a"},
	} {
		for _, limit := range []int32{1, 2, 3, 100} {
			t.Run(fmt.Sprintf("%q/%d", namespace, limit), func(t *testing.T) {
				assert.Equal(t, want, walkWorkerNames(t, pagingService(pagingATEClient()), namespace, limit))
			})
		}
	}
}

func TestListSubstrateWorkersHidesATEAPIErrorText(t *testing.T) {
	ateClient := pagingATEClient()
	ateClient.err = status.Error(codes.Internal, "worker u2-tenant-b is down")

	page, err := pagingService(ateClient).ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	assert.Equal(t, "ate-api could not list workers: Internal", page.ATEAPIError)
	assert.Empty(t, page.Workers)
	assert.Empty(t, page.NextPageToken)
}

func TestGetSubstrateSummaryHidesATEAPIWorkerErrorText(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, atev1.AddToScheme(scheme))
	ateClient := &failingWorkersATEClient{fakeATEClient: *pagingATEClient()}
	ateClient.workerErr = status.Error(codes.Internal, "worker u2-tenant-b is down")
	service := system.NewService(fake.NewClientBuilder().WithScheme(scheme).Build(), []string{"tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "ate-api could not read inventory: Internal", summary.ATEAPIError)
	assert.Equal(t, int64(3), summary.ActorCount, "the actor walk is independent of the worker walk")
}

func TestSubstrateForgedTokensStayInWatchedNamespaces(t *testing.T) {
	forged := []string{
		scopedPageToken("tenant-b/"),
		scopedPageToken("tenant-a/" + fakePageToken("tenant-b/b-1", 1)),
		scopedPageToken("tenant-a/" + fakePageToken("", -5)),
		scopedPageToken("tenant-a/" + fakePageToken("", 1<<40)),
		scopedPageToken("/" + fakePageToken("", 0)),
		fakePageToken("tenant-a/a-1", 1),
		"!!!",
	}
	random := rand.New(rand.NewPCG(1, 1))
	for range 300 {
		raw := make([]byte, random.IntN(80))
		for i := range raw {
			raw[i] = "tenant-abc/xyz=+"[random.IntN(16)]
		}
		forged = append(forged, scopedPageToken(string(raw)), string(raw))
	}

	ateClient := pagingATEClient()
	service := pagingService(ateClient)
	watched := []string{"tenant-a", "tenant-c"}
	for _, scope := range []string{"", "tenant-a", "tenant-c"} {
		for _, pageToken := range forged {
			actors, err := service.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Atespace: scope, Page: &apiv1.PageRequest{Limit: 3, PageToken: pageToken}})
			if err != nil {
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
			}
			for _, actor := range actors.Actors {
				require.Contains(t, watched, actor.GetMetadata().GetAtespace())
			}
			require.NotContains(t, tokenText(actors.NextPageToken), "tenant-b")

			workers, err := service.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Namespace: scope, Page: &apiv1.PageRequest{Limit: 3, PageToken: pageToken}})
			if err != nil {
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument), err)
			}
			for _, worker := range workers.Workers {
				require.Contains(t, watched, worker.GetWorkerNamespace())
			}
			require.NotContains(t, tokenText(workers.NextPageToken), "tenant-b")
		}
	}
	assert.Subset(t, watched, ateClient.actorAtespaces, "every ate-api actor read is scoped to a watched atespace")
}

func TestSubstrateLongestNamesFitThePageToken(t *testing.T) {
	namespace := strings.Repeat("n", 63)
	ateClient := &fakeATEClient{pageSize: 1}
	for _, suffix := range []string{"0", "1"} {
		ateClient.actors = append(ateClient.actors, substrateActor(strings.Repeat("a", 252)+suffix, namespace, ateapipb.ActorState_ACTOR_STATE_RUNNING, "", ""))
		ateClient.workers = append(ateClient.workers, &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: strings.Repeat("w", 252) + suffix}, WorkerNamespace: namespace})
	}
	service := system.NewService(nil, []string{namespace}, &pkgAuth.NoopAuthorizer{}, ateClient)

	assert.Len(t, walkActorNames(t, service, "", 1), 2)
	assert.Len(t, walkWorkerNames(t, service, "", 1), 2)
}

func TestInventoryCursorsResumeAfterReturnedIdentity(t *testing.T) {
	for _, watched := range [][]string{nil, {"team"}} {
		for _, requested := range []string{"", "team"} {
			t.Run(fmt.Sprintf("watched=%v/requested=%s", watched, requested), func(t *testing.T) {
				ate := &fakeATEClient{pageSize: 1}
				for _, name := range []string{"a-hidden", "b-visible", "c-hidden", "d-visible", "e-hidden", "f-visible", "z-hidden"} {
					ate.actors = append(ate.actors, substrateActor(name, "team", 0, "", ""))
					ate.workers = append(ate.workers, &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: name}, WorkerNamespace: "team"})
				}
				policy := inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{}}
				for resource, location := range map[string]string{"SubstrateActor": apiauthorization.AttributeAtespace, "SubstrateWorker": apiauthorization.AttributeNamespace} {
					scope := inventoryScope(location, "team", "b-visible")
					scope.AnyOf[0].All[1].Values = []string{"b-visible", "d-visible", "f-visible"}
					policy.scopes[resource] = scope
				}
				svc := system.NewService(nil, watched, policy, ate)
				ctx := pagingContext(t)
				actors, err := svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: requested, Page: &apiv1.PageRequest{Limit: 2}})
				require.NoError(t, err)
				require.Empty(t, actors.ATEAPIError)
				require.Equal(t, []string{"b-visible", "d-visible"}, actorNames(actors.Actors))
				require.Contains(t, tokenText(actors.NextPageToken), "d-visible")
				require.NotContains(t, tokenText(actors.NextPageToken), "hidden")
				require.NotContains(t, tokenText(actors.NextPageToken), "f-visible", "lookahead must not become the public boundary")
				workers, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: requested, Page: &apiv1.PageRequest{Limit: 2}})
				require.NoError(t, err)
				require.Empty(t, workers.ATEAPIError)
				require.Len(t, workers.Workers, 2)
				require.Equal(t, "d-visible", workers.Workers[1].GetMetadata().GetName())
				require.Contains(t, tokenText(workers.NextPageToken), "d-visible")
				require.NotContains(t, tokenText(workers.NextPageToken), "hidden")
				require.NotContains(t, tokenText(workers.NextPageToken), "f-visible")

				// A deleted boundary and a revoked permission do not prevent forward paging.
				ate.actors = slices.DeleteFunc(ate.actors, func(a *ateapipb.Actor) bool { return a.GetMetadata().GetName() == "d-visible" })
				ate.workers = slices.DeleteFunc(ate.workers, func(w *ateapipb.Worker) bool { return w.GetMetadata().GetName() == "d-visible" })
				policy.scopes["SubstrateActor"] = inventoryScope(apiauthorization.AttributeAtespace, "team", "f-visible")
				policy.scopes["SubstrateWorker"] = inventoryScope(apiauthorization.AttributeNamespace, "team", "f-visible")
				ate.actorTokens, ate.workerTokens = nil, nil
				// Reconstructing the service demonstrates that no process-local cursor state is needed.
				svc = system.NewService(nil, watched, policy, ate)
				nextActors, err := svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: requested, Page: &apiv1.PageRequest{Limit: 2, PageToken: actors.NextPageToken}})
				require.NoError(t, err)
				require.Empty(t, nextActors.ATEAPIError)
				require.Equal(t, []string{"f-visible"}, actorNames(nextActors.Actors))
				require.Empty(t, nextActors.NextPageToken, "trailing denied rows must not create a continuation")
				require.NotEmpty(t, ate.actorTokens[0], "continuation must not rescan from the beginning")
				require.Contains(t, tokenText(ate.actorTokens[0]), "d-visible")
				nextWorkers, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: requested, Page: &apiv1.PageRequest{Limit: 2, PageToken: workers.NextPageToken}})
				require.NoError(t, err)
				require.Empty(t, nextWorkers.ATEAPIError)
				require.Len(t, nextWorkers.Workers, 1)
				require.Equal(t, "f-visible", nextWorkers.Workers[0].GetMetadata().GetName())
				require.Empty(t, nextWorkers.NextPageToken)
				require.NotEmpty(t, ate.workerTokens[0])
				require.Contains(t, tokenText(ate.workerTokens[0]), "d-visible")

				// Revoking the remaining visible row takes effect on the same cursor.
				policy.scopes["SubstrateActor"] = inventoryScope(apiauthorization.AttributeAtespace, "team", "b-visible")
				policy.scopes["SubstrateWorker"] = inventoryScope(apiauthorization.AttributeNamespace, "team", "b-visible")
				revokedActors, err := svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: requested, Page: &apiv1.PageRequest{Limit: 2, PageToken: actors.NextPageToken}})
				require.NoError(t, err)
				require.Empty(t, revokedActors.ATEAPIError)
				require.Empty(t, revokedActors.Actors)
				require.Empty(t, revokedActors.NextPageToken)
				revokedWorkers, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: requested, Page: &apiv1.PageRequest{Limit: 2, PageToken: workers.NextPageToken}})
				require.NoError(t, err)
				require.Empty(t, revokedWorkers.ATEAPIError)
				require.Empty(t, revokedWorkers.Workers)
				require.Empty(t, revokedWorkers.NextPageToken)

				_, err = svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: requested, Page: &apiv1.PageRequest{PageToken: actors.NextPageToken}})
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
				otherScope := "team"
				if requested != "" {
					otherScope = ""
				}
				_, err = svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: otherScope, Page: &apiv1.PageRequest{PageToken: actors.NextPageToken}})
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
			})
		}
	}
}

// Override only the backend paging operation so scan behavior is exercised through
// the public service, without exposing its private collection helper.
type inventoryScanATEClient struct {
	fakeATEClient
	read func(context.Context, int32, string) ([]*ateapipb.Worker, string, error)
}

var _ system.ATEClient = (*inventoryScanATEClient)(nil)

func (i *inventoryScanATEClient) ListWorkersPage(ctx context.Context, size int32, token string) ([]*ateapipb.Worker, string, error) {
	return i.read(ctx, size, token)
}

func TestInventoryScanFailureDiscardsPartialPage(t *testing.T) {
	for _, failure := range []string{"backend", "repeated token", "cycle", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(pagingContext(t))
			defer cancel()
			reads := 0
			ate := &inventoryScanATEClient{read: func(context.Context, int32, string) ([]*ateapipb.Worker, string, error) {
				reads++
				// Fail rather than hang if the production scan bound stops working.
				require.Less(t, reads, 20_000)
				if reads == 1 {
					return []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "visible"}, WorkerNamespace: "team"}}, "private-position", nil
				}
				switch failure {
				case "backend":
					return nil, "", errors.New("backend failed at private-position")
				case "repeated token":
					return nil, "private-position", nil
				case "cycle":
					return nil, strconv.Itoa(reads % 2), nil
				default:
					cancel()
					return nil, "another-position", nil
				}
			}}
			svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
			page, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1}})
			require.NoError(t, err)
			require.NotEmpty(t, page.ATEAPIError)
			require.Empty(t, page.Workers)
			require.Empty(t, page.NextPageToken)
			require.NotContains(t, page.ATEAPIError, "private-position")
			require.GreaterOrEqual(t, reads, 2)
		})
	}
}

func TestInventoryScanFollowsEmptyBackendPages(t *testing.T) {
	reads := 0
	ate := &inventoryScanATEClient{read: func(_ context.Context, _ int32, token string) ([]*ateapipb.Worker, string, error) {
		reads++
		if reads == 1 {
			require.Empty(t, token)
			return nil, "next", nil
		}
		require.Equal(t, "next", token)
		return []*ateapipb.Worker{
			{Metadata: &ateapipb.ResourceMetadata{Name: "denied"}, WorkerNamespace: "other"},
			{Metadata: &ateapipb.ResourceMetadata{Name: "visible"}, WorkerNamespace: "team"},
		}, "", nil
	}}
	svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
	page, err := svc.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Empty(t, page.ATEAPIError)
	require.Len(t, page.Workers, 1)
	require.Equal(t, "visible", page.Workers[0].GetMetadata().GetName())
	require.Empty(t, page.NextPageToken)
	require.Equal(t, 2, reads)
}

func TestListSubstrateActors(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})

	newService := func(client system.ATEClient) *system.Service {
		return system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, client)
	}

	t.Run("answers with one page and the token for the next", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, "", ""),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
		require.NoError(t, err)
		require.Len(t, page.Actors, 2)
		// Preserve upstream order, including across different actor states.
		assert.Equal(t, []string{"actor-1", "actor-2"}, []string{page.Actors[0].GetMetadata().GetName(), page.Actors[1].GetMetadata().GetName()})
		assert.NotEmpty(t, page.NextPageToken)
		assert.Equal(t, 2, ateClient.actorReads, "look ahead to establish an authorized next page")
		assert.False(t, page.ComputedAt.IsZero())
	})

	t.Run("continues from the token it was given", func(t *testing.T) {
		ateClient := &fakeATEClient{pageSize: 2, actors: []*ateapipb.Actor{
			substrateActor("actor-1", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-2", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-0"),
			substrateActor("actor-3", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "team", "worker-1"),
		}}
		service := newService(ateClient)

		first, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 2}})
		require.NoError(t, err)
		second, err := service.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{
			Page: &apiv1.PageRequest{Limit: 2, PageToken: first.NextPageToken},
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

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: "team", Page: &apiv1.PageRequest{Limit: 10}})
		require.NoError(t, err)
		require.Len(t, page.Actors, 1)
		assert.Equal(t, "actor-1", page.Actors[0].GetMetadata().GetName())
	})

	t.Run("an ate-api failure is an empty page beside a warning", func(t *testing.T) {
		ateClient := &fakeATEClient{err: errors.New("ate-api unreachable")}

		page, err := newService(ateClient).ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
		require.NoError(t, err)
		assert.Empty(t, page.Actors)
		assert.Equal(t, "ate-api could not list actors: Unknown", page.ATEAPIError)
		// A failed page offers no continuation.
		assert.Empty(t, page.NextPageToken)
	})

	t.Run("authorizes", func(t *testing.T) {
		denied := system.NewService(nil, nil, systemDenyAuthorizer{}, &fakeATEClient{})
		_, err := denied.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), err)
	})
}

func TestListSubstrateWorkers(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "user"}})

	ateClient := &fakeATEClient{pageSize: 2, workers: []*ateapipb.Worker{
		{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-0"}, WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-0", Status: &ateapipb.WorkerStatus{Allocated: &ateapipb.WorkerResources{Actors: 2}}},
		{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-1"}, WorkerNamespace: "other", WorkerPool: "pool", WorkerPod: "worker-1"},
		{Metadata: &ateapipb.ResourceMetadata{Name: "worker-identity-2"}, WorkerNamespace: "team", WorkerPool: "pool", WorkerPod: "worker-2"},
	}}
	service := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ateClient)

	page, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, page.Workers, 1)
	assert.Equal(t, "worker-0", page.Workers[0].WorkerPod)
	assert.NotEmpty(t, page.NextPageToken)
	assert.Equal(t, 2, ateClient.workerReads)
	next, err := service.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1.PageRequest{Limit: 1, PageToken: page.NextPageToken}})
	require.NoError(t, err)
	require.Len(t, next.Workers, 1)
	assert.Equal(t, "worker-2", next.Workers[0].WorkerPod)
	assert.Empty(t, next.NextPageToken)
	assert.Equal(t, 3, ateClient.workerReads)
}

func TestInventoryFiltersBeforePaging(t *testing.T) {
	ctx := pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{ID: "reader"}})
	ate := &fakeATEClient{pageSize: 1, actors: []*ateapipb.Actor{substrateActor("hidden", "team", 0, "", ""), substrateActor("visible", "team", 0, "", "")}, workers: []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "hidden"}, WorkerNamespace: "workers"}, {Metadata: &ateapipb.ResourceMetadata{Name: "visible"}, WorkerNamespace: "workers"}}}
	policy := inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{
		"SubstrateActor":  inventoryScope(apiauthorization.AttributeAtespace, "team", "visible"),
		"SubstrateWorker": inventoryScope(apiauthorization.AttributeNamespace, "workers", "visible"),
	}}
	svc := system.NewService(nil, nil, policy, ate)
	actors, err := svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.NoError(t, err)
	require.Len(t, actors.Actors, 1)
	require.Empty(t, actors.NextPageToken)
	workers, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	require.Len(t, workers.Workers, 1)
	require.Empty(t, workers.NextPageToken)
	denied := system.NewService(nil, nil, inventoryAuthorizer{}, nil)
	_, err = denied.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
	_, err = denied.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
}

func TestActorListScanBudgetSpansWatchedAtespaces(t *testing.T) {
	ate := &fakeATEClient{}
	var namespaces []string
	for i := range 101 {
		namespace := fmt.Sprintf("team-%03d", i)
		namespaces = append(namespaces, namespace)
		ate.actors = append(ate.actors, substrateActor("actor", namespace, 0, "", ""))
	}
	svc := system.NewService(nil, namespaces, &pkgAuth.NoopAuthorizer{}, ate)
	for attempt := range 2 {
		page, err := svc.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 100}})
		require.NoError(t, err)
		require.Contains(t, page.ATEAPIError, "ResourceExhausted")
		require.Empty(t, page.Actors)
		require.Empty(t, page.NextPageToken)
		require.Equal(t, (attempt+1)*100, ate.actorReads, "one request-wide budget, reset for each request")
	}
	// A narrower request can succeed without scanning earlier atespaces.
	page, err := svc.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Atespace: namespaces[100]})
	require.NoError(t, err)
	require.Empty(t, page.ATEAPIError)
	require.Len(t, page.Actors, 1)
	require.Equal(t, 201, ate.actorReads)
}

func TestWorkerListScanBudgetAndDeadline(t *testing.T) {
	for _, finishAtLimit := range []bool{false, true} {
		t.Run(fmt.Sprintf("finish-at-limit=%v", finishAtLimit), func(t *testing.T) {
			reads := 0
			ate := &inventoryScanATEClient{read: func(ctx context.Context, _ int32, _ string) ([]*ateapipb.Worker, string, error) {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
				reads++
				if finishAtLimit && reads == 100 {
					return []*ateapipb.Worker{{Metadata: &ateapipb.ResourceMetadata{Name: "visible"}, WorkerNamespace: "team"}}, "", nil
				}
				return nil, strconv.Itoa(reads), nil
			}}
			svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
			page, err := svc.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1}})
			require.NoError(t, err)
			require.Equal(t, 100, reads)
			require.Empty(t, page.NextPageToken)
			if finishAtLimit {
				require.Empty(t, page.ATEAPIError)
				require.Len(t, page.Workers, 1)
			} else {
				require.Contains(t, page.ATEAPIError, "ResourceExhausted")
				require.Empty(t, page.Workers)
			}
		})
	}
}

func TestInventoryListHonorsEarlierCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(pagingContext(t), time.Now().Add(-time.Second))
	defer cancel()
	ate := &fakeATEClient{}
	svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
	actors, err := svc.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{})
	require.NoError(t, err)
	require.Contains(t, actors.ATEAPIError, "DeadlineExceeded")
	require.Empty(t, actors.Actors)
	require.Empty(t, actors.NextPageToken)
	workers, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	require.Contains(t, workers.ATEAPIError, "DeadlineExceeded")
	require.Empty(t, workers.Workers)
	require.Empty(t, workers.NextPageToken)
	require.Zero(t, ate.actorReads)
	require.Zero(t, ate.workerReads)
}

func TestInventoryListDeadlineDuringBackendRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(pagingContext(t), 20*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	ate := &inventoryScanATEClient{read: func(ctx context.Context, _ int32, _ string) ([]*ateapipb.Worker, string, error) {
		actualDeadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, actualDeadline, "list timeout must not extend caller deadline")
		<-ctx.Done()
		return nil, "", ctx.Err()
	}}
	svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
	page, err := svc.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{})
	require.NoError(t, err)
	require.Contains(t, page.ATEAPIError, "DeadlineExceeded")
	require.Empty(t, page.Workers)
	require.Empty(t, page.NextPageToken)
}

func TestInventoryHybridBatchingKeepsDenseRequestsSmall(t *testing.T) {
	for _, limit := range []int32{1, 50, 100} {
		t.Run(strconv.Itoa(int(limit)), func(t *testing.T) {
			reads := 0
			ate := &inventoryScanATEClient{read: func(_ context.Context, size int32, _ string) ([]*ateapipb.Worker, string, error) {
				reads++
				require.Equal(t, limit+1, size)
				rows := make([]*ateapipb.Worker, size)
				for i := range rows {
					rows[i] = &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: fmt.Sprintf("worker-%03d", i)}, WorkerNamespace: "team"}
				}
				return rows, "backend-next", nil
			}}
			svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
			page, err := svc.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: limit}})
			require.NoError(t, err)
			require.Empty(t, page.ATEAPIError)
			require.Len(t, page.Workers, int(limit))
			require.NotEmpty(t, page.NextPageToken)
			require.Equal(t, 1, reads)
		})
	}
}

func TestInventoryRowBudgetCountsEveryFetchedRow(t *testing.T) {
	for _, scenario := range []string{"no matches", "partial page", "end at budget", "lookahead at budget"} {
		t.Run(scenario, func(t *testing.T) {
			fetched := 0
			var sizes []int32
			ate := &inventoryScanATEClient{read: func(_ context.Context, size int32, _ string) ([]*ateapipb.Worker, string, error) {
				sizes = append(sizes, size)
				require.LessOrEqual(t, fetched+int(size), 10_000, "must cap the final backend batch")
				rows := make([]*ateapipb.Worker, size)
				for i := range rows {
					fetched++
					namespace := "other"
					if fetched == 1 && (scenario == "partial page" || scenario == "lookahead at budget") || fetched == 10_000 && (scenario == "end at budget" || scenario == "lookahead at budget") {
						namespace = "team"
					}
					rows[i] = &ateapipb.Worker{Metadata: &ateapipb.ResourceMetadata{Name: fmt.Sprintf("worker-%05d", fetched)}, WorkerNamespace: namespace}
				}
				next := strconv.Itoa(fetched)
				if fetched == 10_000 && (scenario == "end at budget" || scenario == "lookahead at budget") {
					next = ""
				}
				return rows, next, nil
			}}
			svc := system.NewService(nil, nil, &pkgAuth.NoopAuthorizer{}, ate)
			page, err := svc.ListSubstrateWorkers(pagingContext(t), &apiv1.ListSubstrateWorkersRequest{Namespace: "team", Page: &apiv1.PageRequest{Limit: 1}})
			require.NoError(t, err)
			require.Equal(t, 10_000, fetched)
			require.Len(t, sizes, 11)
			require.Equal(t, int32(2), sizes[0])
			for _, size := range sizes[1:10] {
				require.Equal(t, int32(1_000), size)
			}
			require.Equal(t, int32(998), sizes[10])
			if scenario == "no matches" || scenario == "partial page" {
				require.Contains(t, page.ATEAPIError, "ResourceExhausted")
				require.Empty(t, page.Workers)
				require.Empty(t, page.NextPageToken)
			} else {
				require.Empty(t, page.ATEAPIError)
				require.Len(t, page.Workers, 1)
				if scenario == "lookahead at budget" {
					require.NotEmpty(t, page.NextPageToken)
				} else {
					require.Empty(t, page.NextPageToken)
				}
			}
		})
	}
}

func TestInventoryRowBudgetAndHybridBatchingSpanAtespaces(t *testing.T) {
	ate := &fakeATEClient{}
	for _, namespace := range []string{"team-a", "team-b"} {
		count := 2
		if namespace == "team-b" {
			count = 10_001
		}
		for i := range count {
			ate.actors = append(ate.actors, substrateActor(fmt.Sprintf("actor-%05d", i), namespace, 0, "", ""))
		}
	}
	policy := inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{"SubstrateActor": inventoryScope(apiauthorization.AttributeAtespace, "team-b", "missing")}}
	svc := system.NewService(nil, []string{"team-a", "team-b"}, policy, ate)
	page, err := svc.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Contains(t, page.ATEAPIError, "ResourceExhausted")
	require.Empty(t, page.Actors)
	require.Empty(t, page.NextPageToken)
	require.Equal(t, "team-a", ate.actorAtespaces[0])
	require.Equal(t, "team-b", ate.actorAtespaces[1])
	require.Equal(t, int32(2), ate.actorSizes[0])
	require.Equal(t, int32(1_000), ate.actorSizes[1], "first-batch sizing must not reset at an atespace boundary")
	var total int32
	for _, size := range ate.actorSizes {
		total += size
	}
	require.Equal(t, int32(10_000), total, "row budget must not reset at an atespace boundary")
}

func TestActorListRedactsIndependentlyDeniedWorkerPlacement(t *testing.T) {
	for _, scenario := range []string{"allowed", "none", "wrong name", "pod name", "pool name", "unwatched", "missing identity", "node without assignment", "snapshot locality only"} {
		t.Run(scenario, func(t *testing.T) {
			assignment := &ateapipb.WorkerAssignment{Worker: &ateapipb.ObjectRef{Name: "worker-id"}, WorkerNamespace: "workers", WorkerPod: "private-pod", WorkerPool: "private-pool", WorkerPodIps: []string{"10.1.2.3"}, NodeName: "private-node"}
			actor := substrateActor("visible-actor", "team", ateapipb.ActorState_ACTOR_STATE_RUNNING, "", "")
			actor.Status.WorkerAssignment = assignment
			actor.Status.AssignedNode = "private-node"
			actor.Status.Snapshots = []*ateapipb.Snapshot{{Uuid: "snapshot", Locality: "snapshot-node"}}
			workerName := "worker-id"
			watched := []string{"team", "workers"}
			switch scenario {
			case "wrong name":
				workerName = "other-worker"
			case "pod name":
				workerName = assignment.WorkerPod
			case "pool name":
				workerName = assignment.WorkerPool
			case "unwatched":
				watched = []string{"team"}
			case "missing identity":
				assignment.Worker = nil
			case "node without assignment":
				actor.Status.WorkerAssignment = nil
			case "snapshot locality only":
				actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
				actor.Status.WorkerAssignment = nil
				actor.Status.AssignedNode = ""
			}
			workerScope := inventoryScope(apiauthorization.AttributeNamespace, "workers", workerName)
			if scenario == "none" {
				workerScope = apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}
			}
			policy := inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{
				"SubstrateActor":  inventoryScope(apiauthorization.AttributeAtespace, "team", "visible-actor"),
				"SubstrateWorker": workerScope,
			}}
			original := proto.CloneOf(actor)
			svc := system.NewService(nil, watched, policy, &fakeATEClient{actors: []*ateapipb.Actor{actor}})
			page, err := svc.ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{Atespace: "team"})
			require.NoError(t, err)
			require.Empty(t, page.ATEAPIError)
			require.Len(t, page.Actors, 1)
			got := page.Actors[0].GetStatus()
			require.Equal(t, actor.Status.State, got.GetState())
			require.Len(t, got.GetSnapshots(), 1)
			require.Equal(t, "snapshot", got.GetSnapshots()[0].GetUuid())
			require.Empty(t, got.GetSnapshots()[0].GetLocality())
			if scenario == "allowed" {
				require.True(t, proto.Equal(assignment, got.GetWorkerAssignment()))
				require.Equal(t, "private-node", got.GetAssignedNode())
			} else {
				require.Nil(t, got.GetWorkerAssignment())
				require.Empty(t, got.GetAssignedNode())
			}
			require.True(t, proto.Equal(original, actor), "redaction must not mutate shared backend objects")
		})
	}
}

func TestActorListWorkerScopeErrorsFailBeforeInventoryRead(t *testing.T) {
	for _, failed := range []bool{false, true} {
		policy := inventoryAuthorizer{scopes: map[string]apiauthorization.AuthorizationScope{
			"SubstrateActor":  {Kind: apiauthorization.ScopeAll},
			"SubstrateWorker": inventoryScope(apiauthorization.AttributeAtespace, "wrong-attribute", "worker"),
		}}
		if failed {
			policy.fail = "SubstrateWorker"
		}
		ate := &fakeATEClient{}
		_, err := system.NewService(nil, nil, policy, ate).ListSubstrateActors(pagingContext(t), &apiv1.ListSubstrateActorsRequest{})
		require.Error(t, err)
		require.Zero(t, ate.actorReads)
	}
}

func TestSubstrateSummaryHidesBackendErrorDetails(t *testing.T) {
	for _, backendErr := range []error{
		status.Error(codes.Unavailable, "private-atespace/private-actor unavailable"),
		fmt.Errorf("private-atespace: %w", status.Error(codes.Internal, "private-worker")),
		errors.New("private database details"),
	} {
		t.Run(status.Code(backendErr).String(), func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, atev1.AddToScheme(scheme))
			kube := fake.NewClientBuilder().WithScheme(scheme).Build()
			svc := system.NewService(kube, nil, &pkgAuth.NoopAuthorizer{}, &fakeATEClient{err: backendErr})
			result, err := svc.GetSubstrateSummary(pagingContext(t), "", "")
			require.NoError(t, err)
			require.Equal(t, "ate-api could not read inventory: "+status.Code(backendErr).String(), result.ATEAPIError)
			require.NotContains(t, result.ATEAPIError, "private")
		})
	}
}
