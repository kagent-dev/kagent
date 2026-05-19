package hermes_test

import (
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
	"github.com/stretchr/testify/require"
)

func TestGatewayListenWaitScript(t *testing.T) {
	script := hermes.GatewayListenWaitScript(hermes.HermesInternalGatewayPort)
	require.Contains(t, script, "127.0.0.1:18642")
	require.Contains(t, script, "ss not found")
	require.Contains(t, script, "/tmp/gateway.log")
	require.Contains(t, script, "exit 1")
	require.NotContains(t, script, "done; exit 0")
	// Must not succeed after the poll loop without a listen match.
	lines := strings.Split(script, "\n")
	lastNonEmpty := ""
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			lastNonEmpty = strings.TrimSpace(line)
		}
	}
	require.Equal(t, "exit 1", lastNonEmpty)
}
