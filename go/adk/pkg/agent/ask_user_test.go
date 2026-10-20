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
		name        string
		root, child *bool
		want        []bool
	}{
		{name: "omitted", want: []bool{true, true, true}},
		{name: "enabled", root: new(true), child: new(true), want: []bool{true, true, true}},
		{name: "disabled", root: new(false), want: []bool{false, false, false}},
		{name: "child cannot reenable", root: new(false), child: new(true), want: []bool{false, false, false}},
		{name: "child disables its subtree", child: new(false), want: []bool{true, false, false}},
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
			config := &configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{
				Model: model, Stream: new(false), AskUser: tc.root,
				SubAgents: []*configv1.ADKAgentConfig{{
					Name: "child", Model: model, Stream: new(false), AskUser: tc.child,
					SubAgents: []*configv1.ADKAgentConfig{{Name: "grandchild", Model: model, Stream: new(false), AskUser: new(true)}},
				}},
			}}
			root, err := CreateGoogleADKAgent(t.Context(), config, "root", nil)
			require.NoError(t, err)
			child := root.SubAgents()[0]
			for i, agent := range []adkagent.Agent{root, child, child.SubAgents()[0]} {
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
					require.Equal(t, tc.want[i], slices.Contains(names, "ask_user"), agent.Name())
					require.Contains(t, names, "read_file", "unrelated built-in tools remain available")
				case <-time.After(time.Second):
					t.Fatal("agent completed without a model request")
				}
			}
		})
	}
}
