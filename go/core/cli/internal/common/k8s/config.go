// Package k8s provides utilities for working with Kubernetes configurations.
package k8s

import (
	"fmt"
	"strings"

	clientset "github.com/kagent-dev/kagent/go/api/clientset/versioned"
	"k8s.io/client-go/tools/clientcmd"
)

// NewKagentClientset builds the generated kagent clientset from the ambient
// kubeconfig. Harness and AgentTemplate are CRDs, so every reader goes through here.
func NewKagentClientset() (clientset.Interface, error) {
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes client config: %w", err)
	}
	clients, err := clientset.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return clients, nil
}

// GetCurrentNamespace returns the current namespace from the kubeconfig.
// If no namespace is configured, it returns an error.
func GetCurrentNamespace() (string, error) {
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	namespace, _, err := config.Namespace()
	if err != nil {
		return "", fmt.Errorf("failed to get current namespace: %w", err)
	}

	return namespace, nil
}

// GetCurrentKindClusterName extracts the kind cluster name from the current kubeconfig context.
// Returns an error if the current context is not a kind cluster.
func GetCurrentKindClusterName() (string, error) {
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	rawConfig, err := config.RawConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get raw kubeconfig: %w", err)
	}

	currentContext, ok := rawConfig.Contexts[rawConfig.CurrentContext]
	if !ok {
		return "", fmt.Errorf("current context %q not found in kubeconfig", rawConfig.CurrentContext)
	}

	const kindPrefix = "kind-"
	if after, ok0 := strings.CutPrefix(currentContext.Cluster, kindPrefix); ok0 {
		return after, nil
	}

	return "", fmt.Errorf("current cluster %q is not a kind cluster", currentContext.Cluster)
}

// GetCurrentContext returns the name of the current kubeconfig context.
func GetCurrentContext() (string, error) {
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	rawConfig, err := config.RawConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get raw kubeconfig: %w", err)
	}

	return rawConfig.CurrentContext, nil
}

// IsKindCluster checks if the current kubeconfig context points to a kind cluster.
func IsKindCluster() bool {
	_, err := GetCurrentKindClusterName()
	return err == nil
}
