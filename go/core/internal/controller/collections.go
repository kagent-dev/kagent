package controller

import (
	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Collections contains the Kubernetes inputs used to resolve an AgentTemplate
// and the template/harness pairs derived from Harness admission selectors.
type Collections struct {
	AgentTemplates        krt.Collection[*kagentv1alpha3.AgentTemplate]
	Harnesses             krt.Collection[*kagentv1alpha3.Harness]
	ModelConfigs          krt.Collection[*kagentv1alpha3.ModelConfig]
	RemoteMCPServers      krt.Collection[*kagentv1alpha3.RemoteMCPServer]
	ConfigMaps            krt.Collection[*corev1.ConfigMap]
	Secrets               krt.Collection[*corev1.Secret]
	WorkerPools           krt.Collection[*atev1alpha1.WorkerPool]
	ActorTemplates        krt.StaticCollection[ObservedActorTemplate]
	Pairs                 krt.Collection[AgentTemplateHarnessPair]
	Reconciliations       krt.Collection[PairReconciliation]
	ModelConfigStatuses   krt.StatusCollection[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]
	ResolvedModelConfigs  krt.Collection[v2translator.ResolvedModelConfig]
	AgentTemplateStatuses krt.StatusCollection[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]
}

// ObservedActorTemplate adapts an ate-api resource to KRT's keyed collection.
type ObservedActorTemplate struct {
	Template *ateapipb.ActorTemplate
}

func (t ObservedActorTemplate) ResourceName() string {
	return t.Template.GetMetadata().GetAtespace() + "/" + t.Template.GetMetadata().GetName()
}

// AgentTemplateHarnessPair is one same-namespace combination selected by a
// Harness. It carries the source objects so later collections can resolve the
// pair without returning to an imperative cache.
type AgentTemplateHarnessPair struct {
	AgentTemplate *kagentv1alpha3.AgentTemplate
	Harness       *kagentv1alpha3.Harness
}

func (p AgentTemplateHarnessPair) ResourceName() string {
	return p.AgentTemplate.Namespace + "/" + p.AgentTemplate.Name + "/" + p.Harness.Name
}

// NewCollections creates the complete read-only input graph. An empty
// watchNamespaces list watches all namespaces.
func NewCollections(client kube.Client, watchNamespaces []string, opts krt.OptionsBuilder) Collections {
	agentTemplates := typedCollection[*kagentv1alpha3.AgentTemplate](client, watchNamespaces, "AgentTemplates", opts)
	harnesses := typedCollection[*kagentv1alpha3.Harness](client, watchNamespaces, "Harnesses", opts)
	modelConfigs := typedCollection[*kagentv1alpha3.ModelConfig](client, watchNamespaces, "ModelConfigs", opts)
	remoteMCPServers := typedCollection[*kagentv1alpha3.RemoteMCPServer](client, watchNamespaces, "RemoteMCPServers", opts)
	configMaps := typedCollection[*corev1.ConfigMap](client, watchNamespaces, "ConfigMaps", opts)
	secrets := typedCollection[*corev1.Secret](client, watchNamespaces, "Secrets", opts)
	workerPools := typedCollection[*atev1alpha1.WorkerPool](client, watchNamespaces, "WorkerPools", opts)
	actorTemplates := krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...)
	pairs := newPairCollection(agentTemplates, harnesses, opts)
	modelConfigStatuses, resolvedModelConfigs := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)
	compilerCollections := v2translator.Collections{
		AgentTemplates: agentTemplates, ResolvedModelConfigs: resolvedModelConfigs, RemoteMCPServers: remoteMCPServers,
		ConfigMaps: configMaps, Secrets: secrets, WorkerPools: workerPools,
	}
	reconciliations := newPairReconciliations(pairs, compilerCollections, actorTemplates, opts)
	statuses := newAgentTemplateStatuses(agentTemplates, reconciliations, opts)

	return Collections{
		AgentTemplates:        agentTemplates,
		Harnesses:             harnesses,
		ModelConfigs:          modelConfigs,
		RemoteMCPServers:      remoteMCPServers,
		ConfigMaps:            configMaps,
		Secrets:               secrets,
		WorkerPools:           workerPools,
		ActorTemplates:        actorTemplates,
		Pairs:                 pairs,
		Reconciliations:       reconciliations,
		ModelConfigStatuses:   modelConfigStatuses,
		ResolvedModelConfigs:  resolvedModelConfigs,
		AgentTemplateStatuses: statuses,
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

func newPairCollection(agentTemplates krt.Collection[*kagentv1alpha3.AgentTemplate], harnesses krt.Collection[*kagentv1alpha3.Harness], opts krt.OptionsBuilder) krt.Collection[AgentTemplateHarnessPair] {
	harnessesByNamespace := krt.NewNamespaceIndex(harnesses)
	return krt.NewManyCollection(agentTemplates, func(ctx krt.HandlerContext, agentTemplate *kagentv1alpha3.AgentTemplate) []AgentTemplateHarnessPair {
		matchingHarnesses := harnessesByNamespace.Fetch(ctx, agentTemplate.Namespace, krt.FilterGeneric(func(object any) bool {
			harness := object.(*kagentv1alpha3.Harness)
			if harness.Spec.AllowedAgentTemplates == nil {
				return false
			}
			selector, err := metav1.LabelSelectorAsSelector(&harness.Spec.AllowedAgentTemplates.Selector)
			return err == nil && selector.Matches(labels.Set(agentTemplate.Labels))
		}))
		pairs := make([]AgentTemplateHarnessPair, 0, len(matchingHarnesses))
		for _, harness := range matchingHarnesses {
			pairs = append(pairs, AgentTemplateHarnessPair{AgentTemplate: agentTemplate, Harness: harness})
		}
		return pairs
	}, opts.WithName("AgentTemplateHarnessPairs")...)
}
