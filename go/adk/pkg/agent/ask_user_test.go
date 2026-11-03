package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// Inspect real model requests, including those from shared children, rather than
// only checking the configured boolean.
func TestAskUserAvailability(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	for _, tc := range []struct {
		name    string
		runtime *configv1.ADKRuntimeConfig
		want    bool
	}{
		{name: "omitted runtime", want: true},
		{name: "omitted builtin tools", runtime: &configv1.ADKRuntimeConfig{}, want: true},
		{name: "enabled", runtime: &configv1.ADKRuntimeConfig{BuiltinTools: &configv1.BuiltinToolsConfig{DisableAskUser: false}}, want: true},
		{name: "disabled", runtime: &configv1.ADKRuntimeConfig{BuiltinTools: &configv1.BuiltinToolsConfig{DisableAskUser: true}}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan []string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Tools []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode model request: %v", err)
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				var names []string
				for _, tool := range request.Tools {
					names = append(names, tool.Function.Name)
				}
				requests <- names
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"id":"test","object":"chat.completion","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
				if err != nil {
					t.Errorf("write model response: %v", err)
				}
			}))
			defer server.Close()
			model := &configv1.ADKModelConfig{Model: "test", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: server.URL}}}
			config := &configv1.ADKConfig{Runtime: tc.runtime, Agent: &configv1.ADKAgentConfig{
				Model: model, Stream: new(false),
				SubAgents: []*configv1.ADKAgentConfig{{
					Name: "child", Model: model, Stream: new(false),
					SubAgents: []*configv1.ADKAgentConfig{{Name: "grandchild", Model: model, Stream: new(false)}},
				}},
			}}
			root, err := CreateGoogleADKAgent(t.Context(), config, "root", nil)
			require.NoError(t, err)
			child := root.SubAgents()[0]
			for _, agent := range []adkagent.Agent{root, child, child.SubAgents()[0]} {
				sessions := adksession.InMemoryService()
				r, err := runner.New(runner.Config{AppName: "test", Agent: agent, SessionService: sessions})
				require.NoError(t, err)
				sess, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "test", UserID: "user"})
				require.NoError(t, err)
				for _, err := range r.Run(t.Context(), "user", sess.Session.ID(), genai.NewContentFromText("hello", "user"), adkagent.RunConfig{}) {
					require.NoError(t, err)
				}
				select {
				case names := <-requests:
					require.Equal(t, tc.want, slices.Contains(names, "ask_user"), agent.Name())
					require.Contains(t, names, "read_file", "unrelated built-in tools remain available")
				case <-time.After(time.Second):
					t.Fatal("agent completed without a model request")
				}
			}
		})
	}
}
