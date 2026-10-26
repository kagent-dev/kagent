package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

type namespaceDemand struct {
	Agent     string
	Namespace string
}

// ResourceName keys demand by Agent so namespace watches can track each consumer.
func (n namespaceDemand) ResourceName() string { return n.Agent }

// newReferenceNamespaces observes only namespaces whose Agents consume Selector
// policies. A field-selected list/watch permits resourceNames-scoped RBAC and
// never makes Same/All references depend on cluster-wide Namespace permissions.
// Agents in the same namespace share a watch, which stops when its last consumer
// leaves. Updating the returned KRT collection triggers dependent policy checks
// with the latest namespace labels or observation error.
func newReferenceNamespaces(client kube.Client, agents krt.Collection[*v1alpha3.Agent], inputs translator.Collections, opts krt.OptionsBuilder) krt.Collection[translator.NamespaceObservation] {
	observations := krt.NewStaticCollection[translator.NamespaceObservation](nil, nil, opts.WithName("ReferenceNamespaces")...)
	demands := krt.NewCollection(agents, func(ctx krt.HandlerContext, agent *v1alpha3.Agent) *namespaceDemand {
		if !translator.AgentNeedsNamespace(ctx, inputs, agent) {
			return nil
		}
		return &namespaceDemand{Agent: agent.Namespace + "/" + agent.Name, Namespace: agent.Namespace}
	}, opts.WithName("ReferenceNamespaceDemand")...)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-opts.Stop(); cancel() }()
	var mu sync.Mutex
	consumers := map[string]map[string]bool{}
	watchers := map[string]context.CancelFunc{}
	demands.Register(func(event krt.Event[namespaceDemand]) {
		mu.Lock()
		defer mu.Unlock()
		if event.Old != nil {
			old := *event.Old
			delete(consumers[old.Namespace], old.Agent)
			if len(consumers[old.Namespace]) == 0 {
				if stop := watchers[old.Namespace]; stop != nil {
					stop()
				}
				delete(watchers, old.Namespace)
				observations.DeleteObject(old.Namespace)
			}
		}
		if event.New != nil {
			demand := *event.New
			if consumers[demand.Namespace] == nil {
				consumers[demand.Namespace] = map[string]bool{}
			}
			consumers[demand.Namespace][demand.Agent] = true
			if watchers[demand.Namespace] == nil {
				watchCtx, stop := context.WithCancel(ctx)
				watchers[demand.Namespace] = stop
				observations.UpdateObject(translator.NamespaceObservation{Name: demand.Namespace, Error: "waiting for Namespace list/watch observation"})
				// Publishing is serialized with cancellation so an old watcher cannot
				// overwrite a replacement watcher's observation.
				publish := func(observation translator.NamespaceObservation) {
					mu.Lock()
					defer mu.Unlock()
					if watchCtx.Err() == nil {
						observations.UpdateObject(observation)
					}
				}
				go observeReferenceNamespace(watchCtx, client, demand.Namespace, publish)
			}
		}
	})
	return observations
}

// observeReferenceNamespace maintains observation until canceled. List/watch
// failures replace cached labels with an error so policy checks fail closed,
// then observation is retried after five seconds.
func observeReferenceNamespace(ctx context.Context, client kube.Client, name string, publish func(translator.NamespaceObservation)) {
	for ctx.Err() == nil {
		err := watchReferenceNamespace(ctx, client, name, publish)
		if ctx.Err() != nil {
			return
		}
		publish(translator.NamespaceObservation{Name: name, Error: fmt.Sprintf("requires Namespace list/watch access with metadata.name=%s: %v", name, err)})
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// watchReferenceNamespace lists one consuming namespace and watches from the
// list's resource version. It publishes the initial namespace and subsequent
// additions or updates for policy reevaluation, and publishes an error when the
// namespace is absent or deleted. Watch failures return to the retry loop.
func watchReferenceNamespace(ctx context.Context, client kube.Client, name string, publish func(translator.NamespaceObservation)) error {
	selector := fields.OneTermEqualSelector("metadata.name", name).String()
	namespaces := client.Kube().CoreV1().Namespaces()
	list, err := namespaces.List(ctx, metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return err
	}
	// Establish the watch before publishing labels, so watch RBAC denial never
	// temporarily authorizes a reference based on unobservable metadata.
	stream, err := namespaces.Watch(ctx, metav1.ListOptions{FieldSelector: selector, ResourceVersion: list.ResourceVersion})
	if err != nil {
		return err
	}
	defer stream.Stop()
	if len(list.Items) == 0 {
		publish(translator.NamespaceObservation{Name: name, Error: "namespace not found"})
	} else {
		publish(translator.NamespaceObservation{Name: name, Namespace: list.Items[0].DeepCopy()})
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-stream.ResultChan():
			if !ok {
				return fmt.Errorf("namespace watch closed; re-establishing observation")
			}
			switch event.Type {
			case watch.Added, watch.Modified:
				namespace, ok := event.Object.(*corev1.Namespace)
				if !ok {
					return fmt.Errorf("unexpected namespace watch object %T", event.Object)
				}
				publish(translator.NamespaceObservation{Name: name, Namespace: namespace.DeepCopy()})
			case watch.Deleted:
				publish(translator.NamespaceObservation{Name: name, Error: "namespace deleted"})
			case watch.Error:
				return fmt.Errorf("namespace watch failed: %v", event.Object)
			}
		}
	}
}
