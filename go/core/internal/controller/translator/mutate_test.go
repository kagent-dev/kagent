package translator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func pdbWithSpec(spec policyv1.PodDisruptionBudgetSpec) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "test"},
		Spec:       spec,
	}
}

// TestMutateFuncFor_PodDisruptionBudget_SwitchingThresholdClearsTheOther is the regression
// test for the reason PodDisruptionBudget needs an explicit case in MutateFuncFor.
//
// minAvailable and maxUnavailable are mutually exclusive and the API server rejects a spec
// that sets both. The generic mergeWithOverride fallback cannot express the transition:
// mergo only writes keys present in the desired object, so the field being moved away from
// survives on the existing object and the update fails.
func TestMutateFuncFor_PodDisruptionBudget_SwitchingThresholdClearsTheOther(t *testing.T) {
	existing := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MinAvailable: new(intstr.FromInt32(2)),
		Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"kagent": "agent"}},
	})
	desired := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
		Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"kagent": "agent"}},
	})

	require.NoError(t, MutateFuncFor(existing, desired)())

	assert.Nil(t, existing.Spec.MinAvailable,
		"minAvailable must be cleared when the agent switches to maxUnavailable")
	require.NotNil(t, existing.Spec.MaxUnavailable)
	assert.Equal(t, intstr.FromInt32(1), *existing.Spec.MaxUnavailable)
}

func TestMutateFuncFor_PodDisruptionBudget_SwitchingBackClearsMaxUnavailable(t *testing.T) {
	existing := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
	})
	desired := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MinAvailable: new(intstr.FromString("50%")),
	})

	require.NoError(t, MutateFuncFor(existing, desired)())

	assert.Nil(t, existing.Spec.MaxUnavailable,
		"maxUnavailable must be cleared when the agent switches to minAvailable")
	require.NotNil(t, existing.Spec.MinAvailable)
	assert.Equal(t, intstr.FromString("50%"), *existing.Spec.MinAvailable)
}

// TestMutateFuncFor_PodDisruptionBudget_ClearsUnhealthyPodEvictionPolicy covers the same
// unset-a-field problem for the optional eviction policy.
func TestMutateFuncFor_PodDisruptionBudget_ClearsUnhealthyPodEvictionPolicy(t *testing.T) {
	policy := policyv1.AlwaysAllow
	existing := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable:             new(intstr.FromInt32(1)),
		UnhealthyPodEvictionPolicy: &policy,
	})
	desired := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
	})

	require.NoError(t, MutateFuncFor(existing, desired)())

	assert.Nil(t, existing.Spec.UnhealthyPodEvictionPolicy,
		"removing unhealthyPodEvictionPolicy from the agent spec must clear it on the object")
}

// TestMutateFuncFor_PodDisruptionBudget_UpdatesSelector guards against a stale selector
// surviving an update, which would leave the budget protecting the wrong pods.
func TestMutateFuncFor_PodDisruptionBudget_UpdatesSelector(t *testing.T) {
	existing := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
		Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"kagent": "old"}},
	})
	desired := pdbWithSpec(policyv1.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
		Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"kagent": "new"}},
	})

	require.NoError(t, MutateFuncFor(existing, desired)())

	require.NotNil(t, existing.Spec.Selector)
	assert.Equal(t, map[string]string{"kagent": "new"}, existing.Spec.Selector.MatchLabels)
}

// TestMutateFuncFor_PodDisruptionBudget_PreservesExistingLabelsAndAnnotations asserts the
// shared metadata handling in MutateFuncFor still applies: labels and annotations added
// out-of-band survive, while desired keys win on conflict.
func TestMutateFuncFor_PodDisruptionBudget_PreservesExistingLabelsAndAnnotations(t *testing.T) {
	existing := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "agent",
			Namespace:   "test",
			Labels:      map[string]string{"added-by-operator": "keep", "shared": "old"},
			Annotations: map[string]string{"added-by-operator": "keep", "shared": "old"},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(1)),
		},
	}
	desired := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "agent",
			Namespace:   "test",
			Labels:      map[string]string{"shared": "new"},
			Annotations: map[string]string{"shared": "new"},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(1)),
		},
	}

	require.NoError(t, MutateFuncFor(existing, desired)())

	assert.Equal(t, "keep", existing.Labels["added-by-operator"])
	assert.Equal(t, "new", existing.Labels["shared"])
	assert.Equal(t, "keep", existing.Annotations["added-by-operator"])
	assert.Equal(t, "new", existing.Annotations["shared"])
}
