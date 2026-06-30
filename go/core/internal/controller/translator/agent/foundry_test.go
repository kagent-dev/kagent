package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTranslateModelFoundryWorkloadIdentity(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeWorkloadIdentity,
					WorkloadIdentity: &v1alpha2.FoundryWorkloadIdentityConfig{
						ClientID: "11111111-1111-1111-1111-111111111111",
						TenantID: "22222222-2222-2222-2222-222222222222",
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	translator := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, runtimeRequirements, secretHashBytes, err := translator.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)
	require.Empty(t, secretHashBytes)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, "gpt-4.1-nano", foundryModel.Model)
	assert.Equal(t, "https://kagentfoundrytest0623.cognitiveservices.azure.com/", foundryModel.Endpoint)
	assert.Equal(t, "gpt-4-1-nano", foundryModel.Deployment)
	assert.Equal(t, "2024-10-21", foundryModel.APIVersion)
	assert.Equal(t, adk.FoundryAuthTypeWorkloadIdentity, foundryModel.Auth.Type)

	assert.Equal(t, "https://kagentfoundrytest0623.cognitiveservices.azure.com/", envVarValue(t, deploymentData.EnvVars, env.FoundryEndpoint.Name()))
	assert.Equal(t, "gpt-4-1-nano", envVarValue(t, deploymentData.EnvVars, env.FoundryDeployment.Name()))
	assert.Equal(t, "2024-10-21", envVarValue(t, deploymentData.EnvVars, env.FoundryAPIVersion.Name()))
	assertNoEnvVar(t, deploymentData.EnvVars, env.OpenAIAPIKey.Name())
	assertNoEnvVar(t, deploymentData.EnvVars, env.AzureOpenAIAPIKey.Name())

	assert.Equal(t, "true", runtimeRequirements.PodLabels[azureWorkloadIdentityUseLabel])
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", runtimeRequirements.ServiceAccountAnnotations[azureWorkloadIdentityClientIDAnnotation])
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", runtimeRequirements.ServiceAccountAnnotations[azureWorkloadIdentityTenantIDAnnotation])
}

func TestTranslateModelFoundryAPIKey(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           "gpt-4.1-nano",
			Provider:        v1alpha2.ModelProviderFoundry,
			APIKeySecret:    "foundry-secret",
			APIKeySecretKey: "api-key",
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeAPIKey,
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	translator := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, runtimeRequirements, _, err := translator.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, adk.FoundryAuthTypeAPIKey, foundryModel.Auth.Type)
	assert.False(t, foundryModel.APIKeyPassthrough)

	apiKeyEnv := envVar(t, deploymentData.EnvVars, env.FoundryAPIKey.Name())
	require.NotNil(t, apiKeyEnv.ValueFrom)
	require.NotNil(t, apiKeyEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "foundry-secret", apiKeyEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "api-key", apiKeyEnv.ValueFrom.SecretKeyRef.Key)
	assert.Empty(t, runtimeRequirements.PodLabels)
	assert.Empty(t, runtimeRequirements.ServiceAccountAnnotations)
}

func TestTranslateModelFoundryAPIKeyPassthrough(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:             "gpt-4.1-nano",
			Provider:          v1alpha2.ModelProviderFoundry,
			APIKeyPassthrough: true,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeAPIKeyPassthrough,
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	translator := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, runtimeRequirements, _, err := translator.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, adk.FoundryAuthTypeAPIKeyPassthrough, foundryModel.Auth.Type)
	assert.True(t, foundryModel.APIKeyPassthrough)
	assertNoEnvVar(t, deploymentData.EnvVars, env.FoundryAPIKey.Name())
	assert.Empty(t, runtimeRequirements.PodLabels)
	assert.Empty(t, runtimeRequirements.ServiceAccountAnnotations)
}

func TestTranslateModelFoundryResolvesConfigMapRefs(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	values := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-values",
			Namespace: "default",
		},
		Data: map[string]string{
			"endpoint": "https://from-configmap.cognitiveservices.azure.com/",
			"clientId": "33333333-3333-3333-3333-333333333333",
			"tenantId": "44444444-4444-4444-4444-444444444444",
		},
	}
	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				EndpointFrom: &v1alpha2.FoundryEndpointSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: values.Name},
						Key:                  "endpoint",
					},
				},
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeWorkloadIdentity,
					WorkloadIdentity: &v1alpha2.FoundryWorkloadIdentityConfig{
						ClientIDFrom: &v1alpha2.FoundryValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: values.Name},
								Key:                  "clientId",
							},
						},
						TenantIDFrom: &v1alpha2.FoundryValueSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: values.Name},
								Key:                  "tenantId",
							},
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(values, modelConfig).Build()
	translator := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, runtimeRequirements, _, err := translator.translateModel(context.Background(), "default", "foundry-model")
	require.NoError(t, err)

	foundryModel, ok := model.(*adk.Foundry)
	require.True(t, ok)
	assert.Equal(t, "https://from-configmap.cognitiveservices.azure.com/", foundryModel.Endpoint)
	assert.Equal(t, "https://from-configmap.cognitiveservices.azure.com/", envVarValue(t, deploymentData.EnvVars, env.FoundryEndpoint.Name()))
	assert.Equal(t, "33333333-3333-3333-3333-333333333333", runtimeRequirements.ServiceAccountAnnotations[azureWorkloadIdentityClientIDAnnotation])
	assert.Equal(t, "44444444-4444-4444-4444-444444444444", runtimeRequirements.ServiceAccountAnnotations[azureWorkloadIdentityTenantIDAnnotation])
}

func TestTranslateModelFoundryWorkloadIdentityRequiresResolvedClientID(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	optional := true
	values := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-values",
			Namespace: "default",
		},
		Data: map[string]string{},
	}
	modelConfig := foundryWorkloadIdentityModelConfig("foundry-model")
	modelConfig.Spec.Foundry.Auth.WorkloadIdentity.ClientID = ""
	modelConfig.Spec.Foundry.Auth.WorkloadIdentity.ClientIDFrom = &v1alpha2.FoundryValueSource{
		ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: values.Name},
			Key:                  "clientId",
			Optional:             &optional,
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(values, modelConfig).Build()
	translator := &adkApiTranslator{kube: kubeClient}

	_, _, _, _, err := translator.translateModel(context.Background(), "default", "foundry-model")
	require.ErrorContains(t, err, "Foundry workload identity clientId is required")
}

func TestTranslateAgentFoundryWorkloadIdentityRuntimeRequirements(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeWorkloadIdentity,
					WorkloadIdentity: &v1alpha2.FoundryWorkloadIdentityConfig{
						ClientID: "11111111-1111-1111-1111-111111111111",
						TenantID: "22222222-2222-2222-2222-222222222222",
					},
				},
			},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Description: "Foundry smoke agent",
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "You are a Foundry smoke test agent",
				ModelConfig:   "foundry-model",
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: "foundry-model"}, nil, "", nil)

	outputs, err := TranslateAgent(context.Background(), translator, agent)
	require.NoError(t, err)
	require.NotNil(t, outputs)

	var deployment *appsv1.Deployment
	var configSecret *corev1.Secret
	var serviceAccount *corev1.ServiceAccount
	for _, obj := range outputs.Manifest {
		switch typedObj := obj.(type) {
		case *appsv1.Deployment:
			deployment = typedObj
		case *corev1.Secret:
			configSecret = typedObj
		case *corev1.ServiceAccount:
			serviceAccount = typedObj
		}
	}

	require.NotNil(t, deployment)
	require.NotNil(t, configSecret)
	require.NotNil(t, serviceAccount)
	assert.Equal(t, "true", deployment.Spec.Template.Labels[azureWorkloadIdentityUseLabel])
	assert.Equal(t, "foundry-agent", deployment.Spec.Template.Spec.ServiceAccountName)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", serviceAccount.Annotations[azureWorkloadIdentityClientIDAnnotation])
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", serviceAccount.Annotations[azureWorkloadIdentityTenantIDAnnotation])

	configJSON := configSecret.StringData["config.json"]
	require.NotEmpty(t, configJSON)
	var agentConfig adk.AgentConfig
	require.NoError(t, json.Unmarshal([]byte(configJSON), &agentConfig))
}

func TestTranslateAgentFoundryRequiresGoRuntime(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-model",
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeWorkloadIdentity,
					WorkloadIdentity: &v1alpha2.FoundryWorkloadIdentityConfig{
						ClientID: "11111111-1111-1111-1111-111111111111",
					},
				},
			},
		},
	}
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are a Foundry smoke test agent",
				ModelConfig:   "foundry-model",
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: "foundry-model"}, nil, "", nil)

	_, err := TranslateAgent(context.Background(), translator, agent)
	require.ErrorContains(t, err, `Foundry model provider requires declarative runtime "go"`)
}

func TestTranslateAgentFoundrySummarizerRequiresGoRuntime(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	mainModel := openAIModelConfig("main-model")
	summarizerModel := foundryWorkloadIdentityModelConfig("foundry-summarizer")
	summarizerName := summarizerModel.Name
	compactionInterval := 5
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openai-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are a test agent",
				ModelConfig:   mainModel.Name,
				Context: &v1alpha2.ContextConfig{
					Compaction: &v1alpha2.ContextCompressionConfig{
						CompactionInterval: &compactionInterval,
						Summarizer: &v1alpha2.ContextSummarizerConfig{
							ModelConfig: &summarizerName,
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mainModel, summarizerModel, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: mainModel.Name}, nil, "", nil)

	_, err := TranslateAgent(context.Background(), translator, agent)
	require.ErrorContains(t, err, `Foundry model provider requires declarative runtime "go"`)
}

func TestTranslateAgentFoundryMemoryRequiresGoRuntime(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	mainModel := openAIModelConfig("main-model")
	memoryModel := foundryWorkloadIdentityModelConfig("foundry-memory")
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openai-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "You are a test agent",
				ModelConfig:   mainModel.Name,
				Memory: &v1alpha2.MemorySpec{
					ModelConfig: memoryModel.Name,
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mainModel, memoryModel, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: mainModel.Name}, nil, "", nil)

	_, err := TranslateAgent(context.Background(), translator, agent)
	require.ErrorContains(t, err, `Foundry model provider requires declarative runtime "go"`)
}

func TestTranslateAgentFoundryRuntimeRequirementConflict(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	mainModel := foundryWorkloadIdentityModelConfig("main-model")
	summarizerModel := foundryWorkloadIdentityModelConfig("foundry-summarizer")
	summarizerModel.Spec.Foundry.Auth.WorkloadIdentity.ClientID = "99999999-9999-9999-9999-999999999999"
	summarizerName := summarizerModel.Name
	compactionInterval := 5
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "You are a test agent",
				ModelConfig:   mainModel.Name,
				Context: &v1alpha2.ContextConfig{
					Compaction: &v1alpha2.ContextCompressionConfig{
						CompactionInterval: &compactionInterval,
						Summarizer: &v1alpha2.ContextSummarizerConfig{
							ModelConfig: &summarizerName,
						},
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mainModel, summarizerModel, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: mainModel.Name}, nil, "", nil)

	_, err := TranslateAgent(context.Background(), translator, agent)
	require.ErrorContains(t, err, `conflicting service account annotation "azure.workload.identity/client-id"`)
}

func TestTranslateAgentFoundryWorkloadIdentityRejectsExternalServiceAccount(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := foundryWorkloadIdentityModelConfig("foundry-model")
	serviceAccountName := "external-foundry-sa"
	agent := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foundry-agent",
			Namespace: "default",
		},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				Runtime:       v1alpha2.DeclarativeRuntime_Go,
				SystemMessage: "You are a Foundry smoke test agent",
				ModelConfig:   modelConfig.Name,
				Deployment: &v1alpha2.DeclarativeDeploymentSpec{
					SharedDeploymentSpec: v1alpha2.SharedDeploymentSpec{
						ServiceAccountName: &serviceAccountName,
					},
				},
			},
		},
	}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig, agent).Build()
	translator := NewAdkApiTranslator(kubeClient, types.NamespacedName{Namespace: "default", Name: modelConfig.Name}, nil, "", nil)

	_, err := TranslateAgent(context.Background(), translator, agent)
	require.ErrorContains(t, err, `model runtime requires ServiceAccount annotations, but serviceAccountName "external-foundry-sa" is external`)
}

func openAIModelConfig(name string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderOpenAI,
			OpenAI:   &v1alpha2.OpenAIConfig{},
		},
	}
}

func foundryWorkloadIdentityModelConfig(name string) *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1-nano",
			Provider: v1alpha2.ModelProviderFoundry,
			Foundry: &v1alpha2.FoundryConfig{
				Endpoint:   "https://kagentfoundrytest0623.cognitiveservices.azure.com/",
				Deployment: "gpt-4-1-nano",
				Auth: v1alpha2.FoundryAuthConfig{
					Type: v1alpha2.FoundryAuthTypeWorkloadIdentity,
					WorkloadIdentity: &v1alpha2.FoundryWorkloadIdentityConfig{
						ClientID: "11111111-1111-1111-1111-111111111111",
					},
				},
			},
		},
	}
}

func envVar(t *testing.T, envVars []corev1.EnvVar, name string) corev1.EnvVar {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name == name {
			return envVar
		}
	}
	t.Fatalf("env var %s not found", name)
	return corev1.EnvVar{}
}

func envVarValue(t *testing.T, envVars []corev1.EnvVar, name string) string {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name == name {
			return envVar.Value
		}
	}
	t.Fatalf("env var %s not found", name)
	return ""
}

func assertNoEnvVar(t *testing.T, envVars []corev1.EnvVar, name string) {
	t.Helper()
	for _, envVar := range envVars {
		if envVar.Name == name {
			t.Fatalf("env var %s unexpectedly present", name)
		}
	}
}
