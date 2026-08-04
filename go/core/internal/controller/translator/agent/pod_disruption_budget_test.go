package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// translateBYOAgentWithDeployment is the BYO counterpart of translateAgentWithDeployment
// (deployment_annotations_test.go). BYO agents resolve their deployment through a separate
// function from declarative ones, so shared fields need coverage on both paths.
func translateBYOAgentWithDeployment(
	t *testing.T,
	deploymentSpec v1alpha2.SharedDeploymentSpec,
) *translator.AgentOutputs {
	t.Helper()

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "byo-agent", Namespace: "test"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_BYO,
			BYO: &v1alpha2.BYOAgentSpec{
				Deployment: &v1alpha2.ByoDeploymentSpec{
					Image:                "example.com/byo:latest",
					SharedDeploymentSpec: deploymentSpec,
				},
			},
		},
	}

	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent).Build()
	trans := translator.NewAdkApiTranslator(
		kubeClient, types.NamespacedName{Namespace: "test", Name: "test-model"}, nil, "", nil,
	)

	result, err := translator.TranslateAgent(context.Background(), trans, agent)
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

// findPodDisruptionBudget returns the PodDisruptionBudget in the manifest, or nil when the
// translator emitted none.
func findPodDisruptionBudget(manifest []client.Object) *policyv1.PodDisruptionBudget {
	for _, obj := range manifest {
		if pdb, ok := obj.(*policyv1.PodDisruptionBudget); ok {
			return pdb
		}
	}
	return nil
}

// TestPodDisruptionBudget_OmittedWhenUnset asserts the no-op path: an agent that does not
// ask for a budget gets no PodDisruptionBudget in its manifest. This is what allows the
// reconciler to prune a budget an agent previously requested.
func TestPodDisruptionBudget_OmittedWhenUnset(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{})

	assert.Nil(t, findPodDisruptionBudget(result.Manifest),
		"no PodDisruptionBudget should be emitted when podDisruptionBudget is unset")
}

func TestPodDisruptionBudget_MaxUnavailable(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(1)),
		},
	})

	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb, "PodDisruptionBudget should be in manifest")

	assert.Equal(t, "policy/v1", pdb.APIVersion)
	assert.Equal(t, "PodDisruptionBudget", pdb.Kind)
	assert.Equal(t, "test-agent", pdb.Name)
	assert.Equal(t, "test", pdb.Namespace)

	require.NotNil(t, pdb.Spec.MaxUnavailable)
	assert.Equal(t, intstr.FromInt32(1), *pdb.Spec.MaxUnavailable)
	assert.Nil(t, pdb.Spec.MinAvailable)
	assert.Nil(t, pdb.Spec.UnhealthyPodEvictionPolicy)
}

func TestPodDisruptionBudget_MinAvailablePercentage(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MinAvailable: new(intstr.FromString("50%")),
		},
	})

	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb)

	require.NotNil(t, pdb.Spec.MinAvailable)
	assert.Equal(t, intstr.FromString("50%"), *pdb.Spec.MinAvailable)
	assert.Nil(t, pdb.Spec.MaxUnavailable)
}

func TestPodDisruptionBudget_UnhealthyPodEvictionPolicy(t *testing.T) {
	policy := policyv1.AlwaysAllow
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MaxUnavailable:             new(intstr.FromInt32(1)),
			UnhealthyPodEvictionPolicy: &policy,
		},
	})

	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb)

	require.NotNil(t, pdb.Spec.UnhealthyPodEvictionPolicy)
	assert.Equal(t, policyv1.AlwaysAllow, *pdb.Spec.UnhealthyPodEvictionPolicy)
}

// TestPodDisruptionBudget_SelectorMatchesDeployment is the assertion that matters most: a
// budget whose selector does not match the Deployment selector silently protects nothing.
func TestPodDisruptionBudget_SelectorMatchesDeployment(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MaxUnavailable: new(intstr.FromInt32(1)),
		},
	})

	deployment := findDeployment(t, result)
	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb)

	require.NotNil(t, deployment.Spec.Selector)
	require.NotNil(t, pdb.Spec.Selector)
	assert.Equal(t, deployment.Spec.Selector.MatchLabels, pdb.Spec.Selector.MatchLabels,
		"PodDisruptionBudget selector must match the Deployment selector exactly")
	assert.NotEmpty(t, pdb.Spec.Selector.MatchLabels, "selector must not be empty")

	// The pod template must actually satisfy the selector, otherwise the budget matches
	// zero pods and permits unlimited disruption.
	for key, want := range pdb.Spec.Selector.MatchLabels {
		assert.Equal(t, want, deployment.Spec.Template.Labels[key],
			"pod template label %q should satisfy the budget selector", key)
	}
}

// TestPodDisruptionBudget_BYOAgent asserts the field works through the BYO resolve path,
// which is a separate function from the declarative one.
func TestPodDisruptionBudget_BYOAgent(t *testing.T) {
	result := translateBYOAgentWithDeployment(t, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MinAvailable: new(intstr.FromInt32(2)),
		},
	})

	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb, "BYO agents should also get a PodDisruptionBudget")

	require.NotNil(t, pdb.Spec.MinAvailable)
	assert.Equal(t, intstr.FromInt32(2), *pdb.Spec.MinAvailable)

	deployment := findDeployment(t, result)
	assert.Equal(t, deployment.Spec.Selector.MatchLabels, pdb.Spec.Selector.MatchLabels)
}

func TestPodDisruptionBudget_OmittedWhenUnsetForBYOAgent(t *testing.T) {
	result := translateBYOAgentWithDeployment(t, v1alpha2.SharedDeploymentSpec{})

	assert.Nil(t, findPodDisruptionBudget(result.Manifest))
}

// TestPodDisruptionBudget_RegisteredAsOwnedType asserts the type is in the owned-resource
// set. That set drives the prune pass, so a missing entry leaves a stale budget behind
// forever once an agent stops requesting one.
func TestPodDisruptionBudget_RegisteredAsOwnedType(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	trans := translator.NewAdkApiTranslator(
		kubeClient, types.NamespacedName{Namespace: "test", Name: "test-model"}, nil, "", nil,
	)

	var found bool
	for _, obj := range trans.GetOwnedResourceTypes() {
		if _, ok := obj.(*policyv1.PodDisruptionBudget); ok {
			found = true
			break
		}
	}
	assert.True(t, found,
		"PodDisruptionBudget must be in GetOwnedResourceTypes so stale budgets are pruned")
}

// TestPodDisruptionBudget_DoesNotMutateAgentSpec asserts the resolved deployment holds a
// copy, so translation never writes back into the agent object held by the client cache.
func TestPodDisruptionBudget_DoesNotMutateAgentSpec(t *testing.T) {
	spec := &v1alpha2.PodDisruptionBudgetSpec{
		MaxUnavailable: new(intstr.FromInt32(1)),
	}

	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		PodDisruptionBudget: spec,
	})

	pdb := findPodDisruptionBudget(result.Manifest)
	require.NotNil(t, pdb)

	// Mutating the emitted budget must not reach back into the spec the agent owns.
	pdb.Spec.MaxUnavailable = new(intstr.FromInt32(99))
	assert.Equal(t, intstr.FromInt32(1), *spec.MaxUnavailable,
		"translation must not share the IntOrString pointer with the agent spec")
}
