package agentgateway

import (
	"context"
	"fmt"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const credentialIndex = "kagent.dev/connection-credential"

// SetupWithManager registers the optional connection controllers on an existing
// manager. Callers grant RBAC for gateway sources and derived resources.
// Annotation kagent.dev/connection=true selects sources within the manager's
// watched namespaces. Each manager uses one gateway configuration.
func SetupWithManager(manager ctrl.Manager, config Config) error {
	if err := config.validate(); err != nil {
		return err
	}
	for _, add := range []func(*runtime.Scheme) error{agw.Install, gatewayv1.Install, kagentv1alpha3.AddToScheme} {
		if err := add(manager.GetScheme()); err != nil {
			return fmt.Errorf("failed to register connection scheme: %w", err)
		}
	}
	for _, model := range []bool{true, false} {
		r := &reconciler{client: manager.GetClient(), scheme: manager.GetScheme(), config: config, model: model}
		if err := manager.GetFieldIndexer().IndexField(context.Background(), r.parent(), credentialIndex, func(object client.Object) []string {
			ref := credentialRef(object)
			if object.GetAnnotations()[ConnectionAnnotation] != "true" || ref == nil {
				return nil
			}
			return []string{string(ref.Name)}
		}); err != nil {
			return fmt.Errorf("failed to index connection credentials: %w", err)
		}
		name := "agentgateway-mcp-connection"
		controller := ctrl.NewControllerManagedBy(manager).
			For(r.parent(), builder.WithPredicates(predicate.Or[client.Object](
				predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{},
			))).
			Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSecret))
		if model {
			name = "agentgateway-model-connection"
			controller = controller.Owns(&kagentv1alpha3.ModelConfig{})
		} else {
			controller = controller.Owns(&kagentv1alpha3.RemoteMCPServer{}).Owns(&gatewayv1.HTTPRoute{}).Owns(&agw.AgentgatewayPolicy{})
		}
		if err := controller.Named(name).Complete(r); err != nil {
			return fmt.Errorf("failed to register connection controller: %w", err)
		}
	}
	return nil
}

func (r *reconciler) requestsForSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	var parents []client.Object
	if r.model {
		list := &agw.AgentgatewayModelList{}
		if err := r.client.List(ctx, list, client.InNamespace(secret.GetNamespace()), client.MatchingFields{credentialIndex: secret.GetName()}); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list model connections", "error", err)
			return nil
		}
		for i := range list.Items {
			parents = append(parents, &list.Items[i])
		}
	} else {
		list := &agw.AgentgatewayBackendList{}
		if err := r.client.List(ctx, list, client.InNamespace(secret.GetNamespace()), client.MatchingFields{credentialIndex: secret.GetName()}); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to list MCP connections", "error", err)
			return nil
		}
		for i := range list.Items {
			parents = append(parents, &list.Items[i])
		}
	}
	var requests []reconcile.Request
	for _, parent := range parents {
		ref := credentialRef(parent)
		if parent.GetAnnotations()[ConnectionAnnotation] == "true" && ref != nil && string(ref.Name) == secret.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
		}
	}
	return requests
}
