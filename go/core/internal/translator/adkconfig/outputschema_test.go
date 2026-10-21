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

func TestApplyOutputSchemaGemini(t *testing.T) {
	multiType := []byte(`{"type":"object","properties":{"code":{"type":["string","integer"]}}}`)
	nullable := []byte(`{"type":"object","properties":{"code":{"type":["string","null"]}}}`)
	for _, tt := range []struct {
		name    string
		model   *configv1.ADKModelConfig
		schema  []byte
		wantErr string
	}{
		{
			name:    "Gemini with a multi-type list",
			model:   &configv1.ADKModelConfig{Model: "gemini-3.5-flash-lite", Provider: &configv1.ADKModelConfig_Gemini{}},
			schema:  multiType,
			wantErr: "/properties/code must have exactly one type",
		},
		{
			name:   "Gemini with a nullable string",
			model:  &configv1.ADKModelConfig{Model: "gemini-3.5-flash-lite", Provider: &configv1.ADKModelConfig_Gemini{}},
			schema: nullable,
		},
		{
			name:   "OpenAI with a multi-type list",
			model:  &configv1.ADKModelConfig{Model: "gpt-5", Provider: &configv1.ADKModelConfig_Openai{}},
			schema: multiType,
		},
		{
			name:   "Azure OpenAI with a multi-type list",
			model:  &configv1.ADKModelConfig{Model: "gpt-5", Provider: &configv1.ADKModelConfig_AzureOpenai{}},
			schema: multiType,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{Model: tt.model}
			err := applyOutputSchema(config, &translator.ResolvedOutputSchema{Schema: tt.schema})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				var validation *translator.ValidationError
				require.ErrorAs(t, err, &validation)
				require.Nil(t, config.Output)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, string(tt.schema), string(config.Output.JsonSchema))
		})
	}
}
