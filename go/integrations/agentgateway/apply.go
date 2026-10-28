package agentgateway

import (
	"context"
	"fmt"
	"reflect"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *reconciler) apply(ctx context.Context, parent, desired client.Object) error {
	gvk, err := apiutil.GVKForObject(desired, r.scheme)
	if err != nil {
		return fmt.Errorf("failed to resolve connection consumer kind: %w", err)
	}
	current := desired.DeepCopyObject().(client.Object)
	err = r.client.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to read connection consumer: %w", err)
	}
	if err == nil && !metav1.IsControlledBy(current, parent) {
		return fmt.Errorf("%s %s already has another owner", gvk.Kind, desired.GetName())
	}
	if err := controllerutil.SetControllerReference(parent, desired, r.scheme); err != nil {
		return fmt.Errorf("failed to set connection owner: %w", err)
	}
	desired.GetObjectKind().SetGroupVersionKind(gvk)
	if apierrors.IsNotFound(err) {
		if err := r.client.Create(ctx, desired, client.FieldOwner("kagent-agentgateway")); err != nil {
			return fmt.Errorf("failed to create connection consumer: %w", err)
		}
		return nil
	}
	desired.SetUID(current.GetUID())
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return fmt.Errorf("failed to encode connection consumer: %w", err)
	}
	apply := client.ApplyConfigurationFromUnstructured(&unstructured.Unstructured{Object: object})
	if err := r.client.Apply(ctx, apply, client.FieldOwner("kagent-agentgateway"), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply connection consumer: %w", err)
	}
	return nil
}

func (r *reconciler) status(ctx context.Context, parent client.Object, reconcileErr error) error {
	before := parent.DeepCopyObject().(client.Object)
	var conditions *[]metav1.Condition
	switch p := parent.(type) {
	case *agw.AgentgatewayModel:
		index := -1
		for i := range p.Status.Parents {
			if p.Status.Parents[i].ControllerName == "kagent.dev/agentgateway-connections" {
				index = i
				break
			}
		}
		if index < 0 {
			p.Status.Parents = append(p.Status.Parents, gatewayv1.RouteParentStatus{ControllerName: "kagent.dev/agentgateway-connections"})
			index = len(p.Status.Parents) - 1
		}
		p.Status.Parents[index].ParentRef = r.config.Gateway
		conditions = &p.Status.Parents[index].Conditions
	case *agw.AgentgatewayBackend:
		conditions = &p.Status.Conditions
	default:
		return fmt.Errorf("unsupported connection parent %T", parent)
	}
	condition := metav1.Condition{Type: ReadyCondition, Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "Kagent consumers have converged", ObservedGeneration: parent.GetGeneration()}
	if reconcileErr != nil {
		condition.Status, condition.Reason, condition.Message = metav1.ConditionFalse, "ReconcileFailed", reconcileErr.Error()
	}
	apimeta.SetStatusCondition(conditions, condition)
	if reflect.DeepEqual(before, parent) {
		return nil
	}
	return r.patchStatus(ctx, before, parent)
}

func (r *reconciler) clearStatus(ctx context.Context, parent client.Object) error {
	before := parent.DeepCopyObject().(client.Object)
	switch p := parent.(type) {
	case *agw.AgentgatewayModel:
		parents := p.Status.Parents[:0]
		for _, entry := range p.Status.Parents {
			if entry.ControllerName != "kagent.dev/agentgateway-connections" {
				parents = append(parents, entry)
			}
		}
		p.Status.Parents = parents
	case *agw.AgentgatewayBackend:
		apimeta.RemoveStatusCondition(&p.Status.Conditions, ReadyCondition)
	}
	if reflect.DeepEqual(before, parent) {
		return nil
	}
	return r.patchStatus(ctx, before, parent)
}

func (r *reconciler) patchStatus(ctx context.Context, before, parent client.Object) error {
	if err := r.client.Status().Patch(ctx, parent, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("failed to update connection status: %w", err)
	}
	return nil
}
