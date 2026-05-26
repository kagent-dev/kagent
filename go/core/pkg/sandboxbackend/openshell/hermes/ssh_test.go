package hermes_test

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/openshell/hermes"
	"github.com/stretchr/testify/require"
)

func TestDefaultSSHLaunchCommand(t *testing.T) {
	require.Equal(t, "cd /sandbox/.hermes && exec hermes", hermes.DefaultSSHLaunchCommand())
}
