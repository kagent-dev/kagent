package config_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestAgentConfigRoundTrip(t *testing.T) {
	cfg := &configv1.ADKConfig{
		Runtime: &configv1.ADKRuntimeConfig{MaxContentLength: new(int64(0))},
		Agent:   &configv1.ADKAgentConfig{Model: &configv1.ADKModelConfig{Model: "gpt", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{Temperature: new(0.0)}}}, Stream: new(false)},
	}
	raw, err := runtimeconfig.Marshal(cfg)
	require.NoError(t, err)
	parsed := new(configv1.ADKConfig)
	require.NoError(t, runtimeconfig.Unmarshal(raw, parsed))
	require.True(t, proto.Equal(cfg, parsed))
	require.NotNil(t, parsed.Agent.Stream)
	require.NotNil(t, parsed.Runtime.MaxContentLength)
	require.NotNil(t, parsed.Agent.Model.GetOpenai().Temperature)
}
func TestAgentConfigRejectsInvalidInput(t *testing.T) {
	for _, raw := range []string{
		`{"agent":{"model":{}}}`,
		`{"agent":{"model":{"openai":{},"anthropic":{}}}}`,
		`{"unknown":true}`,
		`{"model":{"openai":{}}}`,
		`{"agent":{"runtime":{}}}`,
		`{"agent":{"sub_agents":[{"runtime":{}}]}}`,
		`{"agent":{"sub_agents":[{"sub_agents":[{"runtime":{}}]}]}}`,
		`{"agent":{"agent_plugins":{"skills":[{"name":"skill","source":{"oci":"registry.example.com/skill"}}]}}}`,
		`{"runtime":{"max_content_length":-1}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			cfg := &configv1.ADKConfig{Agent: &configv1.ADKAgentConfig{Instruction: "original"}}
			require.Error(t, runtimeconfig.Unmarshal([]byte(raw), cfg))
			require.Equal(t, "original", cfg.Agent.Instruction)
		})
	}
}

// The same fixture is loaded by Python. It covers oneofs, exact int64s, binary
// schema bytes, arbitrary provider JSON, and explicit false/zero presence.
func TestSharedAgentContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/agent.json")
	require.NoError(t, err)
	cfg := new(configv1.ADKConfig)
	require.NoError(t, runtimeconfig.Unmarshal(raw, cfg))
	require.Equal(t, 250*time.Millisecond, cfg.Agent.HttpTools[0].Params.Timeout.AsDuration())
	require.Equal(t, int64(9007199254740993), cfg.Agent.Model.GetOpenai().GetSeed())
	require.NotNil(t, cfg.Agent.Model.GetOpenai().Temperature)
	require.Zero(t, cfg.Agent.Model.GetOpenai().GetTemperature())
	require.NotNil(t, cfg.Agent.Stream)
	require.False(t, cfg.Agent.GetStream())
	require.NotNil(t, cfg.Agent.Model.TlsInsecureSkipVerify)
	require.NotNil(t, cfg.Agent.HttpTools[0].Params.TerminateOnClose)
	require.NotNil(t, cfg.Agent.ContextConfig.Compaction.OverlapSize)
	require.Equal(t, `{"type":"object","properties":{"answer":{"type":"string"}}}`, string(cfg.Agent.Output.JsonSchema))
	for _, child := range cfg.Agent.SubAgents {
		require.Equal(t, child.Name, runtimeconfig.ModelType(child.Model))
	}
	require.Equal(t, "version", cfg.Agent.AgentPlugins.Plugins[1].Source.GetS3().VersionId)
	resources := runtimeconfig.PluginResources(cfg.Agent.AgentPlugins)
	require.Equal(t, "/run/kagent/artifacts/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", resources.Skills[0].Source.Directory)
	require.True(t, proto.Equal(cfg.Agent.AgentPlugins, runtimeconfig.PluginConfig(resources)))
	encoded, err := runtimeconfig.Marshal(cfg)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(encoded))
	require.NotContains(t, string(encoded), "\n")
	require.NoError(t, runtimeconfig.Unmarshal(encoded, new(configv1.ADKConfig)))
}

func TestModelToEmbeddingConfig(t *testing.T) {
	model := &configv1.ADKModelConfig{Model: "embedding", PassthroughHeaders: []*configv1.InvocationCredentialHeader{{Name: "Api-Key"}}, TlsInsecureSkipVerify: new(false), Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{Endpoint: "https://foundry.example.com", Deployment: "embedding", ApiVersion: "version"}}}
	cfg := runtimeconfig.ModelToEmbeddingConfig(model)
	require.Equal(t, "foundry", cfg.Provider)
	require.Equal(t, "https://foundry.example.com", cfg.Endpoint)
	require.Equal(t, "embedding", cfg.Deployment)
	require.Equal(t, "version", cfg.ApiVersion)
	require.Equal(t, model.PassthroughHeaders, cfg.PassthroughHeaders)
	require.NotNil(t, cfg.TlsInsecureSkipVerify)
	require.False(t, *cfg.TlsInsecureSkipVerify)
	model.Provider = &configv1.ADKModelConfig_Bedrock{Bedrock: &configv1.ADKBedrock{}}
	require.Equal(t, model.PassthroughHeaders, runtimeconfig.ModelToEmbeddingConfig(model).PassthroughHeaders)
}

// Both runtimes load these cases to enforce the same numeric boundaries.
func TestSharedNumericValidation(t *testing.T) {
	raw, err := os.ReadFile("testdata/numeric_cases.json")
	require.NoError(t, err)
	var cases []struct {
		Name   string          `json:"name"`
		Valid  bool            `json:"valid"`
		Config json.RawMessage `json:"config"`
	}
	require.NoError(t, json.Unmarshal(raw, &cases))
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			err := runtimeconfig.Unmarshal(test.Config, new(configv1.ADKConfig))
			if test.Valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
