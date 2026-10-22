package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// catalog reads what could be run: Agents.
type catalog interface {
	Namespaces(ctx context.Context) ([]namespaceCount, error)
	Agents(ctx context.Context, namespace string) ([]string, error)
}

// namespaceCount is a namespace and how many Agents it holds.
type namespaceCount struct {
	Name   string
	Agents int
}

type agentLister interface {
	ListAgents(context.Context, *apiv1.ListAgentsRequest) (*apiv1.ListAgentsResponse, error)
}

// apiCatalog reads the catalog through the kagent API.
type apiCatalog struct {
	agents agentLister
}

var _ catalog = (*apiCatalog)(nil)

// catalogPageSize is the largest page the API serves.
const catalogPageSize = 100

// Namespaces lists the namespaces holding Agents.
func (c *apiCatalog) Namespaces(ctx context.Context) ([]namespaceCount, error) {
	refs, err := c.list(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list Agents across namespaces: %w", err)
	}
	counts := map[string]int{}
	for _, ref := range refs {
		counts[ref.GetNamespace()]++
	}
	namespaces := make([]namespaceCount, 0, len(counts))
	for name, agents := range counts {
		namespaces = append(namespaces, namespaceCount{Name: name, Agents: agents})
	}
	slices.SortFunc(namespaces, func(a, b namespaceCount) int {
		return strings.Compare(a.Name, b.Name)
	})
	return namespaces, nil
}

func (c *apiCatalog) Agents(ctx context.Context, namespace string) ([]string, error) {
	refs, err := c.list(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("list Agents: %w", err)
	}
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.GetName())
	}
	return names, nil
}

// list reads every page, so one response never has to hold the whole collection.
func (c *apiCatalog) list(ctx context.Context, namespace string) ([]*apiv1.ResourceReference, error) {
	var refs []*apiv1.ResourceReference
	token := ""
	for {
		response, err := c.agents.ListAgents(ctx, &apiv1.ListAgentsRequest{
			Namespace: namespace,
			Page:      &apiv1.PageRequest{Limit: catalogPageSize, PageToken: token},
		})
		if err != nil {
			return nil, err
		}
		for _, agent := range response.GetAgents() {
			refs = append(refs, agent.GetRef())
		}
		if token = response.GetPage().GetNextPageToken(); token == "" {
			return refs, nil
		}
	}
}
