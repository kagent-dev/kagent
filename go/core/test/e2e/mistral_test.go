package e2e_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mistralChatModel = "mistral-large-latest"
)

// setupMistralModelConfig creates a Mistral ModelConfig pointing at the given
// endpoint. Mistral speaks the OpenAI wire protocol, so the mock only needs to
// implement /chat/completions.
func setupMistralModelConfig(t *testing.T, cli client.Client, endpoint, model string) *v1alpha2.ModelConfig {
	t.Helper()
	baseURL := endpoint
	modelCfg := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-mistral-model-config-",
			Namespace:    "kagent",
		},
		Spec: v1alpha2.ModelConfigSpec{
			Model:           model,
			Provider:        v1alpha2.ModelProviderMistral,
			APIKeySecret:    "kagent-mistral",
			APIKeySecretKey: "MISTRAL_API_KEY",
			Mistral: &v1alpha2.MistralConfig{
				BaseURL: &baseURL,
			},
		},
	}
	require.NoError(t, cli.Create(t.Context(), modelCfg))
	cleanup(t, cli, modelCfg)
	return modelCfg
}

// setupMistralMockServer stands up a mock OpenAI-compatible endpoint that
// returns a canned chat-completion response, matching the shape Mistral would
// return. Returns a cluster-reachable URL.
func setupMistralMockServer(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			fmt.Fprint(w, `{"id":"chatcmpl-mistral-1","object":"chat.completion","created":0,"model":"mistral-large-latest","choices":[{"index":0,"message":{"role":"assistant","content":"Bonjour from the mock Mistral endpoint."},"finish_reason":"stop"}]}`)
		default:
			http.Error(w, fmt.Sprintf("unexpected Mistral mock path %s", r.URL.Path), http.StatusNotFound)
		}
	}))
	server.Listener = listener
	server.Start()

	clusterURL := buildK8sURL(server.URL)
	return clusterURL, server.Close
}

// TestE2EInvokeWithMistralAgent verifies a Mistral-backed agent can be
// deployed and returns a mock chat completion. The test is skipped when the
// e2e environment or a fake MISTRAL_API_KEY secret is not available: it never
// contacts the real Mistral API.
func TestE2EInvokeWithMistralAgent(t *testing.T) {
	// Skip when the caller has not opted into Mistral e2e coverage. The check
	// keeps CI green in environments without a mistral-api-key secret while
	// still allowing local runs to exercise the full agent pipeline.
	if os.Getenv("MISTRAL_API_KEY") == "" {
		t.Skip("Skipping Mistral e2e: MISTRAL_API_KEY not set (used only as a signal to opt in; the test uses a mock server)")
	}

	endpoint, stopServer := setupMistralMockServer(t)
	defer stopServer()

	cli := setupK8sClient(t, false)
	chatModelCfg := setupMistralModelConfig(t, cli, endpoint, mistralChatModel)

	goRuntime := v1alpha2.DeclarativeRuntime_Go
	agent := setupAgentWithOptions(t, cli, chatModelCfg.Name, nil, AgentOptions{
		Name:    "mistral-go-adk-test",
		Runtime: &goRuntime,
	})
	a2aClient := setupA2AClient(t, agent)

	runSyncTest(t, a2aClient,
		"Say hello in French",
		"Bonjour",
	)
}
