package apiclient

import (
	"context"
	"sync"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube/kubetypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

var registerTypesOnce sync.Once

// RegisterTypes connects KRT's type registry to the generated Kubernetes
// clients. This is the same registration boundary used by agentgateway.
func RegisterTypes() {
	registerTypesOnce.Do(registerTypes)
}

func registerTypes() {
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("agenttemplates"),
		kagentv1alpha3.GroupVersion.WithKind("AgentTemplate"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().AgentTemplates(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().AgentTemplates(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.AgentTemplate] {
			return c.(Client).Kagent().ApiV1alpha3().AgentTemplates(namespace)
		},
	)
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("harnesses"),
		kagentv1alpha3.GroupVersion.WithKind("Harness"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().Harnesses(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().Harnesses(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.Harness] {
			return c.(Client).Kagent().ApiV1alpha3().Harnesses(namespace)
		},
	)
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("modelconfigs"),
		kagentv1alpha3.GroupVersion.WithKind("ModelConfig"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().ModelConfigs(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().ModelConfigs(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.ModelConfig] {
			return c.(Client).Kagent().ApiV1alpha3().ModelConfigs(namespace)
		},
	)
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("remotemcpservers"),
		kagentv1alpha3.GroupVersion.WithKind("RemoteMCPServer"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().RemoteMCPServers(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().RemoteMCPServers(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.RemoteMCPServer] {
			return c.(Client).Kagent().ApiV1alpha3().RemoteMCPServers(namespace)
		},
	)
	kubeclient.Register(
		atev1alpha1.GroupVersion.WithResource("workerpools"),
		atev1alpha1.GroupVersion.WithKind("WorkerPool"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Substrate().ApiV1alpha1().WorkerPools(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Substrate().ApiV1alpha1().WorkerPools(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*atev1alpha1.WorkerPool] {
			return c.(Client).Substrate().ApiV1alpha1().WorkerPools(namespace)
		},
	)
	kubeclient.Register(
		atev1alpha1.GroupVersion.WithResource("actortemplates"),
		atev1alpha1.GroupVersion.WithKind("ActorTemplate"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Substrate().ApiV1alpha1().ActorTemplates(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Substrate().ApiV1alpha1().ActorTemplates(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*atev1alpha1.ActorTemplate] {
			return c.(Client).Substrate().ApiV1alpha1().ActorTemplates(namespace)
		},
	)
}
