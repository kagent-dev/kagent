package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestEgressPolicyReferenceStatusTracksSecrets(t *testing.T) {
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test", nil)
	policy := &kagentv1alpha3.EgressPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "egress", Generation: 3},
		Spec:       kagentv1alpha3.EgressPolicySpec{Destinations: []kagentv1alpha3.EgressDestination{{Origin: "https://example.com", Authentication: &kagentv1alpha3.EgressAuthentication{Header: "Authorization", SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "key"}}}}},
	}
	policies := krt.NewStaticCollection(nil, []*kagentv1alpha3.EgressPolicy{policy}, opts.WithName("Policies")...)
	secrets := krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...)
	statuses := newEgressPolicyStatuses(policies, secrets, opts)
	clientset := kagentfake.NewSimpleClientset(policy.DeepCopy())
	statusClient := clientset.ApiV1alpha3()
	reconciler := &Reconciler{collections: Collections{EgressPolicies: policies, EgressPolicyStatuses: statuses}, status: statusClient}
	assertStatus := func(reason string, expected metav1.ConditionStatus) {
		t.Helper()
		require.Eventually(t, func() bool {
			desired := statuses.GetKey("team-a/egress")
			return desired != nil && desired.Status.Conditions[0].Reason == reason
		}, time.Second*5, time.Millisecond)
		require.NoError(t, reconciler.reconcileEgressPolicyStatus(t.Context(), "team-a/egress"))
		updated, err := statusClient.EgressPolicies(policy.Namespace).Get(t.Context(), policy.Name, metav1.GetOptions{})
		require.NoError(t, err)
		condition := apimeta.FindStatusCondition(updated.Status.Conditions, kagentv1alpha3.EgressPolicyConditionTypeResolvedRefs)
		require.NotNil(t, condition)
		require.Equal(t, expected, condition.Status)
		require.Equal(t, policy.Generation, condition.ObservedGeneration)
		require.False(t, condition.LastTransitionTime.IsZero())
		policies.UpdateObject(updated)
		before := len(clientset.Actions())
		require.NoError(t, reconciler.reconcileEgressPolicyStatus(t.Context(), "team-a/egress"))
		require.Len(t, clientset.Actions(), before)
	}
	assertStatus(kagentv1alpha3.EgressPolicyReasonSecretNotFound, metav1.ConditionFalse)
	secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "credentials"}, Data: map[string][]byte{"key": {}}})
	assertStatus(kagentv1alpha3.EgressPolicyReasonSecretNotFound, metav1.ConditionFalse)
	secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: policy.Namespace, Name: "credentials"}})
	assertStatus(kagentv1alpha3.EgressPolicyReasonSecretKeyNotFound, metav1.ConditionFalse)
	secrets.UpdateObject(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: policy.Namespace, Name: "credentials"}, Data: map[string][]byte{"key": {}}})
	assertStatus("Resolved", metav1.ConditionTrue)
	secrets.DeleteObject("team-a/credentials")
	assertStatus(kagentv1alpha3.EgressPolicyReasonSecretNotFound, metav1.ConditionFalse)
	policy = policy.DeepCopy()
	policy.Generation++
	policy.Spec.Destinations[0].Authentication = nil
	policies.UpdateObject(policy)
	assertStatus("Resolved", metav1.ConditionTrue)
}
