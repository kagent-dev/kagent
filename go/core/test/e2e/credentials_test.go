package e2e_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestModelCredentialDelivery(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		const token = "gateway-injection-e2e-token"
		header, wantCredential := "Authorization", "Bearer "+token
		if harness.name == claudeE2EHarness {
			header, wantCredential = "X-Api-Key", token
		}
		target := interactionTarget(t)
		kube := interactionKubeClient(t)
		// Each compiler must deliver the configured credential to its model origin.
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-auth-", Namespace: "kagent"}, StringData: map[string]string{"token": token}}
		require.NoError(t, kube.Create(t.Context(), secret))
		t.Cleanup(func() { require.NoError(t, kube.Delete(context.Background(), secret)) })
		origin := startCredentialMock(t, header, wantCredential)
		model := harness.createModel(t, kube, reachableModelURL(t, origin), nil)
		before := model.DeepCopy()
		model.Spec.APIKeySecret, model.Spec.APIKeySecretKey = secret.Name, "token"
		require.NoError(t, kube.Patch(t.Context(), model, ctrlclient.MergeFrom(before)))
		template := &v1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-credentials-", Namespace: "kagent", Labels: harness.labels()},
			Spec:       v1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: model.Name}, SystemPrompt: "Reply briefly."},
		}
		createAndWaitInteractionTemplate(t, harness, kube, template)
		fixture := newInteractionFixtureForTemplate(t, harness, target, template.Name)
		_, _, task := fixture.send(t, "What is 2+2?")
		require.Equal(t, a2atype.TaskStateCompleted, task.Status.State, "task status: %+v", task.Status)
	})
}

// Match credentials at the model origin itself. A separate reverse proxy adds
// another streaming connection that can fail independently of credential delivery.
func startCredentialMock(t *testing.T, header, value string) string {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile("mocks/invoke_agent.json", interactionMocks)
	require.NoError(t, err)
	match := mockllm.HeaderMatch{Name: header, Value: value, MatchType: mockllm.MatchTypeExact}
	for i := range cfg.OpenAI {
		cfg.OpenAI[i].Match.Headers = append(cfg.OpenAI[i].Match.Headers, match)
	}
	for i := range cfg.OpenAIResponse {
		cfg.OpenAIResponse[i].Match.Headers = append(cfg.OpenAIResponse[i].Match.Headers, match)
	}
	for i := range cfg.Anthropic {
		cfg.Anthropic[i].Match.Headers = append(cfg.Anthropic[i].Match.Headers, match)
	}
	return startMockLLMConfig(t, cfg)
}

func TestCredentialMockRequiresHeader(t *testing.T) {
	for _, provider := range []struct {
		name, path, header, value, body, terminal string
		missingStatus                             int
	}{
		{
			name: "chat completions", path: "/v1/chat/completions", header: "Authorization", value: "Bearer credential-test",
			body: `{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"What is 2+2?"}],"stream":true}`, terminal: "data: [DONE]",
			missingStatus: http.StatusNotFound,
		},
		{
			name: "responses", path: "/v1/responses", header: "Authorization", value: "Bearer credential-test",
			body: `{"model":"gpt-5.2-codex","input":"What is 2+2?","stream":true}`, terminal: "event: response.completed",
			missingStatus: http.StatusNotFound,
		},
		{
			name: "anthropic", path: "/v1/messages", header: "X-Api-Key", value: "credential-test",
			body: `{"model":"claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"What is 2+2?"}]}],"stream":true}`, terminal: "event: message_stop",
			missingStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(provider.name, func(t *testing.T) {
			origin := startCredentialMock(t, provider.header, provider.value)
			for _, test := range []struct {
				name, value string
				wantStatus  int
			}{
				{name: "missing", wantStatus: provider.missingStatus},
				{name: "wrong", value: provider.value + "-wrong", wantStatus: http.StatusNotFound},
				{name: "correct", value: provider.value, wantStatus: http.StatusOK},
			} {
				t.Run(test.name, func(t *testing.T) {
					request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, origin+provider.path, strings.NewReader(provider.body))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					if provider.name == "anthropic" {
						request.Header.Set("Anthropic-Version", "2023-06-01")
					}
					if test.value != "" {
						request.Header.Set(provider.header, test.value)
					}
					response, err := http.DefaultClient.Do(request)
					require.NoError(t, err)
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.Equal(t, test.wantStatus, response.StatusCode, "%s", body)
					if test.wantStatus == http.StatusOK {
						require.Contains(t, string(body), provider.terminal)
					}
				})
			}
		})
	}
}
