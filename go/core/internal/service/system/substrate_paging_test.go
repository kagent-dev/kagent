package system_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func pagingContext(t *testing.T) context.Context {
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
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
	require.NoError(t, atev1alpha1.AddToScheme(scheme))
	ateClient := &failingWorkersATEClient{fakeATEClient: *pagingATEClient()}
	ateClient.workerErr = status.Error(codes.Internal, "worker u2-tenant-b is down")
	service := system.NewService(fake.NewClientBuilder().WithScheme(scheme).Build(), []string{"tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)

	summary, err := service.GetSubstrateSummary(pagingContext(t), "", "")
	require.NoError(t, err)
	assert.Equal(t, "ate-api could not list workers: Internal", summary.ATEAPIError)
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
