package controller

import (
	"reflect"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
)

// Collections contains Kubernetes inputs and the compiled state of each Agent.
type Collections struct {
	Agents                   krt.Collection[*kagentv1alpha3.Agent]
	AgentTemplates           krt.Collection[*kagentv1alpha3.AgentTemplate]
	Harnesses                krt.Collection[*kagentv1alpha3.Harness]
	ModelConfigs             krt.Collection[*kagentv1alpha3.ModelConfig]
	RemoteMCPServers         krt.Collection[*kagentv1alpha3.RemoteMCPServer]
	ConfigMaps               krt.Collection[*corev1.ConfigMap]
	Secrets                  krt.Collection[*corev1.Secret]
	WorkerPools              krt.Collection[*atev1alpha1.WorkerPool]
	AgentRuntimeObservations krt.StaticCollection[AgentRuntimeObservation]
	Reconciliations          krt.Collection[AgentReconciliation]
	ModelConfigStatuses      krt.StatusCollection[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]
	ResolvedModelConfigs     krt.Collection[v2translator.ResolvedModelConfig]
	AgentStatuses            krt.StatusCollection[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]
}

// AgentRuntimeObservation records a Agent's preparation. The revision prevents a
// cached observation from making changed inputs ready before reconciliation.
type AgentRuntimeObservation struct {
	Namespace  string
	AgentName  string
	RevisionID v2translator.RevisionID
	Template   *ateapipb.ActorTemplate
	Failure    *ReconciliationFailure
}

func (p AgentRuntimeObservation) ResourceName() string {
	return p.Namespace + "/" + p.AgentName
}

var _ krt.Equaler[AgentRuntimeObservation] = AgentRuntimeObservation{}

// Equals compares runtime contents rather than protobuf's mutable caches.
func (p AgentRuntimeObservation) Equals(other AgentRuntimeObservation) bool {
	if !proto.Equal(p.Template, other.Template) {
		return false
	}
	p.Template, other.Template = nil, nil
	return reflect.DeepEqual(p, other)
}

// NewCollections creates the complete read-only input graph. An empty
// watchNamespaces list watches all namespaces.
func NewCollections(client kube.Client, watchNamespaces []string, opts krt.OptionsBuilder) Collections {
	agents := typedCollection[*kagentv1alpha3.Agent](client, watchNamespaces, "Agents", opts)
	agentTemplates := typedCollection[*kagentv1alpha3.AgentTemplate](client, watchNamespaces, "AgentTemplates", opts)
	harnesses := typedCollection[*kagentv1alpha3.Harness](client, watchNamespaces, "Harnesses", opts)
	modelConfigs := typedCollection[*kagentv1alpha3.ModelConfig](client, watchNamespaces, "ModelConfigs", opts)
	remoteMCPServers := typedCollection[*kagentv1alpha3.RemoteMCPServer](client, watchNamespaces, "RemoteMCPServers", opts)
	configMaps := typedCollection[*corev1.ConfigMap](client, watchNamespaces, "ConfigMaps", opts)
	secrets := typedCollection[*corev1.Secret](client, watchNamespaces, "Secrets", opts)
	workerPools := typedCollection[*atev1alpha1.WorkerPool](client, watchNamespaces, "WorkerPools", opts)
	agentRuntimeObservations := krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...)
	modelConfigStatuses, resolvedModelConfigs := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)
	compilerCollections := v2translator.Collections{
		Harnesses: harnesses, AgentTemplates: agentTemplates, ResolvedModelConfigs: resolvedModelConfigs, RemoteMCPServers: remoteMCPServers,
		ConfigMaps: configMaps, Secrets: secrets, WorkerPools: workerPools,
	}
	reconciliations := newAgentReconciliations(agents, compilerCollections, agentRuntimeObservations, opts)
	statuses := newAgentStatuses(agents, reconciliations, opts)

	return Collections{
		Agents:                   agents,
		AgentTemplates:           agentTemplates,
		Harnesses:                harnesses,
		ModelConfigs:             modelConfigs,
		RemoteMCPServers:         remoteMCPServers,
		ConfigMaps:               configMaps,
		Secrets:                  secrets,
		WorkerPools:              workerPools,
		AgentRuntimeObservations: agentRuntimeObservations,
		Reconciliations:          reconciliations,
		ModelConfigStatuses:      modelConfigStatuses,
		ResolvedModelConfigs:     resolvedModelConfigs,
		AgentStatuses:            statuses,
	}
}

func typedCollection[T controllers.ComparableObject](client kube.Client, namespaces []string, name string, opts krt.OptionsBuilder) krt.Collection[T] {
	if len(namespaces) == 0 {
		return krt.NewInformer[T](client, opts.WithName(name)...)
	}

	collections := make([]krt.Collection[T], 0, len(namespaces))
	for _, namespace := range namespaces {
		collections = append(collections, krt.NewFilteredInformer[T](client, kclient.Filter{Namespace: namespace}, opts.WithName(name+"/"+namespace)...))
	}
	return krt.JoinCollection(collections, append(opts.WithName(name), krt.WithJoinUnchecked())...)
}
