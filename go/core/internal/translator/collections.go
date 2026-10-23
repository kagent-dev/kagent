package translator

import (
	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Collections contains every typed input used while compiling a revision.
// Production supplies informer-backed collections; tests supply KRT mocks.
type Collections struct {
	EgressPolicies       krt.Collection[*v1alpha3.EgressPolicy]
	Harnesses            krt.Collection[*v1alpha3.Harness]
	AgentTemplates       krt.Collection[*v1alpha3.AgentTemplate]
	ResolvedModelConfigs krt.Collection[ResolvedModelConfig]
	RemoteMCPServers     krt.Collection[*v1alpha3.RemoteMCPServer]
	ConfigMaps           krt.Collection[*corev1.ConfigMap]
	Secrets              krt.Collection[*corev1.Secret]
	WorkerPools          krt.Collection[*atev1alpha1.WorkerPool]
}
