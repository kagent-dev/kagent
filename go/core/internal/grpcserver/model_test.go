package grpcserver

import (
	"context"
	"net"
	"testing"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingModelRefresher struct {
	namespace string
	name      string
}

func (r *recordingModelRefresher) RefreshModelProviderConfigModels(_ context.Context, namespace, name string) ([]string, error) {
	r.namespace = namespace
	r.name = name
	return []string{"fresh-model"}, nil
}

func TestModelServiceCRUD(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("v1alpha2.AddToScheme() error = %v", err)
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha2.ModelProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-config", Namespace: "default"},
		Spec:       v1alpha2.ModelProviderConfigSpec{Type: v1alpha2.ModelProviderOpenAI},
		Status: v1alpha2.ModelProviderConfigStatus{
			Conditions: []metav1.Condition{{
				Type:   v1alpha2.ModelProviderConfigConditionTypeReady,
				Status: metav1.ConditionTrue,
			}},
			DiscoveredModels: []string{"cached-model"},
		},
	}).Build()
	refresher := &recordingModelRefresher{}
	service := modelservice.NewService(
		kubeClient,
		&authimpl.NoopAuthorizer{},
		"default",
		modelservice.WithProviderModelRefresher(refresher),
	)

	listener := bufconn.Listen(1024 * 1024)
	server, err := New(Config{
		Listener:      listener,
		Registerer:    prometheus.NewRegistry(),
		Authenticator: &authimpl.UnsecureAuthenticator{},
		ModelService:  service,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	serverContext, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Start(serverContext) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := apiv1alpha1.NewModelServiceClient(connection)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "test-user"))

	createResource := modelConfigResource(t, &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "ignored-name", Namespace: "ignored-namespace"},
		Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4",
			Provider: v1alpha2.ModelProviderOpenAI,
		},
	})
	created, err := client.CreateModelConfig(ctx, &apiv1alpha1.CreateModelConfigRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
		Resource: createResource,
	})
	if err != nil {
		t.Fatalf("CreateModelConfig() error = %v", err)
	}
	assertModelConfigResponse(t, created.GetModelConfig(), "default", "test-config", "gpt-4")

	_, err = client.CreateModelConfig(ctx, &apiv1alpha1.CreateModelConfigRequest{
		Ref:      &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
		Resource: createResource,
	})
	assertGRPCCode(t, err, codes.AlreadyExists)

	got, err := client.GetModelConfig(ctx, &apiv1alpha1.GetModelConfigRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
	})
	if err != nil {
		t.Fatalf("GetModelConfig() error = %v", err)
	}
	assertModelConfigResponse(t, got.GetModelConfig(), "default", "test-config", "gpt-4")

	updated, err := client.UpdateModelConfig(ctx, &apiv1alpha1.UpdateModelConfigRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
		Resource: modelConfigResource(t, &v1alpha2.ModelConfig{Spec: v1alpha2.ModelConfigSpec{
			Model:    "gpt-4.1",
			Provider: v1alpha2.ModelProviderOpenAI,
		}}),
	})
	if err != nil {
		t.Fatalf("UpdateModelConfig() error = %v", err)
	}
	assertModelConfigResponse(t, updated.GetModelConfig(), "default", "test-config", "gpt-4.1")

	listed, err := client.ListModelConfigs(ctx, &apiv1alpha1.ListModelConfigsRequest{})
	if err != nil {
		t.Fatalf("ListModelConfigs() error = %v", err)
	}
	if len(listed.GetModelConfigs()) != 1 {
		t.Fatalf("ListModelConfigs() count = %d, want 1", len(listed.GetModelConfigs()))
	}

	_, err = client.DeleteModelConfig(ctx, &apiv1alpha1.DeleteModelConfigRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
	})
	if err != nil {
		t.Fatalf("DeleteModelConfig() error = %v", err)
	}

	_, err = client.GetModelConfig(ctx, &apiv1alpha1.GetModelConfigRequest{
		Ref: &apiv1alpha1.ResourceReference{Namespace: "default", Name: "test-config"},
	})
	assertGRPCCode(t, err, codes.NotFound)

	_, err = client.GetModelConfig(ctx, &apiv1alpha1.GetModelConfigRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)

	invalidResource := modelConfigResource(t, &v1alpha2.ModelConfig{})
	invalidResource.Kind = "Agent"
	_, err = client.CreateModelConfig(ctx, &apiv1alpha1.CreateModelConfigRequest{
		Ref:      &apiv1alpha1.ResourceReference{Name: "invalid"},
		Resource: invalidResource,
	})
	assertGRPCCode(t, err, codes.InvalidArgument)

	modelProviders, err := client.ListSupportedModelProviders(ctx, &apiv1alpha1.ListSupportedModelProvidersRequest{})
	if err != nil {
		t.Fatalf("ListSupportedModelProviders() error = %v", err)
	}
	if len(modelProviders.GetProviders()) != 9 || modelProviders.GetProviders()[0].GetName() != "OpenAI" {
		t.Fatalf("ListSupportedModelProviders() = %+v", modelProviders.GetProviders())
	}

	memoryProviders, err := client.ListSupportedMemoryProviders(ctx, &apiv1alpha1.ListSupportedMemoryProvidersRequest{})
	if err != nil {
		t.Fatalf("ListSupportedMemoryProviders() error = %v", err)
	}
	if len(memoryProviders.GetProviders()) != 1 || memoryProviders.GetProviders()[0].GetName() != "Pinecone" {
		t.Fatalf("ListSupportedMemoryProviders() = %+v", memoryProviders.GetProviders())
	}

	configuredProviders, err := client.ListConfiguredProviders(ctx, &apiv1alpha1.ListConfiguredProvidersRequest{})
	if err != nil {
		t.Fatalf("ListConfiguredProviders() error = %v", err)
	}
	if len(configuredProviders.GetProviders()) != 1 || configuredProviders.GetProviders()[0].GetEndpoint() != "https://api.openai.com/v1" {
		t.Fatalf("ListConfiguredProviders() = %+v", configuredProviders.GetProviders())
	}

	cachedModels, err := client.ListProviderModels(ctx, &apiv1alpha1.ListProviderModelsRequest{ProviderName: "provider-config"})
	if err != nil {
		t.Fatalf("ListProviderModels(cached) error = %v", err)
	}
	if cachedModels.GetProvider() != "provider-config" || len(cachedModels.GetModels()) != 1 || cachedModels.GetModels()[0] != "cached-model" {
		t.Fatalf("ListProviderModels(cached) = %+v", cachedModels)
	}

	refreshedModels, err := client.ListProviderModels(ctx, &apiv1alpha1.ListProviderModelsRequest{ProviderName: "provider-config", Refresh: true})
	if err != nil {
		t.Fatalf("ListProviderModels(refresh) error = %v", err)
	}
	if len(refreshedModels.GetModels()) != 1 || refreshedModels.GetModels()[0] != "fresh-model" || refresher.namespace != "default" || refresher.name != "provider-config" {
		t.Fatalf("ListProviderModels(refresh) = %+v, refresher = %+v", refreshedModels, refresher)
	}

	_, err = client.ListProviderModels(ctx, &apiv1alpha1.ListProviderModelsRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)

	supportedModels, err := client.ListSupportedModels(ctx, &apiv1alpha1.ListSupportedModelsRequest{})
	if err != nil {
		t.Fatalf("ListSupportedModels() error = %v", err)
	}
	if len(supportedModels.GetProviders()) != 9 || supportedModels.GetProviders()[0].GetProvider() != "OpenAI" || supportedModels.GetProviders()[0].GetModels()[0].GetName() != "gpt-5.6-terra" {
		t.Fatalf("ListSupportedModels() = %+v", supportedModels.GetProviders())
	}
}

func modelConfigResource(t *testing.T, modelConfig *v1alpha2.ModelConfig) *apiv1alpha1.StructuredObject {
	t.Helper()
	resource, err := structuredobject.FromGo(
		modelConfig,
		v1alpha2.GroupVersion.String(),
		modelConfigKind,
		DefaultMaxMessageSize,
	)
	if err != nil {
		t.Fatalf("structuredobject.FromGo() error = %v", err)
	}
	return resource
}

func assertModelConfigResponse(t *testing.T, response *apiv1alpha1.ModelConfig, namespace, name, model string) {
	t.Helper()
	if response == nil || response.GetRef() == nil {
		t.Fatal("ModelConfig response or ref is nil")
	}
	if response.GetRef().GetNamespace() != namespace || response.GetRef().GetName() != name {
		t.Fatalf("ModelConfig ref = %s/%s, want %s/%s", response.GetRef().GetNamespace(), response.GetRef().GetName(), namespace, name)
	}
	decoded := &v1alpha2.ModelConfig{}
	if err := structuredobject.ToGo(response.GetResource(), modelConfigKind, decoded, DefaultMaxMessageSize); err != nil {
		t.Fatalf("structuredobject.ToGo() error = %v", err)
	}
	if decoded.Namespace != namespace || decoded.Name != name || decoded.Spec.Model != model {
		t.Fatalf("decoded ModelConfig = %s/%s model %q, want %s/%s model %q", decoded.Namespace, decoded.Name, decoded.Spec.Model, namespace, name, model)
	}
}

func assertGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("gRPC code = %v, want %v (error: %v)", got, want, err)
	}
}
