package adkconfig

import (
	"testing"

	"github.com/stretchr/testify/require"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
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
		strict  bool
		wantErr string
	}{
		{name: "false with open object", schema: openSchema},
		{name: "true with strict schema", schema: strictSchema, strict: true},
		{name: "true with open object", schema: openSchema, strict: true, wantErr: "outputSchemaStrict: the root object must set additionalProperties to false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{Model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}}}
			err := applyOutputSchema(config, &translator.ResolvedOutputSchema{Schema: tt.schema, Strict: tt.strict})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, config.Output)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.strict, config.Model.GetOpenai().StrictOutputSchema)
		})
	}
}

func TestApplyOutputSchemaSetsStrictOnProvider(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"due":{"type":"string"}},"required":["due"],"additionalProperties":false}`)
	for _, tt := range []struct {
		name   string
		model  *configv1.ADKModelConfig
		strict func(*configv1.ADKModelConfig) bool
		want   bool
	}{
		{
			name:   "OpenAI",
			model:  &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}},
			strict: func(m *configv1.ADKModelConfig) bool { return m.GetOpenai().StrictOutputSchema },
			want:   true,
		},
		{
			name:   "Azure OpenAI",
			model:  &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_AzureOpenai{AzureOpenai: &configv1.ADKAzureOpenAI{}}},
			strict: func(m *configv1.ADKModelConfig) bool { return m.GetAzureOpenai().StrictOutputSchema },
			want:   true,
		},
		{
			name:   "Foundry",
			model:  &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{ApiFormat: runtimeconfig.FoundryAPIFormatOpenAI}}},
			strict: func(m *configv1.ADKModelConfig) bool { return m.GetFoundry().StrictOutputSchema },
			want:   true,
		},
		{
			name:   "Foundry in the anthropic format",
			model:  &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{ApiFormat: runtimeconfig.FoundryAPIFormatAnthropic}}},
			strict: func(m *configv1.ADKModelConfig) bool { return m.GetFoundry().StrictOutputSchema },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &configv1.ADKAgentConfig{Model: tt.model}
			require.NoError(t, applyOutputSchema(config, &translator.ResolvedOutputSchema{Schema: schema, Strict: true}))
			require.Equal(t, tt.want, tt.strict(config.Model))
		})
	}
}

func TestApplyOutputSchemaChecksStrictForFoundryAnthropic(t *testing.T) {
	config := &configv1.ADKAgentConfig{Model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{ApiFormat: runtimeconfig.FoundryAPIFormatAnthropic}}}}
	err := applyOutputSchema(config, &translator.ResolvedOutputSchema{Schema: []byte(`{"type":"object","properties":{"due":{"type":"string"}}}`), Strict: true})
	require.ErrorContains(t, err, "outputSchemaStrict: the root object must set additionalProperties to false")
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
			model:  &configv1.ADKModelConfig{Model: "gpt-5", Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}},
			schema: multiType,
		},
		{
			name:   "Azure OpenAI with a multi-type list",
			model:  &configv1.ADKModelConfig{Model: "gpt-5", Provider: &configv1.ADKModelConfig_AzureOpenai{AzureOpenai: &configv1.ADKAzureOpenAI{}}},
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
