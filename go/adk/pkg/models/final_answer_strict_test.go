package models

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
)

func TestStrictOutputSchemaIsSentOnTheFinalAnswerTool(t *testing.T) {
	tools := []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{
		{Name: "get_weather", ParametersJsonSchema: map[string]any{"type": "object"}},
		{Name: outputschema.FinalAnswerToolName, ParametersJsonSchema: map[string]any{"type": "object"}},
	}}}
	for _, strict := range []bool{true, false} {
		t.Run("Given the output schema's strict setting, when chat tools are marked, assert that only the final answer tool carries it", func(t *testing.T) {
			chatTools := genaiToolsToOpenAITools(tools)
			markChatFinalAnswerStrict(chatTools, strict)

			require.False(t, chatTools[0].OfFunction.Function.Strict.Valid(), "an ordinary tool must not be made strict")
			require.Equal(t, strict, chatTools[1].OfFunction.Function.Strict.Value)
		})
		t.Run("Given the output schema's strict setting, when Responses tools are marked, assert that the final answer tool carries it", func(t *testing.T) {
			responsesTools := genaiToolsToResponsesTools(tools)
			markResponsesFinalAnswerStrict(responsesTools, strict)

			require.False(t, responsesTools[0].OfFunction.Strict.Value, "an ordinary tool must stay non-strict")
			require.Equal(t, strict, responsesTools[1].OfFunction.Strict.Value)
		})
	}
}
