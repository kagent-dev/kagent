package adk_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestSharedRuntimeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/runtime.json")
	require.NoError(t, err)
	var cfg adk.RuntimeConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.NoError(t, cfg.Validate())
	require.NotNil(t, cfg.MaxContentLength)
	require.Zero(t, *cfg.MaxContentLength)
	require.False(t, cfg.TokenPropagation.Enabled)
	require.False(t, cfg.Telemetry.CaptureContent)
	require.Equal(t, int64(1500), cfg.Telemetry.Exporters.Traces.TimeoutMillis)
	root := &adk.AgentConfig{Runtime: &cfg, SubAgents: []*adk.AgentConfig{{}}}
	require.NoError(t, root.ValidateRuntime())
	root.SubAgents[0].Runtime = &adk.RuntimeConfig{}
	require.ErrorContains(t, root.ValidateRuntime(), "only allowed on the root")
	root.SubAgents[0].Runtime = nil
	root.SubAgents[0].SubAgents = []*adk.AgentConfig{{Runtime: &cfg}}
	require.ErrorContains(t, root.ValidateRuntime(), "only allowed on the root")
}

func TestRuntimeDefaultsAndValidation(t *testing.T) {
	var cfg adk.RuntimeConfig
	require.NoError(t, cfg.Validate())
	require.Nil(t, cfg.MaxContentLength)
	for _, data := range []string{
		`{"port":"0"}`, `{"max_content_length":-1}`, `{"control_plane_url":"file:///private"}`,
		`{"token_propagation":{"resources":[""]}}`, `{"telemetry":{"agent_name":"partial"}}`,
	} {
		cfg = adk.RuntimeConfig{}
		require.NoError(t, json.Unmarshal([]byte(data), &cfg))
		require.Error(t, cfg.Validate(), data)
	}
}
