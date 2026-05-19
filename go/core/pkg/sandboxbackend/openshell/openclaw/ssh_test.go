package openclaw_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/openclaw"
	"github.com/stretchr/testify/require"
)

func TestDefaultSSHLaunchCommand(t *testing.T) {
	require.Equal(t, "openclaw tui", openclaw.DefaultSSHLaunchCommand())
}
