package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
	"github.com/kagent-dev/kagent/go/core/pkg/consts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// translateAgentWithDeployment runs the translator against a declarative agent whose
// deployment spec is supplied by the caller, and returns the resulting manifest.
func translateAgentWithDeployment(
	t *testing.T,
	agentAnnotations map[string]string,
	deploymentSpec v1alpha2.SharedDeploymentSpec,
) *translator.AgentOutputs {
	t.Helper()

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-agent",
			Namespace:   "test",
			Annotations: agentAnnotations,
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "test-model",
				Deployment: &v1alpha2.DeclarativeDeploymentSpec{
					SharedDeploymentSpec: deploymentSpec,
				},
			},
		},
	}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test-model", Namespace: "test"},
		Spec:       v1alpha2.ModelConfigSpec{Provider: "OpenAI", Model: "gpt-4o"},
	}

	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(agent, modelConfig).Build()
	defaultModel := types.NamespacedName{Namespace: "test", Name: "test-model"}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	result, err := translator.TranslateAgent(context.Background(), translatorInstance, agent)
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

// TestDeploymentAnnotations_LandOnDeploymentOnly asserts deploymentAnnotations reach the
// Deployment metadata while annotations reach the pod template, and that neither leaks
// onto the other objects the translator emits.
func TestDeploymentAnnotations_LandOnDeploymentOnly(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{
		Annotations:           map[string]string{"pod.example.com/inject": "pod-value"},
		DeploymentAnnotations: map[string]string{"deployment.example.com/owner": "platform-team"},
	})

	deployment := findDeployment(t, result)

	// deploymentAnnotations land on the Deployment object metadata.
	assert.Equal(t, "platform-team", deployment.Annotations["deployment.example.com/owner"])
	// ...and not on the pod template.
	assert.NotContains(t, deployment.Spec.Template.Annotations, "deployment.example.com/owner")

	// annotations stay scoped to the pod template, alongside the config hash the
	// translator always writes there.
	assert.Equal(t, "pod-value", deployment.Spec.Template.Annotations["pod.example.com/inject"])
	assert.Contains(t, deployment.Spec.Template.Annotations, consts.ConfigHashAnnotation)
	assert.NotContains(t, deployment.Annotations, "pod.example.com/inject")

	// Every other emitted object keeps the plain object metadata.
	for _, obj := range result.Manifest {
		if _, ok := obj.(*appsv1.Deployment); ok {
			continue
		}
		assert.NotContains(t, obj.GetAnnotations(), "deployment.example.com/owner",
			"deploymentAnnotations leaked onto %T %s", obj, obj.GetName())
		assert.NotContains(t, obj.GetAnnotations(), "pod.example.com/inject",
			"pod annotations leaked onto %T %s", obj, obj.GetName())
	}

	// The Service and ServiceAccount are the objects most likely to regress here,
	// since they share the same object metadata helper as the Deployment.
	var sawService, sawServiceAccount bool
	for _, obj := range result.Manifest {
		switch obj.(type) {
		case *corev1.Service:
			sawService = true
		case *corev1.ServiceAccount:
			sawServiceAccount = true
		}
	}
	assert.True(t, sawService, "Service should be in manifest")
	assert.True(t, sawServiceAccount, "ServiceAccount should be in manifest")
}

// TestDeploymentAnnotations_MergeWithAgentMetadata asserts inherited agent metadata
// annotations survive on the Deployment and that a colliding deploymentAnnotations key
// wins, without mutating the agent object itself.
func TestDeploymentAnnotations_MergeWithAgentMetadata(t *testing.T) {
	agentAnnotations := map[string]string{
		"inherited.example.com/keep": "from-agent",
		"shared.example.com/key":     "from-agent",
	}

	result := translateAgentWithDeployment(t, agentAnnotations, v1alpha2.SharedDeploymentSpec{
		DeploymentAnnotations: map[string]string{"shared.example.com/key": "from-spec"},
	})

	deployment := findDeployment(t, result)
	assert.Equal(t, "from-agent", deployment.Annotations["inherited.example.com/keep"])
	assert.Equal(t, "from-spec", deployment.Annotations["shared.example.com/key"])

	// The inherited map must be cloned before merging, so the agent object held by the
	// client cache is never rewritten.
	assert.Equal(t, "from-agent", agentAnnotations["shared.example.com/key"])
}

// TestDeploymentAnnotations_UnsetKeepsInheritedMetadata asserts the no-op path leaves
// the Deployment metadata exactly as it was before this field existed.
func TestDeploymentAnnotations_UnsetKeepsInheritedMetadata(t *testing.T) {
	result := translateAgentWithDeployment(t,
		map[string]string{"inherited.example.com/keep": "from-agent"},
		v1alpha2.SharedDeploymentSpec{},
	)

	deployment := findDeployment(t, result)
	assert.Equal(t, "from-agent", deployment.Annotations["inherited.example.com/keep"])
}

// TestObjectMeta_DoesNotMutateAgentAnnotations asserts that building the ServiceAccount
// does not write serviceAccountConfig annotations back into the agent's own annotations.
// The ServiceAccount extends the shared object metadata in place, so unless the inherited
// map is cloned it is the very map owned by the agent object in the client cache, and the
// extra keys leak onto every other object built from that metadata.
func TestObjectMeta_DoesNotMutateAgentAnnotations(t *testing.T) {
	agentAnnotations := map[string]string{"inherited.example.com/keep": "from-agent"}

	result := translateAgentWithDeployment(t, agentAnnotations, v1alpha2.SharedDeploymentSpec{
		ServiceAccountConfig: &v1alpha2.ServiceAccountConfig{
			Annotations: map[string]string{"sa.example.com/role": "reader"},
		},
	})

	var serviceAccount *corev1.ServiceAccount
	for _, obj := range result.Manifest {
		if sa, ok := obj.(*corev1.ServiceAccount); ok {
			serviceAccount = sa
			break
		}
	}
	require.NotNil(t, serviceAccount, "ServiceAccount should be in manifest")

	// The ServiceAccount carries both the inherited and the configured annotations.
	assert.Equal(t, "from-agent", serviceAccount.Annotations["inherited.example.com/keep"])
	assert.Equal(t, "reader", serviceAccount.Annotations["sa.example.com/role"])

	// The agent's own annotations are untouched.
	assert.Equal(t, map[string]string{"inherited.example.com/keep": "from-agent"}, agentAnnotations)

	// ...and the ServiceAccount-only annotation reaches no other object.
	for _, obj := range result.Manifest {
		if _, ok := obj.(*corev1.ServiceAccount); ok {
			continue
		}
		assert.NotContains(t, obj.GetAnnotations(), "sa.example.com/role",
			"serviceAccountConfig annotations leaked onto %T %s", obj, obj.GetName())
	}
}
