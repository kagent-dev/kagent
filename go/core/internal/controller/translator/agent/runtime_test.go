package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	translator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func withGoRuntimeDigests(t *testing.T) {
	t.Helper()
	originalBase := translator.GoADKImageDigest
	originalFull := translator.GoADKFullImageDigest
	translator.GoADKImageDigest = "sha256:test-go-base"
	translator.GoADKFullImageDigest = "sha256:test-go-full"
	t.Cleanup(func() {
		translator.GoADKImageDigest = originalBase
		translator.GoADKFullImageDigest = originalFull
	})
}

func withPythonRuntimeDigest(t *testing.T) {
	t.Helper()
	original := translator.PythonADKImageDigest
	translator.PythonADKImageDigest = "sha256:test-app"
	t.Cleanup(func() {
		translator.PythonADKImageDigest = original
	})
}

func TestRuntime_GoRuntime(t *testing.T) {
	withGoRuntimeDigests(t)
	ctx := context.Background()

	// Create agent with Go runtime
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-go-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "Test Go agent",
				ModelConfig:   "test-model",
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	// Translate agent
	result, err := translator.TranslateAgent(ctx, translatorInstance, agent)
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

	// Verify container image uses golang-adk
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "golang-adk", "Image should use golang-adk repository")
	assert.Contains(t, container.Image, "@sha256:test-go-base", "Go runtime should use digest-pinned golang-adk image")

	// Verify Go runtime readiness probe timings (fast startup)
	require.NotNil(t, container.ReadinessProbe)
	assert.Equal(t, int32(1), container.ReadinessProbe.InitialDelaySeconds, "Go runtime should have 1s initial delay")
	assert.Equal(t, int32(5), container.ReadinessProbe.TimeoutSeconds, "Go runtime should have 5s timeout")
	assert.Equal(t, int32(1), container.ReadinessProbe.PeriodSeconds, "Go runtime should have 1s period")
}

func TestRuntime_GoRuntimeWithSkillsUsesFullImageTag(t *testing.T) {
	withGoRuntimeDigests(t)
	ctx := context.Background()

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-go-skills-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "Test Go agent with skills",
				ModelConfig:   "test-model",
			},
			Skills: &v1alpha2.SkillForAgent{
				Refs: []string{"example.com/skill:latest"},
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

	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "golang-adk", "Image should use golang-adk repository")
	assert.Contains(t, container.Image, "@sha256:test-go-full", "Go runtime with skills should use digest-pinned golang-adk-full image")
}

func TestRuntime_PythonRuntime(t *testing.T) {
	withPythonRuntimeDigest(t)
	ctx := context.Background()

	// Create agent with Python runtime (explicit)
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-python-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Python,
				SystemMessage: "Test Python agent",
				ModelConfig:   "test-model",
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	// Translate agent
	result, err := translator.TranslateAgent(ctx, translatorInstance, agent)
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

	// Verify container image uses digest-pinned app (Python ADK)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "/app@", "Image should use app repository")
	assert.Contains(t, container.Image, "@sha256:test-app", "Python runtime should use digest-pinned app image")

	// Verify Python runtime readiness probe timings (slower startup)
	require.NotNil(t, container.ReadinessProbe)
	assert.Equal(t, int32(15), container.ReadinessProbe.InitialDelaySeconds, "Python runtime should have 15s initial delay")
	assert.Equal(t, int32(15), container.ReadinessProbe.TimeoutSeconds, "Python runtime should have 15s timeout")
	assert.Equal(t, int32(15), container.ReadinessProbe.PeriodSeconds, "Python runtime should have 15s period")
}

func TestRuntime_DefaultToPython(t *testing.T) {
	withPythonRuntimeDigest(t)
	ctx := context.Background()

	// Create agent without runtime field (should default to Python)
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-default-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				// Runtime not specified - should default to Python
				SystemMessage: "Test default agent",
				ModelConfig:   "test-model",
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	// Translate agent
	result, err := translator.TranslateAgent(ctx, translatorInstance, agent)
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

	// Verify container image uses digest-pinned app (Python ADK) by default
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "/app@", "Image should default to app repository")
	assert.Contains(t, container.Image, "@sha256:test-app", "Default Python runtime should use digest-pinned app image")

	// Verify Python runtime readiness probe timings
	require.NotNil(t, container.ReadinessProbe)
	assert.Equal(t, int32(15), container.ReadinessProbe.InitialDelaySeconds, "Should default to Python's 15s initial delay")
	assert.Equal(t, int32(15), container.ReadinessProbe.TimeoutSeconds, "Should default to Python's 15s timeout")
	assert.Equal(t, int32(15), container.ReadinessProbe.PeriodSeconds, "Should default to Python's 15s period")
}

func TestRuntime_CustomRepositoryPath(t *testing.T) {
	withGoRuntimeDigests(t)
	ctx := context.Background()

	// Save original DefaultImageConfig.Repository and restore after test
	originalRepo := translator.DefaultImageConfig.Repository
	defer func() {
		translator.DefaultImageConfig.Repository = originalRepo
	}()

	// Set a custom repository path (simulating --image-repository flag)
	translator.DefaultImageConfig.Repository = "my-registry.com/custom/app"

	// Create agent with Go runtime
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-custom-repo-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "Test Go agent with custom repo",
				ModelConfig:   "test-model",
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
	translatorInstance := translator.NewAdkApiTranslator(kubeClient, defaultModel, nil, "", nil)

	// Translate agent
	result, err := translator.TranslateAgent(ctx, translatorInstance, agent)
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

	// Verify container image uses custom repository base with golang-adk
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "my-registry.com/custom/golang-adk", "Image should use custom repository with golang-adk")
	assert.Contains(t, container.Image, "@sha256:test-go-base", "Go runtime should use digest-pinned golang-adk image")
}

func TestRuntime_CustomRepositoryPath_WithSkillsUsesFullTag(t *testing.T) {
	withGoRuntimeDigests(t)
	ctx := context.Background()

	originalRepo := translator.DefaultImageConfig.Repository
	defer func() {
		translator.DefaultImageConfig.Repository = originalRepo
	}()
	translator.DefaultImageConfig.Repository = "my-registry.com/custom/app"

	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-custom-repo-skills-agent",
			Namespace: "test",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "Test Go agent with custom repo and skills",
				ModelConfig:   "test-model",
			},
			Skills: &v1alpha2.SkillForAgent{
				Refs: []string{"example.com/skill:latest"},
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

	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Contains(t, container.Image, "my-registry.com/custom/golang-adk", "Image should use custom repository with golang-adk")
	assert.Contains(t, container.Image, "@sha256:test-go-full", "Go runtime with skills should use digest-pinned golang-adk-full image")
}
