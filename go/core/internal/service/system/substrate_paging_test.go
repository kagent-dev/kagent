package system_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/service/system"
	pkgAuth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func pagingContext(t *testing.T) context.Context {
	return pkgAuth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: pkgAuth.Principal{User: pkgAuth.User{ID: "user"}}})
}

// pagingATEClient mixes three tenants' rows in ate-api order, two rows a page. Every
// tenant-b name holds "tenant-b", so a test can look for it in any text.
func pagingATEClient() *fakeATEClient {
	client := &fakeATEClient{pageSize: 2}
	for _, actor := range []string{"tenant-a/a-1", "tenant-b/b-1", "tenant-c/c-1", "tenant-a/a-2", "tenant-b/b-2", "tenant-c/c-2", "tenant-a/a-3"} {
		atespace, name, _ := strings.Cut(actor, "/")
		client.actors = append(client.actors, substrateActor(name, atespace, ateapipb.ActorState_ACTOR_STATE_RUNNING, "", ""))
	}
	return client
}

// pagingService watches tenant-a and tenant-c, listed out of order and with a repeat.
func pagingService(ateClient system.ATEClient) *system.Service {
	return system.NewService(nil, []string{"tenant-c", "tenant-a", "tenant-a"}, &pkgAuth.NoopAuthorizer{}, ateClient)
}

// assertOwnToken fails when a page token names a tenant-b row, or when an empty page
// carries a token: that would tell the caller a hidden row exists.
func assertOwnToken(t *testing.T, rows int, pageToken string) {
	t.Helper()
	raw, _ := base64.RawURLEncoding.DecodeString(pageToken)
	assert.NotContains(t, pageToken+string(raw), "tenant-b")
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

func actorPageToken(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// A token this controller did not issue never reaches ate-api and never restarts the
// walk at page one. Its error repeats neither the token nor a tenant-b name.
func TestListSubstrateActorsRefusesForeignTokens(t *testing.T) {
	for name, tc := range map[string]struct {
		atespace  string
		pageToken string
	}{
		"not base64":            {pageToken: "upstream:2"},
		"no separator":          {pageToken: actorPageToken("tenant-a")},
		"empty atespace":        {pageToken: actorPageToken("/upstream:2")},
		"unwatched atespace":    {pageToken: actorPageToken("tenant-b/upstream:2")},
		"outside the request":   {atespace: "tenant-c", pageToken: actorPageToken("tenant-a/upstream:2")},
		"ate-api actor token":   {pageToken: actorPageToken(`{"v":1,"kind":"actor","last":["tenant-b","b-1"]}`)},
		"over 1 KiB, else good": {pageToken: actorPageToken("tenant-a/" + strings.Repeat("x", 800))},
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
