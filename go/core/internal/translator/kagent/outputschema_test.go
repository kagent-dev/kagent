package kagent

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
)

func TestApplyOutputSchema(t *testing.T) {
	config := &adk.AgentConfig{}
	output := &v2translator.ResolvedOutputSchema{
		Schema: []byte(`{"type":"object","$defs":{"status":{"type":"string"}},"properties":{"status":{"$ref":"#/$defs/status"}}}`),
		SHA256: "digest",
	}

	require.NoError(t, applyOutputSchema(config, output))
	require.JSONEq(t, string(output.Schema), string(config.Output.JSONSchema))
	require.Equal(t, "digest", config.Output.SHA256)
}

func TestApplyOutputSchemaRejectsRecursiveADKProjection(t *testing.T) {
	config := &adk.AgentConfig{}
	output := &v2translator.ResolvedOutputSchema{Schema: []byte(`{
		"type":"object",
		"$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},
		"properties":{"node":{"$ref":"#/$defs/node"}}
	}`)}

	err := applyOutputSchema(config, output)
	require.ErrorContains(t, err, "recursive output schema")
	require.Nil(t, config.Output)
}
