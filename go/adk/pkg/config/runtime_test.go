package config_test

import (
	"os"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestSharedRuntimeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/runtime.json")
	require.NoError(t, err)
	var cfg configv1.ADKRuntimeConfig
	require.NoError(t, protojson.Unmarshal(data, &cfg))
	require.NoError(t, protovalidate.Validate(&cfg))
	require.NotNil(t, cfg.MaxContentLength)
	require.Zero(t, *cfg.MaxContentLength)
	require.False(t, cfg.TokenPropagation.Enabled)
	require.Equal(t, "/opt/venv", cfg.GetBuiltinTools().GetBashVenvPath())
	require.False(t, cfg.GetBuiltinTools().GetDisableFileSearch())
	require.True(t, cfg.GetBuiltinTools().GetDisableBash())
	require.False(t, cfg.Telemetry.CaptureContent)
	require.Equal(t, int64(1500), cfg.Telemetry.Traces.Timeout.AsDuration().Milliseconds())
	root := &configv1.ADKConfig{Runtime: &cfg, Agent: &configv1.ADKAgentConfig{SubAgents: []*configv1.ADKAgentConfig{{}}}}
	require.NoError(t, protovalidate.Validate(root))
	require.Same(t, &cfg, runtimeconfig.ProcessConfig(root))
}

func TestRuntimeDefaultsAndValidation(t *testing.T) {
	var cfg configv1.ADKRuntimeConfig
	require.NoError(t, protovalidate.Validate(&cfg))
	require.Nil(t, cfg.MaxContentLength)
	require.False(t, cfg.GetBuiltinTools().GetDisableFileSearch())
	require.False(t, cfg.GetBuiltinTools().GetDisableBash())
	for _, data := range []string{
		`{"port":"0"}`, `{"max_content_length":-1}`, `{"control_plane_url":"file:///private"}`,
		`{"token_propagation":{"resources":[""]}}`, `{"telemetry":{"max_capture_bytes":-1}}`,
		`{"telemetry":{"resource_attributes":{"service.name":"shared"}}}`,
		`{"telemetry":{"resource_attributes":{"service.namespace":"shared"}}}`,
	} {
		cfg = configv1.ADKRuntimeConfig{}
		require.NoError(t, protojson.Unmarshal([]byte(data), &cfg))
		require.Error(t, protovalidate.Validate(&cfg), data)
	}
}
