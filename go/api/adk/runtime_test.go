package adk_test

import (
	"os"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/kagent-dev/kagent/go/api/adk"
)

func TestSharedRuntimeContract(t *testing.T) {
	data, err := os.ReadFile("testdata/runtime.json")
	require.NoError(t, err)
	var cfg adk.RuntimeConfig
	require.NoError(t, protojson.Unmarshal(data, &cfg))
	require.NoError(t, protovalidate.Validate(&cfg))
	require.NotNil(t, cfg.MaxContentLength)
	require.Zero(t, *cfg.MaxContentLength)
	require.False(t, cfg.TokenPropagation.Enabled)
	require.False(t, cfg.Telemetry.CaptureContent)
	require.Equal(t, int64(1500), cfg.Telemetry.Traces.Timeout.AsDuration().Milliseconds())
	root := &adk.Config{Runtime: &cfg, Agent: &adk.AgentConfig{SubAgents: []*adk.AgentConfig{{}}}}
	require.NoError(t, protovalidate.Validate(root))
	require.Same(t, &cfg, adk.ProcessConfig(root))
}

func TestRuntimeDefaultsAndValidation(t *testing.T) {
	var cfg adk.RuntimeConfig
	require.NoError(t, protovalidate.Validate(&cfg))
	require.Nil(t, cfg.MaxContentLength)
	for _, data := range []string{
		`{"port":"0"}`, `{"max_content_length":-1}`, `{"control_plane_url":"file:///private"}`,
		`{"token_propagation":{"resources":[""]}}`, `{"telemetry":{"max_capture_bytes":-1}}`,
		`{"telemetry":{"resource_attributes":{"service.name":"shared"}}}`,
		`{"telemetry":{"resource_attributes":{"service.namespace":"shared"}}}`,
	} {
		cfg = adk.RuntimeConfig{}
		require.NoError(t, protojson.Unmarshal([]byte(data), &cfg))
		require.Error(t, protovalidate.Validate(&cfg), data)
	}
}
