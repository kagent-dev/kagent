package controller

import (
	"context"
	"fmt"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/config/schema/kubeclient"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
)

// Collections contains the Kubernetes inputs used to resolve an AgentTemplate
// and the template/harness pairs derived from Harness admission selectors.
type Collections struct {
	AgentTemplates   krt.Collection[*kagentv1alpha3.AgentTemplate]
	Harnesses        krt.Collection[*kagentv1alpha3.Harness]
	ModelConfigs     krt.Collection[*kagentv1alpha3.ModelConfig]
	RemoteMCPServers krt.Collection[*kagentv1alpha3.RemoteMCPServer]
	ConfigMaps       krt.Collection[*corev1.ConfigMap]
	Secrets          krt.Collection[*corev1.Secret]
	WorkerPools      krt.Collection[*atev1alpha1.WorkerPool]
	ActorTemplates   krt.Collection[*atev1alpha1.ActorTemplate]
	Pairs            krt.Collection[AgentTemplateHarnessPair]
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
func NewCollections(client kube.Client, watchNamespaces []string, opts krt.OptionsBuilder) (Collections, error) {
	if err := registerCustomResources(client.RESTConfig()); err != nil {
		return Collections{}, err
	}

	agentTemplates := typedCollection[*kagentv1alpha3.AgentTemplate](client, watchNamespaces, "AgentTemplates", opts)
	harnesses := typedCollection[*kagentv1alpha3.Harness](client, watchNamespaces, "Harnesses", opts)

	collections := Collections{
		AgentTemplates:   agentTemplates,
		Harnesses:        harnesses,
		ModelConfigs:     typedCollection[*kagentv1alpha3.ModelConfig](client, watchNamespaces, "ModelConfigs", opts),
		RemoteMCPServers: typedCollection[*kagentv1alpha3.RemoteMCPServer](client, watchNamespaces, "RemoteMCPServers", opts),
		ConfigMaps:       typedCollection[*corev1.ConfigMap](client, watchNamespaces, "ConfigMaps", opts),
		Secrets:          typedCollection[*corev1.Secret](client, watchNamespaces, "Secrets", opts),
		WorkerPools:      typedCollection[*atev1alpha1.WorkerPool](client, watchNamespaces, "WorkerPools", opts),
		ActorTemplates:   typedCollection[*atev1alpha1.ActorTemplate](client, watchNamespaces, "ActorTemplates", opts),
	}
	collections.Pairs = newPairCollection(agentTemplates, harnesses, opts)
	return collections, nil
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

// registerCustomResources teaches KRT how to list and watch CRDs that are not
// part of Istio's built-in client registry. The collections remain fully typed;
// these registrations can be replaced by generated clients if kagent adds them.
func registerCustomResources(config *rest.Config) error {
	scheme := runtime.NewScheme()
	if err := kagentv1alpha3.AddToScheme(scheme); err != nil {
		return fmt.Errorf("register kagent API types: %w", err)
	}
	if err := atev1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("register Substrate API types: %w", err)
	}

	kagentClient, err := restClient(config, kagentv1alpha3.GroupVersion, scheme)
	if err != nil {
		return fmt.Errorf("create kagent REST client: %w", err)
	}
	ateClient, err := restClient(config, atev1alpha1.GroupVersion, scheme)
	if err != nil {
		return fmt.Errorf("create Substrate REST client: %w", err)
	}

	registerResource[*kagentv1alpha3.AgentTemplate](kagentClient, kagentv1alpha3.GroupVersion, "AgentTemplate", "agenttemplates", func() runtime.Object { return &kagentv1alpha3.AgentTemplateList{} })
	registerResource[*kagentv1alpha3.Harness](kagentClient, kagentv1alpha3.GroupVersion, "Harness", "harnesses", func() runtime.Object { return &kagentv1alpha3.HarnessList{} })
	registerResource[*kagentv1alpha3.ModelConfig](kagentClient, kagentv1alpha3.GroupVersion, "ModelConfig", "modelconfigs", func() runtime.Object { return &kagentv1alpha3.ModelConfigList{} })
	registerResource[*kagentv1alpha3.RemoteMCPServer](kagentClient, kagentv1alpha3.GroupVersion, "RemoteMCPServer", "remotemcpservers", func() runtime.Object { return &kagentv1alpha3.RemoteMCPServerList{} })
	registerResource[*atev1alpha1.WorkerPool](ateClient, atev1alpha1.GroupVersion, "WorkerPool", "workerpools", func() runtime.Object { return &atev1alpha1.WorkerPoolList{} })
	registerResource[*atev1alpha1.ActorTemplate](ateClient, atev1alpha1.GroupVersion, "ActorTemplate", "actortemplates", func() runtime.Object { return &atev1alpha1.ActorTemplateList{} })
	return nil
}

func restClient(config *rest.Config, groupVersion schema.GroupVersion, scheme *runtime.Scheme) (rest.Interface, error) {
	copy := rest.CopyConfig(config)
	copy.GroupVersion = &groupVersion
	copy.APIPath = "/apis"
	copy.NegotiatedSerializer = serializer.NewCodecFactory(scheme)
	return rest.RESTClientFor(copy)
}

// registerResource supplies KRT with the typed list/watch operations normally
// provided by a generated clientset. Controllers are read-only at this layer.
func registerResource[T controllers.ComparableObject](client rest.Interface, groupVersion schema.GroupVersion, kind, resource string, newList func() runtime.Object) {
	gvr := groupVersion.WithResource(resource)
	kubeclient.Register[T](gvr, groupVersion.WithKind(kind),
		func(_ kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (runtime.Object, error) {
			list := newList()
			err := client.Get().Namespace(namespace).Resource(resource).VersionedParams(&options, metav1.ParameterCodec).Do(context.Background()).Into(list)
			return list, err
		},
		func(_ kubeclient.ClientGetter, namespace string, options metav1.ListOptions) (watch.Interface, error) {
			return client.Get().Namespace(namespace).Resource(resource).VersionedParams(&options, metav1.ParameterCodec).Watch(context.Background())
		},
		func(kubeclient.ClientGetter, string) kubetypes.WriteAPI[T] { return nil },
	)
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
