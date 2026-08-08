package client

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type recordingModelService struct {
	apiv1alpha1.UnimplementedModelServiceServer

	mu                    sync.Mutex
	observations          []callObservation
	createRequest         *apiv1alpha1.CreateModelConfigRequest
	updateRequests        []*apiv1alpha1.UpdateModelConfigRequest
	deleteRequest         *apiv1alpha1.DeleteModelConfigRequest
	providerModelsRequest *apiv1alpha1.ListProviderModelsRequest
	modelConfig           *apiv1alpha1.ModelConfig
}

type callObservation struct {
	userID      string
	hasDeadline bool
}

func (s *recordingModelService) observe(ctx context.Context) {
	metadataValues, _ := metadata.FromIncomingContext(ctx)
	_, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, callObservation{
		userID:      first(metadataValues.Get("x-user-id")),
		hasDeadline: hasDeadline,
	})
}

func (s *recordingModelService) ListModelConfigs(ctx context.Context, _ *apiv1alpha1.ListModelConfigsRequest) (*apiv1alpha1.ListModelConfigsResponse, error) {
	s.observe(ctx)
	return &apiv1alpha1.ListModelConfigsResponse{ModelConfigs: []*apiv1alpha1.ModelConfig{s.modelConfig}}, nil
}

func (s *recordingModelService) GetModelConfig(ctx context.Context, request *apiv1alpha1.GetModelConfigRequest) (*apiv1alpha1.GetModelConfigResponse, error) {
	s.observe(ctx)
	if request.GetRef().GetName() == "missing" {
		return nil, status.Error(codes.NotFound, "ModelConfig not found")
	}
	return &apiv1alpha1.GetModelConfigResponse{ModelConfig: s.modelConfig}, nil
}

func (s *recordingModelService) CreateModelConfig(ctx context.Context, request *apiv1alpha1.CreateModelConfigRequest) (*apiv1alpha1.CreateModelConfigResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	s.createRequest = request
	s.mu.Unlock()
	return &apiv1alpha1.CreateModelConfigResponse{ModelConfig: s.modelConfig}, nil
}

func (s *recordingModelService) UpdateModelConfig(ctx context.Context, request *apiv1alpha1.UpdateModelConfigRequest) (*apiv1alpha1.UpdateModelConfigResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	s.updateRequests = append(s.updateRequests, request)
	s.mu.Unlock()
	return &apiv1alpha1.UpdateModelConfigResponse{ModelConfig: s.modelConfig}, nil
}

func (s *recordingModelService) DeleteModelConfig(ctx context.Context, request *apiv1alpha1.DeleteModelConfigRequest) (*apiv1alpha1.DeleteModelConfigResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	s.deleteRequest = request
	s.mu.Unlock()
	return &apiv1alpha1.DeleteModelConfigResponse{}, nil
}

func (s *recordingModelService) ListSupportedModelProviders(ctx context.Context, _ *apiv1alpha1.ListSupportedModelProvidersRequest) (*apiv1alpha1.ListSupportedModelProvidersResponse, error) {
	s.observe(ctx)
	return &apiv1alpha1.ListSupportedModelProvidersResponse{Providers: []*apiv1alpha1.ProviderDefinition{{
		Name:           "OpenAI",
		Type:           "model",
		RequiredParams: []string{"apiKey"},
		OptionalParams: []string{"baseUrl"},
	}}}, nil
}

func (s *recordingModelService) ListSupportedMemoryProviders(ctx context.Context, _ *apiv1alpha1.ListSupportedMemoryProvidersRequest) (*apiv1alpha1.ListSupportedMemoryProvidersResponse, error) {
	s.observe(ctx)
	return &apiv1alpha1.ListSupportedMemoryProvidersResponse{Providers: []*apiv1alpha1.ProviderDefinition{{
		Name:           "Pinecone",
		Type:           "memory",
		RequiredParams: []string{"apiKey"},
	}}}, nil
}

func (s *recordingModelService) ListConfiguredProviders(ctx context.Context, _ *apiv1alpha1.ListConfiguredProvidersRequest) (*apiv1alpha1.ListConfiguredProvidersResponse, error) {
	s.observe(ctx)
	return &apiv1alpha1.ListConfiguredProvidersResponse{Providers: []*apiv1alpha1.ConfiguredProvider{{
		Name:     "configured-openai",
		Type:     "OpenAI",
		Endpoint: "https://api.openai.test/v1",
	}}}, nil
}

func (s *recordingModelService) ListProviderModels(ctx context.Context, request *apiv1alpha1.ListProviderModelsRequest) (*apiv1alpha1.ListProviderModelsResponse, error) {
	s.observe(ctx)
	s.mu.Lock()
	s.providerModelsRequest = request
	s.mu.Unlock()
	return &apiv1alpha1.ListProviderModelsResponse{
		Provider: request.GetProviderName(),
		Models:   []string{"model-a", "model-b"},
	}, nil
}

func (s *recordingModelService) ListSupportedModels(ctx context.Context, _ *apiv1alpha1.ListSupportedModelsRequest) (*apiv1alpha1.ListSupportedModelsResponse, error) {
	s.observe(ctx)
	return &apiv1alpha1.ListSupportedModelsResponse{Providers: []*apiv1alpha1.ProviderModels{{
		Provider: "OpenAI",
		Models: []*apiv1alpha1.ModelInfo{{
			Name:            "gpt-test",
			FunctionCalling: true,
		}},
	}}}, nil
}

func TestModelClientsUseGeneratedGRPC(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	service := &recordingModelService{modelConfig: testModelConfigMessage(t)}
	server := grpc.NewServer()
	apiv1alpha1.RegisterModelServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	var dialCount atomic.Int32
	clientSet := New(
		"http://rest-must-not-be-used.invalid",
		WithUserID("test-user"),
		WithGRPCTarget("passthrough:///bufnet"),
		WithGRPCTimeout(5*time.Second),
		WithGRPCDialOptions(grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialCount.Add(1)
			return listener.Dial()
		})),
	)
	t.Cleanup(func() { require.NoError(t, clientSet.Close()) })

	listed, err := clientSet.ModelConfig.ListModelConfigs(t.Context())
	require.NoError(t, err)
	require.Len(t, listed.Data, 1)
	assert.Equal(t, "Successfully listed ModelConfigs", listed.Message)
	assertModelConfigResource(t, &listed.Data[0])

	got, err := clientSet.ModelConfig.GetModelConfig(t.Context(), "default", "test-config")
	require.NoError(t, err)
	require.NotNil(t, got.Data)
	assert.Equal(t, "Successfully retrieved ModelConfig", got.Message)
	assertModelConfigResource(t, got.Data)

	created, err := clientSet.ModelConfig.CreateModelConfig(t.Context(), &api.CreateModelConfigRequest{
		Ref:    "created-config",
		APIKey: "create-key",
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-created",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
		Secrets: []api.SecretMaterial{{Name: "companion", Key: "token", Value: "secret-value"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Successfully created ModelConfig", created.Message)

	emptyAPIKey := ""
	updated, err := clientSet.ModelConfig.UpdateModelConfig(t.Context(), "default", "test-config", &api.UpdateModelConfigRequest{
		APIKey: &emptyAPIKey,
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-updated",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
		Secrets: []api.SecretMaterial{{Name: "updated", Key: "token", Value: "updated-value"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Successfully updated ModelConfig", updated.Message)
	_, err = clientSet.ModelConfig.UpdateModelConfig(t.Context(), "default", "test-config", &api.UpdateModelConfigRequest{
		Spec: v1alpha2.ModelConfigSpec{Model: "gpt-without-key", Provider: v1alpha2.ModelProviderOpenAI},
	})
	require.NoError(t, err)

	require.NoError(t, clientSet.ModelConfig.DeleteModelConfig(t.Context(), "default", "test-config"))

	modelProviders, err := clientSet.ModelProviderConfig.ListSupportedModelProviders(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []api.ProviderInfo{{
		Name:           "OpenAI",
		Type:           "model",
		RequiredParams: []string{"apiKey"},
		OptionalParams: []string{"baseUrl"},
	}}, modelProviders.Data)

	memoryProviders, err := clientSet.ModelProviderConfig.ListSupportedMemoryProviders(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Pinecone", memoryProviders.Data[0].Name)

	configuredProviders, err := clientSet.ModelProviderConfig.ListConfiguredProviders(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ConfiguredProvider{{
		Name:     "configured-openai",
		Type:     "OpenAI",
		Endpoint: "https://api.openai.test/v1",
	}}, configuredProviders.Data)

	providerModels, err := clientSet.ModelProviderConfig.ListProviderModels(t.Context(), "configured-openai", true)
	require.NoError(t, err)
	assert.Equal(t, ProviderModelsResult{
		Provider: "configured-openai",
		Models:   []string{"model-a", "model-b"},
	}, providerModels.Data)

	supportedModels, err := clientSet.Model.ListSupportedModels(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ProviderModels{
		v1alpha2.ModelProviderOpenAI: {{Name: "gpt-test", FunctionCalling: true}},
	}, supportedModels.Data)

	_, err = clientSet.ModelConfig.GetModelConfig(t.Context(), "default", "missing")
	assert.Equal(t, codes.NotFound, status.Code(err))

	service.mu.Lock()
	defer service.mu.Unlock()
	require.NotNil(t, service.createRequest)
	assert.True(t, proto.Equal(&apiv1alpha1.ResourceReference{Name: "created-config"}, service.createRequest.GetRef()))
	assert.Equal(t, "create-key", service.createRequest.GetApiKey())
	require.Len(t, service.createRequest.GetSecrets(), 1)
	assert.True(t, proto.Equal(
		&apiv1alpha1.SecretMaterial{Name: "companion", Key: "token", Value: "secret-value"},
		service.createRequest.GetSecrets()[0],
	))
	assertRequestModelConfig(t, service.createRequest.GetResource(), "gpt-created")

	require.Len(t, service.updateRequests, 2)
	assert.True(t, proto.Equal(
		&apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
		service.updateRequests[0].GetRef(),
	))
	require.NotNil(t, service.updateRequests[0].ApiKey)
	assert.Empty(t, service.updateRequests[0].GetApiKey())
	assert.Nil(t, service.updateRequests[1].ApiKey)
	require.Len(t, service.updateRequests[0].GetSecrets(), 1)
	assert.True(t, proto.Equal(
		&apiv1alpha1.SecretMaterial{Name: "updated", Key: "token", Value: "updated-value"},
		service.updateRequests[0].GetSecrets()[0],
	))
	assertRequestModelConfig(t, service.updateRequests[0].GetResource(), "gpt-updated")

	assert.True(t, proto.Equal(&apiv1alpha1.DeleteModelConfigRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
	}, service.deleteRequest))
	assert.True(t, proto.Equal(&apiv1alpha1.ListProviderModelsRequest{
		ProviderName: "configured-openai",
		Refresh:      true,
	}, service.providerModelsRequest))
	require.Len(t, service.observations, 12)
	for _, observation := range service.observations {
		assert.Equal(t, "test-user", observation.userID)
		assert.True(t, observation.hasDeadline)
	}
	assert.Equal(t, int32(1), dialCount.Load())

	require.NoError(t, clientSet.Close())
	assert.Nil(t, clientSet.baseClient.grpc.conn)
}

func TestModelClientValidatesRequestsBeforeCallingServer(t *testing.T) {
	clientSet := New("http://unused.invalid", WithGRPCTarget(""))
	t.Cleanup(func() { _ = clientSet.Close() })

	_, err := clientSet.ModelConfig.CreateModelConfig(t.Context(), nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = clientSet.ModelConfig.CreateModelConfig(t.Context(), &api.CreateModelConfigRequest{Ref: "namespace/name/extra"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = clientSet.ModelConfig.UpdateModelConfig(t.Context(), "default", "name", nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func testModelConfigMessage(t *testing.T) *apiv1alpha1.ModelConfig {
	t.Helper()
	resource, err := structuredobject.FromGo(&v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:          "gpt-test",
			Provider:       v1alpha2.ModelProviderOpenAI,
			DefaultHeaders: map[string]string{"x-test": "value"},
		},
		Status: v1alpha2.ModelConfigStatus{
			ObservedGeneration: 3,
			SecretHash:         "secret-hash",
		},
	}, v1alpha2.GroupVersion.String(), modelConfigKind, defaultGRPCMaxMessageSize)
	require.NoError(t, err)
	return &apiv1alpha1.ModelConfig{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
		Resource: resource,
	}
}

func assertModelConfigResource(t *testing.T, resource *api.ModelConfigResource) {
	t.Helper()
	require.NotNil(t, resource)
	assert.Equal(t, "default/test-config", resource.Ref)
	assert.Equal(t, "gpt-test", resource.Spec.Model)
	assert.Equal(t, map[string]string{"x-test": "value"}, resource.Spec.DefaultHeaders)
	assert.Equal(t, int64(3), resource.Status.ObservedGeneration)
	assert.Equal(t, "secret-hash", resource.Status.SecretHash)
}

func assertRequestModelConfig(t *testing.T, resource *apiv1alpha1.StructuredObject, model string) {
	t.Helper()
	decoded := &v1alpha2.ModelConfig{}
	require.NoError(t, structuredobject.ToGo(resource, modelConfigKind, decoded, defaultGRPCMaxMessageSize))
	assert.Equal(t, model, decoded.Spec.Model)
	assert.Empty(t, decoded.Name)
	assert.Empty(t, decoded.Namespace)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
