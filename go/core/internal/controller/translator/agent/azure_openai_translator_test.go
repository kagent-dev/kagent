package agent

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestTranslateModelAzureOpenAISettings verifies the translator populates the
// shared Azure data-plane settings (endpoint/deployment/api-version) on the
// adk.AzureOpenAI model, alongside the existing env vars, so both the chat model
// and memory embeddings can build through the shared azureai client.
func TestTranslateModelAzureOpenAISettings(t *testing.T) {
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "azure-model", Namespace: "default"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4o",
			Provider: v1alpha2.ModelProviderAzureOpenAI,
			AzureOpenAI: &v1alpha2.AzureOpenAIConfig{
				Endpoint:       "https://example.openai.azure.com/",
				DeploymentName: "gpt-4o-deploy",
				APIVersion:     "2024-06-01",
			},
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(modelConfig).Build()
	tr := &adkApiTranslator{kube: kubeClient}

	model, deploymentData, _, err := tr.translateModel(context.Background(), "default", "azure-model")
	require.NoError(t, err)

	azureModel, ok := model.(*adk.AzureOpenAI)
	require.True(t, ok)
	assert.Equal(t, "gpt-4o-deploy", azureModel.Model)
	assert.Equal(t, "https://example.openai.azure.com/", azureModel.Endpoint)
	assert.Equal(t, "gpt-4o-deploy", azureModel.Deployment)
	assert.Equal(t, "2024-06-01", azureModel.APIVersion)

	assert.Equal(t, "https://example.openai.azure.com/", envVarValue(t, deploymentData.EnvVars, env.AzureOpenAIEndpoint.Name()))
	assert.Equal(t, "2024-06-01", envVarValue(t, deploymentData.EnvVars, env.OpenAIAPIVersion.Name()))
}

// TestModelToEmbeddingConfigAzureOpenAI verifies the Azure data-plane settings flow into
// the embedding config so azure_openai memory embeddings resolve the same
// data-plane URL as the chat model.
func TestModelToEmbeddingConfigAzureOpenAI(t *testing.T) {
	m := &adk.AzureOpenAI{
		BaseModel:  adk.BaseModel{Model: "gpt-4o-deploy"},
		Endpoint:   "https://example.openai.azure.com/",
		Deployment: "gpt-4o-deploy",
		APIVersion: "2024-06-01",
	}
	e := adk.ModelToEmbeddingConfig(m)
	require.NotNil(t, e)
	assert.Equal(t, adk.ModelTypeAzureOpenAI, e.Provider)
	assert.Equal(t, "gpt-4o-deploy", e.Model)
	assert.Equal(t, "https://example.openai.azure.com/", e.Endpoint)
	assert.Equal(t, "gpt-4o-deploy", e.Deployment)
	assert.Equal(t, "2024-06-01", e.APIVersion)
}
