package runner

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Pure helper functions.

func TestAgentNameFromAppName(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    string
	}{
		{"no namespace marker returns as-is", "myagent", "myagent"},
		{"namespace marker strips prefix", "default__NS__myagent", "myagent"},
		{"multiple markers uses last occurrence", "a__NS__b__NS__c", "c"},
		{"empty string", "", ""},
		{"marker at end returns empty suffix", "default__NS__", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, agentNameFromAppName(tt.appName))
		})
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty string returns nil", "", nil},
		{"single value", "foo", []string{"foo"}},
		{"multiple values", "foo,bar,baz", []string{"foo", "bar", "baz"}},
		{"trims whitespace around values", " foo , bar ,baz ", []string{"foo", "bar", "baz"}},
		{"skips empty entries", "foo,,bar,", []string{"foo", "bar"}},
		{"all empty entries returns nil", " , , ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, splitCSV(tt.in))
		})
	}
}

// buildTokenPropagationPlugin: env-var driven branching.

func TestBuildTokenPropagationPlugin_DisabledByDefault(t *testing.T) {
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "")
	t.Setenv("STS_WELL_KNOWN_URI", "")
	plugin, err := buildTokenPropagationPlugin(context.Background(), discardLogger())
	require.NoError(t, err)
	assert.Nil(t, plugin, "plugin should be disabled when neither env var is set")
}

func TestBuildTokenPropagationPlugin_PropagateOnlyMode(t *testing.T) {
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "true")
	t.Setenv("STS_WELL_KNOWN_URI", "")
	plugin, err := buildTokenPropagationPlugin(context.Background(), discardLogger())
	require.NoError(t, err)
	require.NotNil(t, plugin, "plugin should be enabled in propagate-only mode without STS exchange")
}

func TestBuildTokenPropagationPlugin_PropagateFlagCaseInsensitive(t *testing.T) {
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "TRUE")
	t.Setenv("STS_WELL_KNOWN_URI", "")
	plugin, err := buildTokenPropagationPlugin(context.Background(), discardLogger())
	require.NoError(t, err)
	require.NotNil(t, plugin)
}

// CreateRunnerConfig: config wiring, no model fixture or mock server needed.

func minimalOpenAIConfig() *adk.AgentConfig {
	return &adk.AgentConfig{
		Description: "test",
		Instruction: "You are helpful. Answer concisely.",
		Model: &adk.OpenAI{
			BaseModel: adk.BaseModel{
				Type:  adk.ModelTypeOpenAI,
				Model: "gpt-4.1-mini",
			},
			BaseUrl: "http://127.0.0.1:0/v1", // never dialed; this only exercises config wiring
		},
	}
}

func TestCreateRunnerConfig_MinimalOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	cfg := minimalOpenAIConfig()
	sessionService := adksession.InMemoryService()

	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, sessionService, "myapp", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "myapp", runnerCfg.AppName)
	assert.NotNil(t, runnerCfg.Agent)
	assert.Equal(t, sessionService, runnerCfg.SessionService)
}

func TestCreateRunnerConfig_DefaultsAppNameWhenEmpty(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	cfg := minimalOpenAIConfig()

	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, nil, "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "kagent-app", runnerCfg.AppName)
	assert.NotNil(t, runnerCfg.SessionService, "should fall back to an in-memory session service")
}

func TestCreateRunnerConfig_MissingModelFails(t *testing.T) {
	cfg := &adk.AgentConfig{
		Description: "test",
		Instruction: "test",
		// Model deliberately omitted.
	}
	_, err := CreateRunnerConfig(context.Background(), cfg, nil, "myapp", nil, nil)
	require.Error(t, err)
}
