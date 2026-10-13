package adk_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestAgentConfigRoundTrip(t *testing.T) {
	cfg := &adk.Config{
		Runtime: &adk.RuntimeConfig{MaxContentLength: new(int64(0))},
		Agent:   &adk.AgentConfig{Model: &adk.Model{Model: "gpt", Provider: &adk.Model_Openai{Openai: &adk.OpenAI{Temperature: new(0.0)}}}, Stream: new(false)},
	}
	raw, err := adk.Marshal(cfg)
	require.NoError(t, err)
	parsed := new(adk.Config)
	require.NoError(t, adk.Unmarshal(raw, parsed))
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
		`{"runtime":{"max_content_length":-1}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			cfg := &adk.Config{Agent: &adk.AgentConfig{Instruction: "original"}}
			require.Error(t, adk.Unmarshal([]byte(raw), cfg))
			require.Equal(t, "original", cfg.Agent.Instruction)
		})
	}
}

// The same fixture is loaded by Python. It covers oneofs, exact int64s, binary
// schema bytes, arbitrary provider JSON, and explicit false/zero presence.
func TestSharedAgentContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/agent.json")
	require.NoError(t, err)
	cfg := new(adk.Config)
	require.NoError(t, adk.Unmarshal(raw, cfg))
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
		require.Equal(t, child.Name, adk.ModelType(child.Model))
	}
	require.Equal(t, "version", cfg.Agent.AgentPlugins.Plugins[1].Source.GetS3().VersionId)
	resources := adk.PluginResources(cfg.Agent.AgentPlugins)
	require.True(t, proto.Equal(cfg.Agent.AgentPlugins, adk.PluginConfig(resources)))
	encoded, err := adk.Marshal(cfg)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(encoded))
	require.NotContains(t, string(encoded), "\n")
	require.NoError(t, adk.Unmarshal(encoded, new(adk.Config)))
}

func TestModelToEmbeddingConfig(t *testing.T) {
	model := &adk.Model{Model: "embedding", ApiKeyPassthrough: true, TlsInsecureSkipVerify: new(false), Provider: &adk.Model_Foundry{Foundry: &adk.Foundry{Endpoint: "https://foundry.example.com", Deployment: "embedding", ApiVersion: "version"}}}
	cfg := adk.ModelToEmbeddingConfig(model)
	require.Equal(t, "foundry", cfg.Provider)
	require.Equal(t, "https://foundry.example.com", cfg.Endpoint)
	require.Equal(t, "embedding", cfg.Deployment)
	require.Equal(t, "version", cfg.ApiVersion)
	require.True(t, cfg.ApiKeyPassthrough)
	require.NotNil(t, cfg.TlsInsecureSkipVerify)
	require.False(t, *cfg.TlsInsecureSkipVerify)
	model.Provider = &adk.Model_Bedrock{Bedrock: &adk.Bedrock{}}
	require.False(t, adk.ModelToEmbeddingConfig(model).ApiKeyPassthrough)
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
			err := adk.Unmarshal(test.Config, new(adk.Config))
			if test.Valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
