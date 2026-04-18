package agent

import (
	"context"
	"embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

//go:embed testdata
var testdata embed.FS

func startMock(t *testing.T, mockFile string) string {
	t.Helper()
	cfg, err := mockllm.LoadConfigFromFile(mockFile, testdata)
	require.NoError(t, err)
	server := mockllm.NewServer(cfg)
	baseURL, err := server.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { server.Stop(context.Background()) }) //nolint:errcheck
	return baseURL
}

// loadConfig reads a config JSON from testdata, replaces {{BASE_URL}} with the
// mock server address, and returns the parsed AgentConfig.
func loadConfig(t *testing.T, path string, baseURL string) *adk.AgentConfig {
	t.Helper()
	data, err := testdata.ReadFile(path)
	require.NoError(t, err)

	raw := strings.ReplaceAll(string(data), "{{BASE_URL}}", baseURL)

	var cfg adk.AgentConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	return &cfg
}

// runAgent creates an agent from config, sends a prompt through the full
// runner pipeline, and returns all text from the response events.
func runAgent(t *testing.T, agentCfg *adk.AgentConfig, prompt string) string {
	t.Helper()
	ctx := logr.NewContext(t.Context(), logr.Discard())

	adkAgent, err := CreateGoogleADKAgent(ctx, agentCfg, "test-agent")
	require.NoError(t, err)

	sessionService := adksession.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:        "test",
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	require.NoError(t, err)

	sess, err := sessionService.Create(ctx, &adksession.CreateRequest{
		AppName: "test",
		UserID:  "user",
	})
	require.NoError(t, err)

	content := &genai.Content{
		Role:  string(genai.RoleUser),
		Parts: []*genai.Part{{Text: prompt}},
	}

	var sb strings.Builder
	for ev, err := range r.Run(ctx, "user", sess.Session.ID(), content, adkagent.RunConfig{}) {
		require.NoError(t, err)
		if ev == nil || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.Text != "" {
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}

func TestAgent_OpenAI(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai.json", baseURL)
	text := runAgent(t, cfg, "What is 2+2?")
	assert.Contains(t, text, "4")
}

func TestAgent_OpenAI_WithParams(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai_params.json", baseURL)
	text := runAgent(t, cfg, "What is 2+2?")
	assert.Contains(t, text, "4")
}

func TestAgent_Ollama(t *testing.T) {
	// mockllm does not support the native Ollama /api/chat endpoint,
	// so we test with an OpenAI-compatible model pointing at the mock.
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "ollama") // placeholder, Ollama ignores it

	cfg := loadConfig(t, "testdata/config_ollama.json", baseURL)
	text := runAgent(t, cfg, "What is 2+2?")
	assert.Contains(t, text, "4")
}

func TestAgent_Anthropic(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_anthropic.json")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_anthropic.json", baseURL)
	text := runAgent(t, cfg, "What is 2+2?")
	assert.Contains(t, text, "4")
}
