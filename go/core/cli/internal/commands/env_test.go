package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvCommandsOwnIndependentFlagState(t *testing.T) {
	first := NewEnvCmd()
	second := NewEnvCmd()
	first.SetArgs([]string{"--format", "json", "--component", "cli"})
	first.SetOut(&bytes.Buffer{})
	secondOutput := &bytes.Buffer{}
	second.SetOut(secondOutput)

	require.NoError(t, first.ExecuteContext(t.Context()))
	require.NoError(t, second.ExecuteContext(t.Context()))
	assert.Contains(t, secondOutput.String(), "# Kagent Environment Variables")
	assert.Contains(t, secondOutput.String(), "## controller")
}
