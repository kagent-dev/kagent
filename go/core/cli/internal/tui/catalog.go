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

// catalog reads what could be run, not what is running. Harness and AgentTemplate
// are CRDs with no gRPC read service, so this is the TUI's only kubeconfig need.
type catalog interface {
	Namespaces(ctx context.Context) ([]namespaceCount, error)
	Harnesses(ctx context.Context, namespace string) ([]string, error)
	AgentTemplates(ctx context.Context, namespace string) ([]string, error)
}

// namespaceCount is a namespace and how many AgentTemplates it holds.
type namespaceCount struct {
	Name      string
	Templates int
}

// kubeCatalog reads the catalog through the generated clientset.
type kubeCatalog struct {
	clients clientset.Interface
}

var _ catalog = (*kubeCatalog)(nil)

// newKubeCatalog builds a catalog from the ambient kubeconfig. A failure is not
// fatal: the caller degrades to instance-derived panels.
func newKubeCatalog() (catalog, error) {
	clients, err := commonk8s.NewKagentClientset()
	if err != nil {
		return nil, err
	}
	return &kubeCatalog{clients: clients}, nil
}

// Namespaces lists the namespaces holding AgentTemplates. An empty namespace
// lists cluster-wide, which needs list permission the caller may not have.
func (c *kubeCatalog) Namespaces(ctx context.Context) ([]namespaceCount, error) {
	list, err := c.clients.ApiV1alpha3().AgentTemplates("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list AgentTemplates across namespaces: %w", err)
	}
	counts := map[string]int{}
	for i := range list.Items {
		counts[list.Items[i].Namespace]++
	}
	namespaces := make([]namespaceCount, 0, len(counts))
	for name, templates := range counts {
		namespaces = append(namespaces, namespaceCount{Name: name, Templates: templates})
	}
	slices.SortFunc(namespaces, func(a, b namespaceCount) int {
		return strings.Compare(a.Name, b.Name)
	})
	return namespaces, nil
}

func (c *kubeCatalog) Harnesses(ctx context.Context, namespace string) ([]string, error) {
	list, err := c.clients.ApiV1alpha3().Harnesses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list Harnesses: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names, nil
}

func (c *kubeCatalog) AgentTemplates(ctx context.Context, namespace string) ([]string, error) {
	list, err := c.clients.ApiV1alpha3().AgentTemplates(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list AgentTemplates: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].Name)
	}
	return names, nil
}
