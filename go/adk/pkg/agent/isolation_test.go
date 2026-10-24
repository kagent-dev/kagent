package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// scriptedModels is an OpenAI-compatible server for a parent and a subagent. The
// parent calls the subagent's tool once per request in its first reply and then
// answers with the tool results. The subagent answers with what it was shown.
type scriptedModels struct {
	parentCalls []string // requests the parent sends to the "reviewer" tool, in one reply

	mu       sync.Mutex
	child    []childRequest
	parentTx [][]string // tool names offered to the parent, per request
}

type childRequest struct {
	Messages string   // role: content, one per line
	Tools    []string // tool names offered
}

type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

func (s *scriptedModels) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var tools []string
		for _, tool := range req.Tools {
			tools = append(tools, tool.Function.Name)
		}
		var transcript []string
		toolResults := 0
		for _, m := range req.Messages {
			transcript = append(transcript, fmt.Sprintf("%s: %v", m.Role, m.Content))
			if m.Role == "tool" {
				toolResults++
			}
		}

		w.Header().Set("Content-Type", "application/json")
		reply := func(message string) {
			_, _ = fmt.Fprintf(w, `{"id":"x","object":"chat.completion","model":"x","choices":[{"index":0,"message":%s,"finish_reason":"stop"}]}`, message)
		}
		if req.Model == "child-model" {
			s.mu.Lock()
			s.child = append(s.child, childRequest{Messages: strings.Join(transcript, "\n"), Tools: tools})
			s.mu.Unlock()
			reply(`{"role":"assistant","content":"child-answer"}`)
			return
		}

		s.mu.Lock()
		s.parentTx = append(s.parentTx, tools)
		s.mu.Unlock()
		if toolResults > 0 {
			reply(fmt.Sprintf(`{"role":"assistant","content":%q}`, fmt.Sprintf("parent saw %d tool results", toolResults)))
			return
		}
		calls := make([]string, len(s.parentCalls))
		for i, request := range s.parentCalls {
			args, _ := json.Marshal(map[string]string{"request": request})
			calls[i] = fmt.Sprintf(`{"id":"call-%d","type":"function","function":{"name":"reviewer","arguments":%q}}`, i, string(args))
		}
		reply(`{"role":"assistant","content":null,"tool_calls":[` + strings.Join(calls, ",") + `]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func openAIModel(url, name string) *configv1.ADKModelConfig {
	return &configv1.ADKModelConfig{Model: name, Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{BaseUrl: url}}}
}

func isolationConfig(url string, isolation configv1.ADKAgentIsolation) *configv1.ADKConfig {
	return &configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{
		Model: openAIModel(url, "parent-model"), Instruction: "coordinate", Stream: new(false),
		SubAgents: []*configv1.ADKAgentConfig{{
			Name: "reviewer", Description: "reviews work", Model: openAIModel(url, "child-model"),
			Instruction: "review", Stream: new(false), Isolation: isolation,
		}},
	}}
}

func runParent(t *testing.T, root adkagent.Agent, input string) {
	t.Helper()
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: root, SessionService: sessions})
	require.NoError(t, err)
	created, err := sessions.Create(t.Context(), &adksession.CreateRequest{AppName: "test", UserID: "user"})
	require.NoError(t, err)
	for _, err := range r.Run(t.Context(), "user", created.Session.ID(), genai.NewContentFromText(input, genai.RoleUser), adkagent.RunConfig{}) {
		require.NoError(t, err)
	}
}

func TestIsolatedSubAgentRunsAsAToolInASessionOfItsOwn(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	models := &scriptedModels{parentCalls: []string{"task one", "task two", "task three"}}
	server := models.serve(t)

	root, err := CreateGoogleADKAgent(t.Context(), isolationConfig(server.URL, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SESSION), "parent", nil)
	require.NoError(t, err)
	require.Empty(t, root.SubAgents(), "an isolated subagent is a tool, not a transfer target")

	runParent(t, root, "the parent's private conversation")

	require.Len(t, models.child, 3, "each parallel call runs in its own session")
	for _, call := range models.child {
		require.NotContains(t, call.Messages, "private conversation", "the subagent must not see the parent's history")
		seen := 0
		for _, task := range models.parentCalls {
			if strings.Contains(call.Messages, task) {
				seen++
			}
		}
		require.Equal(t, 1, seen, "a call saw another call's request:\n%s", call.Messages)
		require.NotContains(t, call.Tools, "ask_user", "an isolated subagent cannot reach a person")
	}
	require.Contains(t, models.parentTx[0], "reviewer", "the parent calls the subagent as a tool")
	require.Contains(t, models.parentTx[0], "ask_user", "the parent can still ask the user")
}

func TestSharedSubAgentStaysATransferTarget(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	models := &scriptedModels{}
	server := models.serve(t)
	for _, isolation := range []configv1.ADKAgentIsolation{
		configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_UNSPECIFIED,
		configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SHARED,
	} {
		t.Run(isolation.String(), func(t *testing.T) {
			root, err := CreateGoogleADKAgent(t.Context(), isolationConfig(server.URL, isolation), "parent", nil)
			require.NoError(t, err)
			require.Len(t, root.SubAgents(), 1)
			require.Equal(t, "reviewer", root.SubAgents()[0].Name())
		})
	}
}

func TestIsolationIsRejectedWhereItCannotWork(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	const url = "http://127.0.0.1:1"
	tests := []struct {
		name   string
		mutate func(*configv1.ADKConfig)
		want   string
	}{
		{"root agent", func(c *configv1.ADKConfig) {
			c.Agent.Isolation = configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SESSION
		}, "the root agent sets isolation"},
		{"approval on an MCP server", func(c *configv1.ADKConfig) {
			c.Agent.SubAgents[0].HttpTools = []*configv1.ADKHTTPMCPServerConfig{{
				Params: &configv1.ADKHTTPConnectionParams{Url: "http://tools.example.com/mcp"}, RequireApproval: true,
			}}
		}, `isolated subagent "reviewer" cannot pause for approval`},
		{"STS token exchange", func(c *configv1.ADKConfig) {
			c.Runtime = &configv1.ADKRuntimeConfig{TokenPropagation: &configv1.TokenPropagationConfig{
				Enabled: true, StsWellKnownUri: new("https://sts.example.com/.well-known/oauth-authorization-server"),
			}}
		}, "cannot be combined with STS token exchange"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := isolationConfig(url, configv1.ADKAgentIsolation_ADK_AGENT_ISOLATION_SESSION)
			tt.mutate(config)
			_, err := CreateGoogleADKAgent(t.Context(), config, "parent", nil)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestUndefinedIsolationValueIsInvalid(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	config := isolationConfig("http://127.0.0.1:1", configv1.ADKAgentIsolation(99))
	_, err := CreateGoogleADKAgent(t.Context(), config, "parent", nil)
	require.Error(t, err, "protovalidate rejects a value the enum does not define")
}
