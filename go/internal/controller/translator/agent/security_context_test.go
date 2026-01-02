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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
)

func TestSecurityContext_AppliedToPodSpec(t *testing.T) {
	ctx := context.Background()

	// Create a test agent with securityContext
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
						PodSecurityContext: &corev1.PodSecurityContext{
							RunAsUser:          ptr.To(int64(1000)),
							RunAsGroup:         ptr.To(int64(1000)),
							FSGroup:            ptr.To(int64(1000)),
							RunAsNonRoot:       ptr.To(true),
							SupplementalGroups: []int64{1000},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                ptr.To(int64(1000)),
							RunAsGroup:               ptr.To(int64(1000)),
							RunAsNonRoot:             ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
								Add:  []corev1.Capability{"NET_BIND_SERVICE"},
							},
						},
					},
				},
			},
		},
	}

	// Create model config
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

	// Set up fake client
	scheme := schemev1.Scheme
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, modelConfig).
		Build()

	// Create translator
	defaultModel := types.NamespacedName{
		Namespace: "test",
		Name:      "test-model",
	}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

	// Translate agent
	result, err := translatorInstance.TranslateAgent(ctx, agent)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Extract deployment from manifest
	var deployment *appsv1.Deployment
	for _, obj := range result.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			deployment = dep
			break
		}
	}
	require.NotNil(t, deployment, "Deployment should be in manifest")
	podTemplate := &deployment.Spec.Template

	// Verify pod-level security context
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext, "Pod securityContext should be set")
	assert.Equal(t, int64(1000), *podSecurityContext.RunAsUser, "Pod runAsUser should be 1000")
	assert.Equal(t, int64(1000), *podSecurityContext.RunAsGroup, "Pod runAsGroup should be 1000")
	assert.Equal(t, int64(1000), *podSecurityContext.FSGroup, "Pod fsGroup should be 1000")
	assert.True(t, *podSecurityContext.RunAsNonRoot, "Pod runAsNonRoot should be true")
	assert.Equal(t, []int64{1000}, podSecurityContext.SupplementalGroups, "Pod supplementalGroups should be [1000]")

	// Verify container-level security context
	require.Len(t, podTemplate.Spec.Containers, 1, "Should have one container")
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext, "Container securityContext should be set")
	assert.Equal(t, int64(1000), *containerSecurityContext.RunAsUser, "Container runAsUser should be 1000")
	assert.Equal(t, int64(1000), *containerSecurityContext.RunAsGroup, "Container runAsGroup should be 1000")
	assert.True(t, *containerSecurityContext.RunAsNonRoot, "Container runAsNonRoot should be true")
	assert.False(t, *containerSecurityContext.AllowPrivilegeEscalation, "Container allowPrivilegeEscalation should be false")

	// Verify capabilities
	require.NotNil(t, containerSecurityContext.Capabilities, "Container capabilities should be set")
	assert.Contains(t, containerSecurityContext.Capabilities.Drop, corev1.Capability("ALL"), "Should drop ALL capabilities")
	assert.Contains(t, containerSecurityContext.Capabilities.Add, corev1.Capability("NET_BIND_SERVICE"), "Should add NET_BIND_SERVICE capability")
}

func TestSecurityContext_OnlyPodSecurityContext(t *testing.T) {
	ctx := context.Background()

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
						PodSecurityContext: &corev1.PodSecurityContext{
							RunAsUser:  ptr.To(int64(2000)),
							RunAsGroup: ptr.To(int64(2000)),
						},
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
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, modelConfig).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: "test",
		Name:      "test-model",
	}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

	result, err := translatorInstance.TranslateAgent(ctx, agent)
	require.NoError(t, err)

	var deployment *appsv1.Deployment
	for _, obj := range result.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			deployment = dep
			break
		}
	}
	require.NotNil(t, deployment)
	podTemplate := &deployment.Spec.Template

	// Verify pod security context is set
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext)
	assert.Equal(t, int64(2000), *podSecurityContext.RunAsUser)
	assert.Equal(t, int64(2000), *podSecurityContext.RunAsGroup)

	// Container security context should be nil if not specified
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	assert.Nil(t, containerSecurityContext, "Container securityContext should be nil when not specified")
}

func TestSecurityContext_OnlyContainerSecurityContext(t *testing.T) {
	ctx := context.Background()

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
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:  ptr.To(int64(3000)),
							RunAsGroup: ptr.To(int64(3000)),
						},
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
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, modelConfig).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: "test",
		Name:      "test-model",
	}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

	result, err := translatorInstance.TranslateAgent(ctx, agent)
	require.NoError(t, err)

	var deployment *appsv1.Deployment
	for _, obj := range result.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			deployment = dep
			break
		}
	}
	require.NotNil(t, deployment)
	podTemplate := &deployment.Spec.Template

	// Pod security context should be nil if not specified
	podSecurityContext := podTemplate.Spec.SecurityContext
	assert.Nil(t, podSecurityContext, "Pod securityContext should be nil when not specified")

	// Container security context should be set
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.Equal(t, int64(3000), *containerSecurityContext.RunAsUser)
	assert.Equal(t, int64(3000), *containerSecurityContext.RunAsGroup)
}

func TestSecurityContext_WithSandbox(t *testing.T) {
	ctx := context.Background()

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Skills: &v1alpha2.SkillForAgent{
				Refs: []string{"test-skill:latest"},
			},
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "test-model",
				Deployment: &v1alpha2.DeclarativeDeploymentSpec{
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:  ptr.To(int64(1000)),
							RunAsGroup: ptr.To(int64(1000)),
						},
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
	err := v1alpha2.AddToScheme(scheme)
	require.NoError(t, err)

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent, modelConfig).
		Build()

	defaultModel := types.NamespacedName{
		Namespace: "test",
		Name:      "test-model",
	}
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "")

	result, err := translatorInstance.TranslateAgent(ctx, agent)
	require.NoError(t, err)

	var deployment *appsv1.Deployment
	for _, obj := range result.Manifest {
		if dep, ok := obj.(*appsv1.Deployment); ok {
			deployment = dep
			break
		}
	}
	require.NotNil(t, deployment)
	podTemplate := &deployment.Spec.Template

	// When sandbox is needed, Privileged should be set even if user provided securityContext
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.True(t, *containerSecurityContext.Privileged, "Privileged should be true when sandbox is needed")
	assert.Equal(t, int64(1000), *containerSecurityContext.RunAsUser, "User-provided runAsUser should still be set")
}
