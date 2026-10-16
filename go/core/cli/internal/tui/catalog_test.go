package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// pagedAgentLister serves its Agents one page at a time and records each request.
type pagedAgentLister struct {
	pages    [][]*apiv1.ResourceReference
	requests []*apiv1.ListAgentsRequest
}

func (l *pagedAgentLister) ListAgents(_ context.Context, request *apiv1.ListAgentsRequest) (*apiv1.ListAgentsResponse, error) {
	index := len(l.requests)
	l.requests = append(l.requests, request)
	response := &apiv1.ListAgentsResponse{Page: &apiv1.PageResponse{}}
	for _, ref := range l.pages[index] {
		response.Agents = append(response.Agents, &apiv1.Agent{Ref: ref})
	}
	if index+1 < len(l.pages) {
		response.Page.NextPageToken = "page-" + string(rune('1'+index))
	}
	return response, nil
}

func TestAPICatalogReadsEveryPage(t *testing.T) {
	lister := &pagedAgentLister{pages: [][]*apiv1.ResourceReference{
		{{Namespace: "team-b", Name: "one"}, {Namespace: "team-a", Name: "two"}},
		{{Namespace: "team-b", Name: "three"}},
	}}

	namespaces, err := (&apiCatalog{agents: lister}).Namespaces(context.Background())
	require.NoError(t, err)
	require.Equal(t, []namespaceCount{{Name: "team-a", Agents: 1}, {Name: "team-b", Agents: 2}}, namespaces)
	require.Len(t, lister.requests, 2)
	// Empty means every namespace.
	require.Empty(t, lister.requests[0].GetNamespace())
	require.Empty(t, lister.requests[0].GetPage().GetPageToken())
	require.Equal(t, "page-1", lister.requests[1].GetPage().GetPageToken())
	require.Equal(t, int32(catalogPageSize), lister.requests[1].GetPage().GetLimit())
}
