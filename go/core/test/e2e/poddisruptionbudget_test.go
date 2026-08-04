package e2e_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// TestE2EAgentPodDisruptionBudget covers the full lifecycle of the optional agent
// PodDisruptionBudget: it is created with the agent, its selector actually matches the
// agent's pods, updates are applied in place, and removing the field from the spec prunes
// the budget instead of orphaning it.
func TestE2EAgentPodDisruptionBudget(t *testing.T) {
	baseURL, stopServer := setupMockServer(t, "mocks/invoke_inline_agent.json")
	defer stopServer()

	cli := setupK8sClient(t, false)
	modelCfg := setupModelConfig(t, cli, baseURL)

	maxUnavailable := intstr.FromInt32(1)
	alwaysAllow := policyv1.AlwaysAllow
	agent := setupAgentWithOptions(t, cli, modelCfg.Name, nil, AgentOptions{
		Name:     "pdb-agent",
		Replicas: new(int32(2)),
		PodDisruptionBudget: &v1alpha2.PodDisruptionBudgetSpec{
			MaxUnavailable:             &maxUnavailable,
			UnhealthyPodEvictionPolicy: &alwaysAllow,
		},
	})

	pdbKey := types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}

	// The budget is created alongside the Deployment.
	pdb := &policyv1.PodDisruptionBudget{}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, cli.Get(t.Context(), pdbKey, pdb))
	}, 2*time.Minute, time.Second, "PodDisruptionBudget should be created for the agent")

	require.NotNil(t, pdb.Spec.MaxUnavailable)
	assert.Equal(t, intstr.FromInt32(1), *pdb.Spec.MaxUnavailable)
	assert.Nil(t, pdb.Spec.MinAvailable)
	require.NotNil(t, pdb.Spec.UnhealthyPodEvictionPolicy)
	assert.Equal(t, policyv1.AlwaysAllow, *pdb.Spec.UnhealthyPodEvictionPolicy)

	// It must be owned by the agent, so `kubectl delete agent` garbage collects it.
	require.Len(t, pdb.OwnerReferences, 1)
	assert.Equal(t, "Agent", pdb.OwnerReferences[0].Kind)
	assert.Equal(t, agent.Name, pdb.OwnerReferences[0].Name)

	// A budget whose selector does not match the Deployment protects nothing, so assert
	// against the live Deployment rather than trusting the translator.
	deployment := &appsv1.Deployment{}
	require.NoError(t, cli.Get(t.Context(), pdbKey, deployment))
	require.NotNil(t, deployment.Spec.Selector)
	require.NotNil(t, pdb.Spec.Selector)
	assert.Equal(t, deployment.Spec.Selector.MatchLabels, pdb.Spec.Selector.MatchLabels,
		"budget selector must match the Deployment selector")

	// The API server reports how many pods the budget actually matches, which is the real
	// proof the selector is correct.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		live := &policyv1.PodDisruptionBudget{}
		if !assert.NoError(c, cli.Get(t.Context(), pdbKey, live)) {
			return
		}
		assert.Equal(c, int32(2), live.Status.ExpectedPods,
			"budget should match the agent's 2 pods")
	}, 2*time.Minute, 2*time.Second)

	t.Run("switching to minAvailable updates the budget in place", func(t *testing.T) {
		minAvailable := intstr.FromInt32(1)
		updateAgentPDB(t, cli, pdbKey, &v1alpha2.PodDisruptionBudgetSpec{
			MinAvailable: new(minAvailable),
		})

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			live := &policyv1.PodDisruptionBudget{}
			if !assert.NoError(c, cli.Get(t.Context(), pdbKey, live)) {
				return
			}
			if !assert.NotNil(c, live.Spec.MinAvailable) {
				return
			}
			assert.Equal(c, intstr.FromInt32(1), *live.Spec.MinAvailable)
			// The threshold being moved away from has to be cleared, otherwise the
			// API server rejects the update for setting both.
			assert.Nil(c, live.Spec.MaxUnavailable)
			// Dropping the policy from the spec must clear it on the object too.
			assert.Nil(c, live.Spec.UnhealthyPodEvictionPolicy)
		}, 2*time.Minute, time.Second)
	})

	t.Run("removing the field prunes the budget", func(t *testing.T) {
		updateAgentPDB(t, cli, pdbKey, nil)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			live := &policyv1.PodDisruptionBudget{}
			err := cli.Get(t.Context(), pdbKey, live)
			assert.True(c, apierrors.IsNotFound(err),
				"budget should be pruned once the agent stops requesting one, got err=%v", err)
		}, 2*time.Minute, time.Second)

		// Pruning the budget must not disturb the Deployment.
		live := &appsv1.Deployment{}
		assert.NoError(t, cli.Get(t.Context(), pdbKey, live))
	})
}

// updateAgentPDB sets (or clears, when spec is nil) the agent's podDisruptionBudget,
// retrying on conflict since the controller writes status concurrently.
func updateAgentPDB(
	t *testing.T,
	cli client.Client,
	key types.NamespacedName,
	spec *v1alpha2.PodDisruptionBudgetSpec,
) {
	t.Helper()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		agent := &v1alpha2.Agent{}
		if !assert.NoError(c, cli.Get(t.Context(), key, agent)) {
			return
		}
		agent.Spec.Declarative.Deployment.PodDisruptionBudget = spec
		assert.NoError(c, cli.Update(t.Context(), agent))
	}, 30*time.Second, time.Second)
}
