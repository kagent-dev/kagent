package controller

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type dependentRefFinder func(context.Context, client.Client, types.NamespacedName) []types.NamespacedName

type agentWatchFinders struct {
	modelConfig     dependentRefFinder
	remoteMCPServer dependentRefFinder
	mcpService      dependentRefFinder
	configMap       dependentRefFinder
	mcpServer       dependentRefFinder
}

func addOwnedResourceWatches(build *builder.Builder, mgr ctrl.Manager, owned []client.Object) (*builder.Builder, error) {
	for _, ownedType := range owned {
		gvk, err := apiutil.GVKForObject(ownedType, mgr.GetScheme())
		if err != nil {
			return nil, fmt.Errorf("resolve GVK for owned resource %T: %w", ownedType, err)
		}
		if _, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			if meta.IsNoMatchError(err) {
				continue
			}
			return nil, fmt.Errorf("resolve REST mapping for owned resource %s: %w", gvk.String(), err)
		}
		build = build.Owns(ownedType, builder.WithPredicates(ownedObjectPredicate{}, predicate.ResourceVersionChangedPredicate{}))
	}
	return build, nil
}

func addCommonAgentWatches(build *builder.Builder, mgr ctrl.Manager, finders agentWatchFinders) (*builder.Builder, error) {
	build = build.Watches(
		&v1alpha2.ModelConfig{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return reconcileRequestsForRefs(finders.modelConfig(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}))
		}),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	).Watches(
		&v1alpha2.RemoteMCPServer{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return reconcileRequestsForRefs(finders.remoteMCPServer(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}))
		}),
	).Watches(
		&corev1.Service{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return reconcileRequestsForRefs(finders.mcpService(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}))
		}),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	).Watches(
		&corev1.ConfigMap{},
		handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return reconcileRequestsForRefs(finders.configMap(ctx, mgr.GetClient(), types.NamespacedName{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}))
		}),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	)

	if _, err := mgr.GetRESTMapper().RESTMapping(mcpServerGK); err == nil {
		build = build.Watches(
			&v1alpha1.MCPServer{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return reconcileRequestsForRefs(finders.mcpServer(ctx, mgr.GetClient(), types.NamespacedName{
					Name:      obj.GetName(),
					Namespace: obj.GetNamespace(),
				}))
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)
	} else if !meta.IsNoMatchError(err) {
		return nil, fmt.Errorf("resolve REST mapping for %s: %w", mcpServerGK.String(), err)
	}

	return build, nil
}
