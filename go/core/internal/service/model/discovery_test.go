package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/model"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type recordingProviderModelRefresher struct {
	models    []string
	err       error
	calls     int
	namespace string
	name      string
}

func (r *recordingProviderModelRefresher) RefreshModelProviderConfigModels(_ context.Context, namespace, name string) ([]string, error) {
	r.calls++
	r.namespace = namespace
	r.name = name
	return r.models, r.err
}

type listErrorClient struct {
	ctrlclient.Client
	err error
}

func (c *listErrorClient) List(context.Context, ctrlclient.ObjectList, ...ctrlclient.ListOption) error {
	return c.err
}

type getErrorClient struct {
	ctrlclient.Client
	err error
}

func (c *getErrorClient) Get(context.Context, ctrlclient.ObjectKey, ctrlclient.Object, ...ctrlclient.GetOption) error {
	return c.err
}

func TestDiscoverySupportedProviderDefinitions(t *testing.T) {
	service := model.NewService(nil, nil, "default")

	modelProviders := service.ListSupportedModelProviders(context.Background())
	require.Len(t, modelProviders, 10)
	assert.Equal(t, []string{
		"OpenAI",
		"Anthropic",
		"AzureOpenAI",
		"Foundry",
		"Ollama",
		"Gemini",
		"GeminiVertexAI",
		"AnthropicVertexAI",
		"Bedrock",
		"SAPAICore",
	}, providerNames(modelProviders))
	assert.Empty(t, modelProviders[0].RequiredParams)
	assert.Equal(t, []string{
		"baseUrl",
		"organization",
		"temperature",
		"maxTokens",
		"maxCompletionTokens",
		"topP",
		"frequencyPenalty",
		"presencePenalty",
		"seed",
		"n",
		"timeout",
		"reasoningEffort",
		"apiFormat",
		"tokenExchange",
	}, modelProviders[0].OptionalParams)
	assert.Equal(t, []string{"azureEndpoint", "apiVersion"}, modelProviders[2].RequiredParams)
	assert.Equal(t, []string{"azureDeployment", "azureAdToken", "temperature", "maxTokens", "topP", "apiFormat"}, modelProviders[2].OptionalParams)
	assert.Equal(t, []string{"deployment", "endpoint"}, modelProviders[3].RequiredParams)
	assert.Equal(t, []string{"apiVersion"}, modelProviders[3].OptionalParams)
	assert.Equal(t, []string{"", "maxOutputTokens", "candidateCount", "responseMimeType"}, modelProviders[6].OptionalParams)

	memoryProviders := service.ListSupportedMemoryProviders(context.Background())
	require.Len(t, memoryProviders, 1)
	assert.Equal(t, "Pinecone", memoryProviders[0].Name)
	assert.Equal(t, "Pinecone", memoryProviders[0].Type)
	assert.Equal(t, []string{"indexHost"}, memoryProviders[0].RequiredParams)
	assert.Equal(t, []string{"topK", "namespace", "recordFields", "scoreThreshold"}, memoryProviders[0].OptionalParams)
}

func TestDiscoveryStaticModelCatalog(t *testing.T) {
	service := model.NewService(nil, nil, "default")
	models := service.ListSupportedModels(context.Background())

	require.Len(t, models, 10)
	require.NotEmpty(t, models[v1alpha3.ModelProviderOpenAI])
	assert.Equal(t, "gpt-5.6-terra", models[v1alpha3.ModelProviderOpenAI][0].Name)
	assert.True(t, models[v1alpha3.ModelProviderOpenAI][0].FunctionCalling)
	assert.Equal(t, model.ModelInfo{Name: "deepseek-r1", FunctionCalling: false}, models[v1alpha3.ModelProviderOllama][5])
	assert.Equal(t, model.ModelInfo{Name: "us.amazon.nova-2-lite-v1:0", FunctionCalling: false}, models[v1alpha3.ModelProviderBedrock][10])

	encoded, err := json.Marshal(models[v1alpha3.ModelProviderOpenAI][0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"gpt-5.6-terra","function_calling":true}`, string(encoded))
}

func TestDiscoveryConfiguredProviders(t *testing.T) {
	scheme := discoveryScheme(t)
	readyCondition := []metav1.Condition{{
		Type:   v1alpha3.ModelProviderConfigConditionTypeReady,
		Status: metav1.ConditionTrue,
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha3.ModelProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "default-endpoint", Namespace: "service-ns"},
			Spec:       v1alpha3.ModelProviderConfigSpec{Type: v1alpha3.ModelProviderOpenAI},
			Status:     v1alpha3.ModelProviderConfigStatus{Conditions: readyCondition},
		},
		&v1alpha3.ModelProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "custom-endpoint", Namespace: "service-ns"},
			Spec: v1alpha3.ModelProviderConfigSpec{
				Type:     v1alpha3.ModelProviderAnthropic,
				Endpoint: "https://models.example.com",
			},
			Status: v1alpha3.ModelProviderConfigStatus{Conditions: readyCondition},
		},
		&v1alpha3.ModelProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready", Namespace: "service-ns"},
			Spec:       v1alpha3.ModelProviderConfigSpec{Type: v1alpha3.ModelProviderAnthropic},
		},
		&v1alpha3.ModelProviderConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "other"},
			Spec:       v1alpha3.ModelProviderConfigSpec{Type: v1alpha3.ModelProviderOpenAI},
			Status:     v1alpha3.ModelProviderConfigStatus{Conditions: readyCondition},
		},
	).Build()
	service := model.NewService(kubeClient, nil, "service-ns")

	providers, err := service.ListConfiguredProviders(context.Background())
	require.NoError(t, err)
	assert.ElementsMatch(t, []model.ConfiguredProvider{
		{
			Name:     "default-endpoint",
			Type:     "OpenAI",
			Endpoint: "https://api.openai.com/v1",
		},
		{
			Name:     "custom-endpoint",
			Type:     "Anthropic",
			Endpoint: "https://models.example.com",
		},
	}, providers)
}

func TestDiscoveryConfiguredProviderListError(t *testing.T) {
	listErr := errors.New("list failed")
	service := model.NewService(&listErrorClient{err: listErr}, nil, "service-ns")

	_, err := service.ListConfiguredProviders(context.Background())
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	assert.ErrorIs(t, err, listErr)
}

func TestDiscoveryProviderModels(t *testing.T) {
	scheme := discoveryScheme(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha3.ModelProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "configured", Namespace: "service-ns"},
		Status: v1alpha3.ModelProviderConfigStatus{
			DiscoveredModels: []string{"model-a", "model-b"},
		},
	}).Build()
	service := model.NewService(kubeClient, nil, "service-ns")

	result, err := service.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "configured"})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderModelsResult{Provider: "configured", Models: []string{"model-a", "model-b"}}, result)
}

func TestDiscoveryProviderModelsRefresh(t *testing.T) {
	refresher := &recordingProviderModelRefresher{models: []string{"fresh-model"}}
	service := model.NewService(nil, nil, "service-ns", model.WithProviderModelRefresher(refresher))

	result, err := service.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "configured", Refresh: true})
	require.NoError(t, err)
	assert.Equal(t, model.ProviderModelsResult{Provider: "configured", Models: []string{"fresh-model"}}, result)
	assert.Equal(t, 1, refresher.calls)
	assert.Equal(t, "service-ns", refresher.namespace)
	assert.Equal(t, "configured", refresher.name)
}

func TestDiscoveryProviderModelsErrors(t *testing.T) {
	scheme := discoveryScheme(t)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha3.ModelProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "service-ns"},
	}).Build()
	service := model.NewService(kubeClient, nil, "service-ns")

	t.Run("empty provider name", func(t *testing.T) {
		_, err := service.GetProviderModels(context.Background(), model.GetProviderModelsRequest{})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	})

	t.Run("missing provider config", func(t *testing.T) {
		_, err := service.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "missing"})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	})

	t.Run("empty discovered models", func(t *testing.T) {
		_, err := service.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "empty"})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
		assert.Equal(t, "No models discovered for model provider, try refreshing", serviceerrors.MessageOf(err))
	})

	t.Run("cached lookup error", func(t *testing.T) {
		getErr := errors.New("get failed")
		getService := model.NewService(&getErrorClient{err: getErr}, nil, "service-ns")

		_, err := getService.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "configured"})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
		assert.ErrorIs(t, err, getErr)
	})

	t.Run("refresh error", func(t *testing.T) {
		refreshErr := errors.New("refresh failed")
		refresher := &recordingProviderModelRefresher{err: refreshErr}
		refreshService := model.NewService(nil, nil, "service-ns", model.WithProviderModelRefresher(refresher))

		_, err := refreshService.GetProviderModels(context.Background(), model.GetProviderModelsRequest{Name: "configured", Refresh: true})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
		assert.ErrorIs(t, err, refreshErr)
	})
}

func discoveryScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	return scheme
}

func providerNames(providers []model.ProviderDefinition) []string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.Name)
	}
	return names
}
