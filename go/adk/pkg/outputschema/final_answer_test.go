package outputschema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

func TestUsesFinalAnswerTool(t *testing.T) {
	openai := func(model string) *configv1.ADKModelConfig {
		return &configv1.ADKModelConfig{Model: model, Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}}}
	}
	tests := []struct {
		name  string
		model *configv1.ADKModelConfig
		want  bool
	}{
		{name: "Given an OpenAI-format provider, assert that the schema goes through the final answer tool", model: openai("deepseek-v4-flash"), want: true},
		{name: "Given an Azure OpenAI provider, assert that the schema goes through the final answer tool", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_AzureOpenai{AzureOpenai: &configv1.ADKAzureOpenAI{}}}, want: true},
		{name: "Given Foundry with its OpenAI format, assert that the schema goes through the final answer tool", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{}}}, want: true},
		{name: "Given Foundry with the Anthropic format, assert that the Anthropic adapter keeps its response format", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{ApiFormat: runtimeconfig.FoundryAPIFormatAnthropic}}}, want: false},
		{name: "Given an OpenAI-format provider with a Gemini model name, assert that ADK supplies the tool instead", model: openai("gemini-2.5-flash"), want: false},
		{name: "Given Anthropic, assert that it keeps its response format", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Anthropic{Anthropic: &configv1.ADKAnthropic{}}}, want: false},
		{name: "Given Bedrock, assert that it keeps its response format", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Bedrock{Bedrock: &configv1.ADKBedrock{}}}, want: false},
		{name: "Given Gemini, assert that ADK owns the choice", model: &configv1.ADKModelConfig{Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{}}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, UsesFinalAnswerTool(test.model))
		})
	}
}

// TestNewFinalAnswerToolKeepsTheCanonicalSchema checks that the tool's
// parameters are the schema as written. ADK's typed projection would reject
// or weaken keywords such as anyOf, const and $defs.
func TestNewFinalAnswerToolKeepsTheCanonicalSchema(t *testing.T) {
	const schema = `{"type":"object","additionalProperties":false,"required":["code","note"],"properties":{"code":{"anyOf":[{"type":"string"},{"type":"integer"}]},"note":{"type":["string","null"]},"kind":{"const":"report"},"due":{"$ref":"#/$defs/due"}},"$defs":{"due":{"type":"object","properties":{"day":{"type":"string"}}}}}`
	t.Run("Given a schema using anyOf, const, a type list and $defs, when the tool is built, assert that its declaration carries the schema unchanged", func(t *testing.T) {
		answerTool, err := NewFinalAnswerTool([]byte(schema))
		require.NoError(t, err)

		declared, ok := answerTool.(interface {
			Declaration() *genai.FunctionDeclaration
		})
		require.True(t, ok)
		declaration := declared.Declaration()
		require.Equal(t, FinalAnswerToolName, declaration.Name)
		encoded, err := json.Marshal(declaration.ParametersJsonSchema)
		require.NoError(t, err)
		require.JSONEq(t, schema, string(encoded))
	})
	t.Run("Given text that is not JSON, when the tool is built, assert that it fails", func(t *testing.T) {
		_, err := NewFinalAnswerTool([]byte(`not json`))
		require.ErrorContains(t, err, "decode output schema")
	})
}
