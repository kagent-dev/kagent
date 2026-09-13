package runner

import (
	"context"
	"embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/mockllm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
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

func loadConfig(t *testing.T, path string, baseURL string) *adk.AgentConfig {
	t.Helper()
	data, err := testdata.ReadFile(path)
	require.NoError(t, err)

	raw := strings.ReplaceAll(string(data), "{{BASE_URL}}", baseURL)

	var cfg adk.AgentConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	return &cfg
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

	plugin, err := buildTokenPropagationPlugin(context.Background(), logr.Discard())
	require.NoError(t, err)
	assert.Nil(t, plugin, "plugin should be disabled when neither env var is set")
}

func TestBuildTokenPropagationPlugin_PropagateOnlyMode(t *testing.T) {
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "true")
	t.Setenv("STS_WELL_KNOWN_URI", "")

	plugin, err := buildTokenPropagationPlugin(context.Background(), logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, plugin, "plugin should be enabled in propagate-only mode without STS exchange")
}

func TestBuildTokenPropagationPlugin_PropagateFlagCaseInsensitive(t *testing.T) {
	t.Setenv("KAGENT_PROPAGATE_TOKEN", "TRUE")
	t.Setenv("STS_WELL_KNOWN_URI", "")

	plugin, err := buildTokenPropagationPlugin(context.Background(), logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, plugin)
}

// CreateRunnerConfig: full path via mockllm.

func TestCreateRunnerConfig_MinimalOpenAI(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai.json", baseURL)
	sessionService := adksession.InMemoryService()

	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, sessionService, "myapp", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "myapp", runnerCfg.AppName)
	assert.NotNil(t, runnerCfg.Agent)
	assert.Equal(t, sessionService, runnerCfg.SessionService)
}

func TestCreateRunnerConfig_DefaultsAppNameWhenEmpty(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai.json", baseURL)

	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, nil, "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "kagent-app", runnerCfg.AppName)
	assert.NotNil(t, runnerCfg.SessionService, "should fall back to an in-memory session service")
}

func TestCreateRunnerConfig_ShareToolsRequiresControllerClient(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai.json", baseURL)
	shareTools := true
	cfg.ShareTools = &shareTools

	// controllerClient is nil, so share tools should be silently skipped
	// rather than causing an error (per the `controllerClient != nil` guard).
	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, nil, "myapp", nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, runnerCfg.Agent)
}

func TestCreateRunnerConfig_ShareToolsWithControllerClient(t *testing.T) {
	baseURL := startMock(t, "testdata/mock_openai.json")
	t.Setenv("OPENAI_API_KEY", "test-key")

	cfg := loadConfig(t, "testdata/config_openai.json", baseURL)
	shareTools := true
	cfg.ShareTools = &shareTools

	controllerClient := &controllerclient.Client{}

	runnerCfg, err := CreateRunnerConfig(context.Background(), cfg, nil, "myapp", nil, controllerClient)
	require.NoError(t, err)
	assert.NotNil(t, runnerCfg.Agent, "agent should build successfully with share tools wired in")
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
