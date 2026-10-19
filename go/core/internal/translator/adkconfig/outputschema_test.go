package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestApplyOutputSchema(t *testing.T) {
	config := &configv1.ADKAgentConfig{}
	output := &translator.ResolvedOutputSchema{
		Schema: []byte(`{"type":"object","$defs":{"status":{"type":"string"}},"properties":{"status":{"$ref":"#/$defs/status"}}}`),
		SHA256: "digest",
	}

	require.NoError(t, applyOutputSchema(config, output))
	require.JSONEq(t, string(output.Schema), string(config.Output.JsonSchema))
	require.Equal(t, "digest", config.Output.Sha256)
}

func TestApplyOutputSchemaRejectsRecursiveADKConversion(t *testing.T) {
	config := &configv1.ADKAgentConfig{}
	output := &translator.ResolvedOutputSchema{Schema: []byte(`{
		"type":"object",
		"$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},
		"properties":{"node":{"$ref":"#/$defs/node"}}
	}`)}

	err := applyOutputSchema(config, output)
	require.ErrorContains(t, err, "recursive output schema")
	require.Nil(t, config.Output)
}

func TestApplyOutputSchemaStrict(t *testing.T) {
	config := &configv1.ADKAgentConfig{}
	output := &translator.ResolvedOutputSchema{
		Schema: []byte(`{"type":"object","properties":{"due":{"type":"string"}},"required":["due"],"additionalProperties":false}`),
		Strict: true,
	}

	require.NoError(t, applyOutputSchema(config, output))
	require.True(t, config.Output.Strict)
}

func TestApplyOutputSchemaStrictRejectsOpenObject(t *testing.T) {
	config := &configv1.ADKAgentConfig{}
	output := &translator.ResolvedOutputSchema{
		Schema: []byte(`{"type":"object","properties":{"due":{"type":"string"}},"required":["due"]}`),
		Strict: true,
	}

	err := applyOutputSchema(config, output)
	require.ErrorContains(t, err, "outputSchemaStrict: strict output schema: the root object must set additionalProperties to false")
	require.Nil(t, config.Output)
}
