package controller

import (
	"context"
	"fmt"

	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func newEgressPolicyStatuses(policies krt.Collection[*kagentv1alpha3.EgressPolicy], secrets krt.Collection[*corev1.Secret], opts krt.OptionsBuilder) krt.StatusCollection[*kagentv1alpha3.EgressPolicy, kagentv1alpha3.EgressPolicyStatus] {
	statuses, _ := krt.NewStatusManyCollection(policies, func(ctx krt.HandlerContext, policy *kagentv1alpha3.EgressPolicy) (*kagentv1alpha3.EgressPolicyStatus, []struct{}) {
		condition := metav1.Condition{Type: kagentv1alpha3.EgressPolicyConditionTypeResolvedRefs, Status: metav1.ConditionTrue, Reason: "Resolved", Message: "All credential references resolved", ObservedGeneration: policy.Generation}
		for index, destination := range policy.Spec.Destinations {
			if destination.Authentication == nil || destination.Authentication.SecretKeyRef == nil {
				continue
			}
			reference := destination.Authentication.SecretKeyRef
			name := types.NamespacedName{Namespace: policy.Namespace, Name: reference.Name}
			secret := krt.FetchOne(ctx, secrets, krt.FilterObjectName(name))
			reason := ""
			if secret == nil {
				reason = kagentv1alpha3.EgressPolicyReasonSecretNotFound
			} else if _, found := (*secret).Data[reference.Key]; !found {
				reason = kagentv1alpha3.EgressPolicyReasonSecretKeyNotFound
			}
			if reason != "" && condition.Status == metav1.ConditionTrue {
				condition.Status = metav1.ConditionFalse
				condition.Reason = reason
				condition.Message = fmt.Sprintf("spec.destinations[%d].authentication.secretKeyRef: Secret %s key %q could not be resolved", index, name, reference.Key)
			}
		}
		return &kagentv1alpha3.EgressPolicyStatus{ObservedGeneration: policy.Generation, Conditions: []metav1.Condition{condition}}, nil
	}, opts.WithName("EgressPolicyStatuses")...)
	return statuses
}

func (r *Reconciler) reconcileEgressPolicyStatus(ctx context.Context, key string) error {
	desired := r.collections.EgressPolicyStatuses.GetKey(key)
	policy := r.collections.EgressPolicies.GetKey(key)
	if desired == nil || policy == nil {
		return nil
	}
	updated := (*policy).DeepCopy()
	updated.Status = desired.Status
	updated.Status.Conditions = conditionsWithTransitionTimes(updated.Status.Conditions, (*policy).Status.Conditions)
	if apiequality.Semantic.DeepEqual(updated.Status, (*policy).Status) {
		return nil
	}
	if _, err := r.status.EgressPolicies(updated.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update EgressPolicy %s status: %w", key, err)
	}
	return nil
}
