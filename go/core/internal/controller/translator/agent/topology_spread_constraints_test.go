package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

// TestTopologySpreadConstraints_AppliedToPodSpec asserts a declarative agent's
// topologySpreadConstraints reach the rendered Deployment pod template spec.
func TestTopologySpreadConstraints_AppliedToPodSpec(t *testing.T) {
	ctx := context.Background()

	tsc := []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "kubernetes.io/hostname",
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-agent"},
			},
		},
		{
			MaxSkew:           2,
			TopologyKey:       "topology.kubernetes.io/zone",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-agent"},
			},
		},
	}

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "test-model",
				Deployment: &v1alpha2.DeclarativeDeploymentSpec{
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						TopologySpreadConstraints: tsc,
					},
				},
			},
		},
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "test",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: "OpenAI",
			Model:    "gpt-4o",
		},
	}

	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, modelConfig).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: "test",
		Name:      "test-model",
	}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	result, err := translator.TranslateAgent(ctx, translatorInstance, agent)
	require.NoError(t, err)
	require.NotNil(t, result)

	var deployment *appsv1.Deployment
	for _, obj := range result.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			deployment = dep
			break
		}
	}
	require.NotNil(t, deployment, "Deployment should be in manifest")

	podTemplate := &deployment.Spec.Template
	require.Equal(t, tsc, podTemplate.Spec.TopologySpreadConstraints,
		"topologySpreadConstraints from the agent spec should land on the pod template")
}

// TestTopologySpreadConstraints_UnsetOmittedFromPodSpec asserts the no-op path
// leaves the pod template without any topologySpreadConstraints, matching the
// pre-field behavior.
func TestTopologySpreadConstraints_UnsetOmittedFromPodSpec(t *testing.T) {
	result := translateAgentWithDeployment(t, nil, v1alpha2.SharedDeploymentSpec{})

	deployment := findDeployment(t, result)
	assert.Empty(t, deployment.Spec.Template.Spec.TopologySpreadConstraints,
		"no topologySpreadConstraints should be set when the field is unset")
}
