package apiclient

import (
	"context"
	"sync"

	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube/kubetypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

var registerTypesOnce sync.Once

// RegisterTypes connects KRT's type registry to the generated Kubernetes
// clients. This is the same registration boundary used by agentgateway.
func RegisterTypes() {
	registerTypesOnce.Do(registerTypes)
}

func registerTypes() {
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("scheduledruns"),
		kagentv1alpha3.GroupVersion.WithKind("ScheduledRun"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().ScheduledRuns(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().ScheduledRuns(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.ScheduledRun] {
			return c.(Client).Kagent().ApiV1alpha3().ScheduledRuns(namespace)
		},
	)
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("egresspolicies"),
		kagentv1alpha3.GroupVersion.WithKind("EgressPolicy"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().EgressPolicies(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().EgressPolicies(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.EgressPolicy] {
			return c.(Client).Kagent().ApiV1alpha3().EgressPolicies(namespace)
		},
	)
	kubeclient.Register(
		kagentv1alpha3.GroupVersion.WithResource("agents"),
		kagentv1alpha3.GroupVersion.WithKind("Agent"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().Agents(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().Agents(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.Agent] {
			return c.(Client).Kagent().ApiV1alpha3().Agents(namespace)
		},
	)
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
		kagentv1alpha3.GroupVersion.WithResource("sandboxtemplates"),
		kagentv1alpha3.GroupVersion.WithKind("SandboxTemplate"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Kagent().ApiV1alpha3().SandboxTemplates(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Kagent().ApiV1alpha3().SandboxTemplates(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*kagentv1alpha3.SandboxTemplate] {
			return c.(Client).Kagent().ApiV1alpha3().SandboxTemplates(namespace)
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
		atev1.GroupVersion.WithResource("workerpools"),
		atev1.GroupVersion.WithKind("WorkerPool"),
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			return c.(Client).Substrate().AteV1().WorkerPools(namespace).List(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return c.(Client).Substrate().AteV1().WorkerPools(namespace).Watch(context.Background(), options)
		},
		func(c kubeclient.ClientGetter, namespace string) kubetypes.WriteAPI[*atev1.WorkerPool] {
			return c.(Client).Substrate().AteV1().WorkerPools(namespace)
		},
	)
}
