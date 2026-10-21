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
	strictSchema := []byte(`{"type":"object","properties":{"due":{"type":"string"}},"required":["due"],"additionalProperties":false}`)
	openSchema := []byte(`{"type":"object","properties":{"due":{"type":"string"},"note":{"type":"string"}},"required":["due"]}`)
	for _, tt := range []struct {
		name    string
		schema  []byte
		strict  *bool
		wantErr string
	}{
		{name: "unset with open object", schema: openSchema},
		{name: "false with open object", schema: openSchema, strict: new(false)},
		{name: "true with strict schema", schema: strictSchema, strict: new(true)},
		{name: "true with open object", schema: openSchema, strict: new(true), wantErr: "outputSchemaStrict: strict output schema: the root object must set additionalProperties to false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{}
			err := applyOutputSchema(config, &translator.ResolvedOutputSchema{Schema: tt.schema, Strict: tt.strict})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, config.Output)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.strict, config.Output.Strict)
		})
	}
}
