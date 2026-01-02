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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

// OpenShift SCC (Security Context Constraints) Test Scenarios
// These tests verify compatibility with OpenShift's SCC policies:
// - restricted: Most restrictive, requires specific UID/GID ranges
// - anyuid: Allows running as any user ID
// - privileged: Allows privileged containers

// TestSecurityContext_OpenShiftSCC_Restricted tests that agents can run under
// OpenShift's restricted SCC which enforces:
// - RunAsNonRoot: true
// - Specific UID/GID ranges (typically allocated by the namespace)
// - No privilege escalation
// - Capabilities dropped to minimum
func TestSecurityContext_OpenShiftSCC_Restricted(t *testing.T) {
	ctx := context.Background()

	// OpenShift restricted SCC requires specific security settings
	// UID ranges are typically allocated per-namespace (e.g., 1000680000-1000689999)
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
							RunAsNonRoot: ptr.To(true),
							FSGroup:      ptr.To(int64(1000680000)),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify pod security context meets restricted SCC requirements
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext, "Pod securityContext should be set for restricted SCC")
	assert.True(t, *podSecurityContext.RunAsNonRoot, "RunAsNonRoot must be true for restricted SCC")
	assert.Equal(t, int64(1000680000), *podSecurityContext.FSGroup, "FSGroup should be in OpenShift allocated range")
	require.NotNil(t, podSecurityContext.SeccompProfile, "SeccompProfile should be set")
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSecurityContext.SeccompProfile.Type)

	// Verify container security context meets restricted SCC requirements
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext, "Container securityContext should be set for restricted SCC")
	assert.True(t, *containerSecurityContext.RunAsNonRoot, "Container runAsNonRoot must be true")
	assert.False(t, *containerSecurityContext.AllowPrivilegeEscalation, "AllowPrivilegeEscalation must be false")
	require.NotNil(t, containerSecurityContext.Capabilities, "Capabilities should be set")
	assert.Contains(t, containerSecurityContext.Capabilities.Drop, corev1.Capability("ALL"), "Should drop ALL capabilities")
	assert.Empty(t, containerSecurityContext.Capabilities.Add, "No capabilities should be added for restricted SCC")
}

// TestSecurityContext_OpenShiftSCC_Anyuid tests that agents can run under
// OpenShift's anyuid SCC which allows:
// - Running as any user ID (including root)
// - Still requires security best practices for containers
func TestSecurityContext_OpenShiftSCC_Anyuid(t *testing.T) {
	ctx := context.Background()

	// Anyuid SCC allows running as specific UID without OpenShift range restrictions
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
							RunAsUser:  ptr.To(int64(1000)),
							RunAsGroup: ptr.To(int64(1000)),
							FSGroup:    ptr.To(int64(1000)),
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                ptr.To(int64(1000)),
							RunAsGroup:               ptr.To(int64(1000)),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify pod security context for anyuid SCC
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext)
	assert.Equal(t, int64(1000), *podSecurityContext.RunAsUser, "Anyuid allows specific user ID")
	assert.Equal(t, int64(1000), *podSecurityContext.RunAsGroup)
	assert.Equal(t, int64(1000), *podSecurityContext.FSGroup)

	// Verify container security context for anyuid SCC
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.Equal(t, int64(1000), *containerSecurityContext.RunAsUser)
	assert.False(t, *containerSecurityContext.AllowPrivilegeEscalation, "Should still prevent privilege escalation")
}

// TestSecurityContext_OpenShiftSCC_Privileged tests agents that require
// privileged access, such as those using sandboxed code execution.
// Note: This SCC should only be used when absolutely necessary.
func TestSecurityContext_OpenShiftSCC_Privileged(t *testing.T) {
	ctx := context.Background()

	// When skills are used, kagent requires privileged mode for sandbox
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Skills: &v1alpha2.SkillForAgent{
				Refs: []string{"code-exec-skill:latest"},
			},
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test agent",
				ModelConfig:   "test-model",
				Deployment: &v1alpha2.DeclarativeDeploymentSpec{
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						PodSecurityContext: &corev1.PodSecurityContext{
							RunAsUser:  ptr.To(int64(0)),
							RunAsGroup: ptr.To(int64(0)),
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify privileged container for sandbox
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.True(t, *containerSecurityContext.Privileged, "Privileged must be true for sandbox execution")

	// Pod security context should still be respected
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext)
	assert.Equal(t, int64(0), *podSecurityContext.RunAsUser, "Should run as root when privileged")
}

// Pod Security Standards (PSS) Test Scenarios
// These tests verify compatibility with Kubernetes Pod Security Standards:
// - restricted: Highly restricted, following current pod hardening best practices
// - baseline: Minimally restrictive, prevents known privilege escalations
// - privileged: Unrestricted policy

// TestSecurityContext_PSS_RestrictedV2 tests that agents can run under the
// Pod Security Standards "restricted" profile (v2), which is the most secure
// and requires:
// - Running as non-root
// - Seccomp profile set to RuntimeDefault or Localhost
// - Dropping all capabilities
// - No privilege escalation
// - Read-only root filesystem (recommended)
func TestSecurityContext_PSS_RestrictedV2(t *testing.T) {
	ctx := context.Background()

	// PSS restricted profile (v2) configuration
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
							RunAsNonRoot: ptr.To(true),
							RunAsUser:    ptr.To(int64(65534)), // nobody user
							RunAsGroup:   ptr.To(int64(65534)),
							FSGroup:      ptr.To(int64(65534)),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             ptr.To(true),
							RunAsUser:                ptr.To(int64(65534)),
							RunAsGroup:               ptr.To(int64(65534)),
							AllowPrivilegeEscalation: ptr.To(false),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify pod security context meets PSS restricted requirements
	podSecurityContext := podTemplate.Spec.SecurityContext
	require.NotNil(t, podSecurityContext, "Pod securityContext is required for PSS restricted")
	assert.True(t, *podSecurityContext.RunAsNonRoot, "RunAsNonRoot must be true for PSS restricted")
	assert.Equal(t, int64(65534), *podSecurityContext.RunAsUser, "Should run as nobody user")
	require.NotNil(t, podSecurityContext.SeccompProfile, "SeccompProfile is required for PSS restricted")
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, podSecurityContext.SeccompProfile.Type)

	// Verify container security context meets PSS restricted requirements
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext, "Container securityContext is required")
	assert.True(t, *containerSecurityContext.RunAsNonRoot, "RunAsNonRoot must be true")
	assert.False(t, *containerSecurityContext.AllowPrivilegeEscalation, "AllowPrivilegeEscalation must be false")
	assert.True(t, *containerSecurityContext.ReadOnlyRootFilesystem, "ReadOnlyRootFilesystem should be true")

	// Verify capabilities are dropped
	require.NotNil(t, containerSecurityContext.Capabilities, "Capabilities must be configured")
	assert.Contains(t, containerSecurityContext.Capabilities.Drop, corev1.Capability("ALL"), "Must drop ALL capabilities")
	assert.Empty(t, containerSecurityContext.Capabilities.Add, "No capabilities should be added for PSS restricted")

	// Verify seccomp profile at container level
	require.NotNil(t, containerSecurityContext.SeccompProfile, "Container seccompProfile is required")
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, containerSecurityContext.SeccompProfile.Type)
}

// TestSecurityContext_PSS_Baseline tests the Pod Security Standards "baseline"
// profile which prevents known privilege escalations but is more permissive.
func TestSecurityContext_PSS_Baseline(t *testing.T) {
	ctx := context.Background()

	// PSS baseline profile - minimal restrictions
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
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
								Add:  []corev1.Capability{"NET_BIND_SERVICE"}, // Allowed in baseline
							},
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify container security context meets PSS baseline requirements
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.False(t, *containerSecurityContext.AllowPrivilegeEscalation, "AllowPrivilegeEscalation should be false")

	// Baseline allows NET_BIND_SERVICE capability
	require.NotNil(t, containerSecurityContext.Capabilities)
	assert.Contains(t, containerSecurityContext.Capabilities.Add, corev1.Capability("NET_BIND_SERVICE"), "NET_BIND_SERVICE is allowed in baseline")
}

// TestSecurityContext_PSS_RestrictedV2_WithVolumes tests PSS restricted compliance
// when the agent requires writable volumes (since ReadOnlyRootFilesystem is true).
func TestSecurityContext_PSS_RestrictedV2_WithVolumes(t *testing.T) {
	ctx := context.Background()

	// PSS restricted with emptyDir volumes for writable paths
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
							RunAsNonRoot: ptr.To(true),
							RunAsUser:    ptr.To(int64(1000)),
							FSGroup:      ptr.To(int64(1000)),
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
							SeccompProfile: &corev1.SeccompProfile{
								Type: corev1.SeccompProfileTypeRuntimeDefault,
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "tmp",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
							{
								Name: "cache",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "tmp",
								MountPath: "/tmp",
							},
							{
								Name:      "cache",
								MountPath: "/cache",
							},
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil)

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

	// Verify PSS restricted compliance
	containerSecurityContext := podTemplate.Spec.Containers[0].SecurityContext
	require.NotNil(t, containerSecurityContext)
	assert.True(t, *containerSecurityContext.ReadOnlyRootFilesystem, "ReadOnlyRootFilesystem should be true")

	// Verify writable volumes are present
	volumeNames := make(map[string]bool)
	for _, vol := range podTemplate.Spec.Volumes {
		volumeNames[vol.Name] = true
	}
	assert.True(t, volumeNames["tmp"], "tmp volume should be present")
	assert.True(t, volumeNames["cache"], "cache volume should be present")

	// Verify volume mounts
	var container = podTemplate.Spec.Containers[0]
	mountPaths := make(map[string]bool)
	for _, mount := range container.VolumeMounts {
		mountPaths[mount.MountPath] = true
	}
	assert.True(t, mountPaths["/tmp"], "/tmp mount should be present")
	assert.True(t, mountPaths["/cache"], "/cache mount should be present")
}
