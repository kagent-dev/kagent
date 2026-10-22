package outputschema

import (
	"testing"

	"github.com/stretchr/testify/require"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestCheckSetModelResponse(t *testing.T) {
	const oneType = "must have exactly one type"
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{
			name:   "nullable string, array of objects and nested object",
			schema: `{"type":"object","properties":{"note":{"type":["string","null"]},"stops":{"type":"array","items":{"type":"object","properties":{"code":{"type":"integer"}}}},"due":{"$ref":"#/$defs/due"}},"$defs":{"due":{"type":"object","properties":{"day":{"type":"string","format":"date"}}}}}`,
		},
		{
			name:    "type list with two non-null types",
			schema:  `{"type":"object","properties":{"code":{"type":["string","integer"]}}}`,
			wantErr: "/properties/code " + oneType,
		},
		{
			name:    "anyOf",
			schema:  `{"type":"object","properties":{"code":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`,
			wantErr: "/properties/code " + oneType,
		},
		{
			name:    "type null",
			schema:  `{"type":"object","properties":{"gone":{"type":"null"}}}`,
			wantErr: "/properties/gone " + oneType,
		},
		{
			name:    "no type",
			schema:  `{"type":"object","properties":{"any":{}}}`,
			wantErr: "/properties/any " + oneType,
		},
		{
			name:    "enum without a type",
			schema:  `{"type":"object","properties":{"level":{"enum":["low","high"]}}}`,
			wantErr: `/properties/level must have exactly one type, such as "string" or ["string", "null"] (Go ADK's set_model_response check rejects a node with no type`,
		},
		{
			name:    "array without items",
			schema:  `{"type":"object","properties":{"tags":{"type":"array"}}}`,
			wantErr: "/properties/tags must set items",
		},
		{
			name:    "nested object property",
			schema:  `{"type":"object","properties":{"due":{"type":"object","properties":{"day":{"type":["string","integer"]}}}}}`,
			wantErr: "/properties/due/properties/day " + oneType,
		},
		{
			name:    "array items",
			schema:  `{"type":"object","properties":{"stops":{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}}`,
			wantErr: "/properties/stops/items " + oneType,
		},
		{
			name:    "first failure in sorted order",
			schema:  `{"type":"object","properties":{"b":{"type":"null"},"a":{"type":"null"}}}`,
			wantErr: "/properties/a " + oneType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projected, err := ToGenAISchema([]byte(tt.schema))
			require.NoError(t, err)
			err = CheckSetModelResponse(projected)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestUsesSetModelResponse(t *testing.T) {
	tests := []struct {
		name  string
		model *configv1.ADKModelConfig
		want  bool
	}{
		{name: "Gemini API", model: &configv1.ADKModelConfig{Model: "gemini-3.5-flash-lite", Provider: &configv1.ADKModelConfig_Gemini{}}, want: true},
		{name: "Gemini API with models prefix", model: &configv1.ADKModelConfig{Model: "models/Gemini-2.5-pro", Provider: &configv1.ADKModelConfig_Gemini{}}, want: true},
		{name: "Gemini name through OpenAI", model: &configv1.ADKModelConfig{Model: "gemini-2.5-flash", Provider: &configv1.ADKModelConfig_Openai{}}, want: true},
		{name: "Gemini API with the default model", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{}}, want: true},
		{name: "Vertex AI", model: &configv1.ADKModelConfig{Model: "gemini-3.5-flash", Provider: &configv1.ADKModelConfig_GeminiVertexAi{}}},
		{name: "Gemma on the Gemini API", model: &configv1.ADKModelConfig{Model: "gemma-3-27b-it", Provider: &configv1.ADKModelConfig_Gemini{}}},
		{name: "OpenAI", model: &configv1.ADKModelConfig{Model: "gpt-5", Provider: &configv1.ADKModelConfig_Openai{}}},
		{name: "no model", model: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, UsesSetModelResponse(tt.model))
		})
	}
}
