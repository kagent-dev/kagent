package modeldiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	adkmodels "github.com/kagent-dev/kagent/go/adk/pkg/models"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	api "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/controller/apiclient"
	"github.com/kagent-dev/kagent/go/core/internal/grpcserver"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	systemservice "github.com/kagent-dev/kagent/go/core/internal/service/system"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Exercise schema admission, real watches/status persistence, replica caches and
// generated ModelService clients together. No existing cluster is modified.
func TestDiscoveryKubernetesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Kubernetes integration")
	}
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		matches, err := filepath.Glob("../../../../bin/k8s/*")
		require.NoError(t, err)
		require.NotEmpty(t, matches, "run make setup-envtest")
		assets = matches[len(matches)-1]
	}
	environment := &envtest.Environment{BinaryAssetsDirectory: assets, CRDDirectoryPaths: []string{"../../../../../helm/kagent-crds/templates"}, ErrorIfCRDPathMissing: true}
	cfg, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, api.AddToScheme(scheme))
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	require.NoError(t, kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team"}}))
	t.Setenv("ANTHROPIC_API_KEY", "invocation")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/team/v1/models" {
			require.Empty(t, r.Header.Get("Authorization"))
			_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"gateway-model","object":"model","owned_by":"gcp.vertex_ai","capabilities":{"tools":true}}]}`)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "gateway-model", body.Model)
		switch r.URL.Path {
		case "/team/v1/chat/completions":
			require.Equal(t, "Bearer invocation", r.Header.Get("Authorization"))
			_, _ = fmt.Fprint(w, `{"id":"chat-test","object":"chat.completion","model":"gateway-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		case "/team/v1/responses":
			require.Equal(t, "Bearer invocation", r.Header.Get("Authorization"))
			_, _ = fmt.Fprint(w, `{"id":"resp-test","object":"response","model":"gateway-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
		case "/team/v1/messages":
			require.Equal(t, "invocation", r.Header.Get("x-api-key"))
			_, _ = fmt.Fprint(w, `{"id":"msg-test","type":"message","role":"assistant","model":"gateway-model","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		default:
			t.Errorf("unexpected invocation path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	source := &api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "gateway"}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: upstream.URL + "/team/v1"}}
	require.NoError(t, kube.Create(ctx, source), "OpenAI discovery must admit an absent Secret")
	invalid := source.DeepCopy()
	invalid.Name = "invalid"
	invalid.ResourceVersion = ""
	invalid.UID = ""
	invalid.Spec.Endpoint = "https://host/v1?limit=1"
	require.Error(t, kube.Create(ctx, invalid))

	startReplica := func(name string) *Controller {
		kc, err := apiclient.New(cfg)
		require.NoError(t, err)
		opts := krt.NewOptionsBuilder(ctx.Done(), name, nil)
		providers := krt.NewInformer[*api.ModelProviderConfig](kc, opts.WithName("Providers")...)
		secrets := krt.NewInformer[*corev1.Secret](kc, opts.WithName("Secrets")...)
		c := New(providers, secrets, opts)
		done := make(chan error, 1)
		go func() { done <- c.Start(ctx) }()
		require.True(t, kc.RunAndWait(ctx.Done()))
		t.Cleanup(func() { cancel(); require.NoError(t, <-done); kc.Shutdown() })
		return c
	}
	leader := startReplica("leader")
	follower := startReplica("follower")
	waitState(t, leader, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	waitState(t, follower, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	writer, err := NewStatusWriter(leader, cfg)
	require.NoError(t, err)
	require.True(t, writer.NeedLeaderElection())
	require.False(t, leader.NeedLeaderElection())
	statusDone := make(chan error, 1)
	go func() { statusDone <- writer.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-statusDone) })
	require.Eventually(t, func() bool {
		obj := &api.ModelProviderConfig{}
		return kube.Get(ctx, testRef, obj) == nil && obj.Status.ModelCount == 1
	}, 5*time.Second, 10*time.Millisecond)
	observed := &api.ModelProviderConfig{}
	require.NoError(t, kube.Get(ctx, testRef, observed))
	require.Equal(t, 1, observed.Status.ModelCount)
	require.NotNil(t, observed.Status.LastDiscoveryTime)

	service := modelservice.NewService(kube, auth.NoopAuthorizer{}, "team", modelservice.WithProviderDiscovery(follower))
	listener := bufconn.Listen(1 << 20)
	server, err := grpcserver.New(grpcserver.Config{Listener: listener, Authenticator: &authimpl.InsecureAuthenticator{}, SystemService: systemservice.NewService(kube, []string{"team"}, auth.NoopAuthorizer{}, nil), ModelService: service})
	require.NoError(t, err)
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-serverDone) })
	connection, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	rpc := apiv1.NewModelServiceClient(connection)
	rpcCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-user-id", "operator"))
	response, err := rpc.ListProviderModels(rpcCtx, &apiv1.ListProviderModelsRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "gateway"}})
	require.NoError(t, err)
	require.Equal(t, "gateway-model", response.DiscoveredModels[0].Id)
	require.True(t, *response.DiscoveredModels[0].Capabilities.Tools)
	for _, format := range []api.OpenAIAPIFormat{api.OpenAIAPIFormatChatCompletions, api.OpenAIAPIFormatResponses, "messages"} {
		name := fmt.Sprintf("selected-%s", format)
		if format == api.OpenAIAPIFormatChatCompletions {
			name = "selected-chat"
		}
		config := &api.ModelConfig{Spec: api.ModelConfigSpec{Provider: api.ModelProviderOpenAI, Model: response.DiscoveredModels[0].Id, OpenAI: &api.OpenAIConfig{BaseURL: response.Endpoint, APIFormat: &format}}}
		if format == "messages" {
			config.Spec.Provider = api.ModelProviderAnthropic
			config.Spec.OpenAI = nil
			config.Spec.Anthropic = &api.AnthropicConfig{BaseURL: strings.TrimSuffix(response.Endpoint, "/v1")}
		}
		resource, err := structuredobject.FromGo(config, api.GroupVersion.String(), "ModelConfig", 4<<20)
		require.NoError(t, err)
		created, err := rpc.CreateModelConfig(rpcCtx, &apiv1.CreateModelConfigRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: name}, Resource: resource})
		require.NoError(t, err)
		saved := &api.ModelConfig{}
		require.NoError(t, structuredobject.ToGo(created.ModelConfig.Resource, "ModelConfig", saved, 4<<20))
		var llm adkmodel.LLM
		if format == "messages" {
			llm, err = adkmodels.NewAnthropicModel(ctx, &adkmodels.AnthropicConfig{Model: saved.Spec.Model, BaseUrl: saved.Spec.Anthropic.BaseURL})
		} else {
			require.Equal(t, response.Endpoint, saved.Spec.OpenAI.BaseURL)
			require.Equal(t, format, *saved.Spec.OpenAI.APIFormat)
			llm, err = adkmodels.NewOpenAIModel(ctx, &adkmodels.OpenAIConfig{Model: saved.Spec.Model, BaseUrl: saved.Spec.OpenAI.BaseURL, APIFormat: string(*saved.Spec.OpenAI.APIFormat), APIKey: "invocation"})
		}
		require.NoError(t, err)
		var answer string
		for result, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}}, Config: &genai.GenerateContentConfig{}}, false) {
			require.NoError(t, err)
			require.NotNil(t, result.Content)
			for _, part := range result.Content.Parts {
				answer += part.Text
			}
		}
		require.Equal(t, "hello", answer)
	}
	// Watch invalidation and refresh failure must never reuse the previous models.
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer failed.Close()
	require.NoError(t, kube.Get(ctx, testRef, source))
	source.Spec.Endpoint = failed.URL
	require.NoError(t, kube.Update(ctx, source))
	waitState(t, leader, apiv1.DiscoveryState_DISCOVERY_STATE_FAILED)
	waitState(t, follower, apiv1.DiscoveryState_DISCOVERY_STATE_FAILED)
	response, err = rpc.ListProviderModels(rpcCtx, &apiv1.ListProviderModelsRequest{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "gateway"}})
	require.NoError(t, err)
	require.Empty(t, response.DiscoveredModels)
	require.NoError(t, kube.Delete(ctx, source))
	require.Eventually(t, func() bool { return leader.Get(types.NamespacedName{Namespace: "team", Name: "gateway"}) == nil }, 5*time.Second, 10*time.Millisecond)
	previousUID := source.UID
	recreated := &api.ModelProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "gateway"}, Spec: api.ModelProviderConfigSpec{Type: api.ModelProviderOpenAI, Endpoint: upstream.URL + "/team/v1"}}
	require.NoError(t, kube.Create(ctx, recreated))
	waitState(t, leader, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	require.NotEqual(t, string(previousUID), leader.Get(testRef).Uid)
	restarted := startReplica("restarted")
	waitState(t, restarted, apiv1.DiscoveryState_DISCOVERY_STATE_CURRENT)
	require.Equal(t, "gateway-model", restarted.Get(testRef).Models[0].Id)
}
