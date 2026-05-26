package openclaw_test

import (
	"encoding/json"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"github.com/stretchr/testify/require"
)

func TestSubstrateGatewayBootstrap(t *testing.T) {
	t.Parallel()
	raw, err := openclaw.BuildGatewayOnlyBootstrapJSON(openclaw.SubstrateGatewayBootstrap("tok", 80, "/api/agentharnesses/kagent/claw/gateway/"))
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(raw, &root))
	gw := root["gateway"].(map[string]any)
	require.Equal(t, "lan", gw["bind"])
	cui := gw["controlUi"].(map[string]any)
	require.Equal(t, "/api/agentharnesses/kagent/claw/gateway", cui["basePath"])
	require.Equal(t, true, cui["dangerouslyDisableDeviceAuth"])
}
