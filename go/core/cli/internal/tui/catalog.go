package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	clientset "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	commonk8s "github.com/kagent-dev/kagent/go/core/cli/internal/common/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// catalog reads what could be run: Agent CRDs, the TUI's only kubeconfig need.
type catalog interface {
	Namespaces(ctx context.Context) ([]namespaceCount, error)
	Agents(ctx context.Context, namespace string) ([]string, error)
}

// namespaceCount is a namespace and how many Agents it holds.
type namespaceCount struct {
	Name   string
	Agents int
}

// kubeCatalog reads the catalog through the generated clientset.
type kubeCatalog struct {
	clients clientset.Interface
}

var _ catalog = (*kubeCatalog)(nil)

// newKubeCatalog builds a catalog from the ambient kubeconfig; callers degrade to instance-derived panels.
func newKubeCatalog() (catalog, error) {
	clients, err := commonk8s.NewKagentClientset()
	if err != nil {
		return nil, err
	}
	return &kubeCatalog{clients: clients}, nil
}

// Namespaces lists namespaces holding Agents, which needs cluster-wide list permission.
func (c *kubeCatalog) Namespaces(ctx context.Context) ([]namespaceCount, error) {
	list, err := c.clients.ApiV1alpha3().Agents("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Agents across namespaces: %w", err)
	}
	counts := map[string]int{}
	for i := range list.Items {
		counts[list.Items[i].Namespace]++
	}
	namespaces := make([]namespaceCount, 0, len(counts))
	for name, templates := range counts {
		namespaces = append(namespaces, namespaceCount{Name: name, Agents: templates})
	}
	slices.SortFunc(namespaces, func(a, b namespaceCount) int {
		return strings.Compare(a.Name, b.Name)
	})
	return namespaces, nil
}

func (c *kubeCatalog) Agents(ctx context.Context, namespace string) ([]string, error) {
	list, err := c.clients.ApiV1alpha3().Agents(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Agents: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names, nil
}
