package adkconfig

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	adkagent "github.com/kagent-dev/kagent/go/adk/pkg/agent"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"istio.io/istio/pkg/kube/krt"
)

// Exercise the compiled payload through the real Go ADK executor against an
// endpoint that rejects streaming, without requiring a Substrate deployment.
func TestNonStreamingModelTurn(t *testing.T) {
	for _, tc := range []struct {
		format   v1alpha3.OpenAIAPIFormat
		path     string
		response string
	}{
		{
			format: v1alpha3.OpenAIAPIFormatChatCompletions, path: "/v1/chat/completions",
			response: `{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4.1-mini",
				"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`,
		},
		{
			format: v1alpha3.OpenAIAPIFormatResponses, path: "/v1/responses",
			response: `{"id":"resp-1","object":"response","model":"gpt-4.1-mini","status":"completed",
				"output":[{"id":"msg-1","type":"message","role":"assistant","status":"completed",
				"content":[{"type":"output_text","text":"OK","annotations":[]}]}]}`,
		},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if request.Stream || r.URL.Path != tc.path {
					http.Error(w, "unsupported request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, err := io.WriteString(w, tc.response)
				if err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			model := openAIModel("agent", server.URL+"/v1")
			model.Spec.Stream = new(false)
			model.Spec.OpenAI.APIFormat = new(tc.format)
			collections := contextTestCollections(t, model)
			result, err := NewBuilder(krt.TestingDummyContext{}, collections).Build(t.Context(), &v2translator.HarnessInput{
				Harness: &v2translator.HarnessConfiguration{Spec: v1alpha3.HarnessSpec{Kagent: &v1alpha3.KagentHarness{}}},
				Root: &v2translator.AgentInput{
					Template: &v2translator.TemplateConfiguration{}, ResolvedModelConfig: resolvedModel(t, collections, "agent"),
				},
			})
			require.NoError(t, err)
			payload, err := json.Marshal(result.Config)
			require.NoError(t, err)
			var config adk.AgentConfig
			require.NoError(t, json.Unmarshal(payload, &config))
			agent, err := adkagent.CreateGoogleADKAgent(t.Context(), &config, "agent", nil)
			require.NoError(t, err)
			executor, err := adka2a.NewKAgentExecutor(adka2a.KAgentExecutorConfig{
				AppName: "test", Stream: config.GetStream(), Logger: slog.New(slog.DiscardHandler),
				SessionService: adksession.InMemoryService(), RunnerConfig: runner.Config{AppName: "test", Agent: agent},
			})
			require.NoError(t, err)
			request := &a2asrv.ExecutorContext{
				TaskID: "task-1", ContextID: "context-1",
				Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("Reply OK")),
			}
			var state a2atype.TaskState
			var text strings.Builder
			for event, err := range executor.Execute(t.Context(), request) {
				require.NoError(t, err)
				switch event := event.(type) {
				case *a2atype.TaskArtifactUpdateEvent:
					for _, part := range event.Artifact.Parts {
						text.WriteString(part.Text())
					}
				case *a2atype.TaskStatusUpdateEvent:
					state = event.Status.State
				}
			}
			require.Equal(t, a2atype.TaskStateCompleted, state)
			require.Equal(t, "OK", text.String())
		})
	}
}
