package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// This is a backend pagination contract test, independent of the agent harness.
// Using real Sessions guarantees two Actors; CI's WorkerPool supplies four workers.
// No retry wraps list calls: an incompatible encoded token must fail on page two.
func TestSubstrateInventoryPagination(t *testing.T) {
	target := interactionTarget(t)
	template := createInteractionTemplate(t, testHarness{name: "kagent", runtimeLabel: "kagent"}, startInteractionMock(t))
	conn := newControllerConn(t, target)
	sessions := apiv1.NewSessionServiceClient(conn)
	system := apiv1.NewSystemServiceClient(conn)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 3*time.Minute)
	t.Cleanup(cancel)
	var wantActors []string
	for range 2 {
		created, err := sessions.CreateSession(ctx, &apiv1.CreateSessionRequest{
			Agent: &apiv1.ResourceReference{Namespace: "kagent", Name: template}, RequestId: uuid.NewString(),
		})
		require.NoError(t, err)
		id := created.GetSession().GetId()
		require.NotEmpty(t, id)
		wantActors = append(wantActors, "kagent/"+substrate.ActorName(id))
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
			defer cleanupCancel()
			require.NoError(t, deleteIdleSession(cleanupCtx, sessions, id))
		})
	}
	for _, atespace := range []string{"", "kagent"} {
		t.Run("actors/"+atespace, func(t *testing.T) {
			seen := map[string]bool{}
			token := ""
			for pages := 0; ; pages++ {
				require.Less(t, pages, 1000, "actor pagination did not terminate")
				page, err := system.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{Atespace: atespace, Page: &apiv1.PageRequest{Limit: 1, PageToken: token}})
				require.NoError(t, err)
				require.Empty(t, page.GetAteApiError())
				require.Len(t, page.GetActors(), 1)
				actor := page.GetActors()[0].GetMetadata()
				key := actor.GetAtespace() + "/" + actor.GetName()
				require.False(t, seen[key], "actor repeated across pages: %s", key)
				seen[key] = true
				token = page.GetPage().GetNextPageToken()
				if pages == 0 {
					require.NotEmpty(t, token, "must exercise a real continuation")
				}
				if token == "" {
					break
				}
			}
			for _, actor := range wantActors {
				require.True(t, seen[actor], "fixture actor missing: %s", actor)
			}
		})
	}
	t.Run("workers", func(t *testing.T) {
		seen := map[string]bool{}
		token := ""
		for pages := 0; ; pages++ {
			require.Less(t, pages, 1000, "worker pagination did not terminate")
			page, err := system.ListSubstrateWorkers(ctx, &apiv1.ListSubstrateWorkersRequest{Page: &apiv1.PageRequest{Limit: 1, PageToken: token}})
			require.NoError(t, err)
			require.Empty(t, page.GetAteApiError())
			require.Len(t, page.GetWorkers(), 1)
			name := page.GetWorkers()[0].GetMetadata().GetName()
			require.NotEmpty(t, name)
			require.False(t, seen[name], "worker repeated across pages: %s", name)
			seen[name] = true
			token = page.GetPage().GetNextPageToken()
			if pages == 0 {
				require.NotEmpty(t, token, "requires at least two registered Substrate workers; CI provisions four")
			}
			if token == "" {
				break
			}
		}
		require.GreaterOrEqual(t, len(seen), 2)
	})
}
