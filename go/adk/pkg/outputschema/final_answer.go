package outputschema

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// FinalAnswerToolName is the name Go ADK recognises as a structured final
// answer: when the model calls a tool with this name, ADK ends the run and
// publishes the tool's arguments as the agent's final response. kagent's
// tool must keep this name so ADK treats a call to it that way.
const FinalAnswerToolName = "set_model_response"

// FinalAnswerInstruction tells the model how to finish. It is appended to the
// agent instruction when the final answer tool is offered.
const FinalAnswerInstruction = "\n\nWhen you have everything you need, give your final response by calling the " +
	FinalAnswerToolName + " tool with your answer. Use the other tools first if you need them, and do not write the " +
	"final response as plain text."

// UsesFinalAnswerTool reports whether kagent offers the output schema to the
// model as a final answer tool instead of a request-level response format.
//
// A response format applies to every request, including the ones that carry
// tools. Providers that enforce it by constraining generation to the schema
// can then only return the final JSON and never a tool call, so an agent with
// tools never uses them. OpenAI-format providers cover many such backends, so
// they get the schema through a tool the model calls to finish.
//
// A model for which ADK adds its own final answer tool is excluded: offering a
// second tool with the same name would be rejected.
func UsesFinalAnswerTool(model *configv1.ADKModelConfig) bool {
	switch p := model.GetProvider().(type) {
	case *configv1.ADKModelConfig_Openai, *configv1.ADKModelConfig_AzureOpenai:
		return !UsesSetModelResponse(model)
	case *configv1.ADKModelConfig_Foundry:
		// Claude on Foundry uses the Anthropic Messages API, not an OpenAI format.
		return p.Foundry.GetApiFormat() != runtimeconfig.FoundryAPIFormatAnthropic && !UsesSetModelResponse(model)
	default:
		return false
	}
}

// NewFinalAnswerTool returns a tool whose parameters are the canonical output
// schema and whose result is its arguments, unchanged. The schema is used as
// written rather than projected onto ADK's typed schema, which cannot express
// every schema the portable profile accepts, such as anyOf. The tool rejects
// arguments that do not match the schema, but ADK still ends the run on that
// call, with an error object as the final response. The executor then fails
// the Task because that response does not match the schema, so the model gets
// no second attempt.
func NewFinalAnswerTool(rawSchema json.RawMessage) (tool.Tool, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	return functiontool.New(functiontool.Config{
		Name:        FinalAnswerToolName,
		Description: "Submit your final response. Call this once, with an answer that matches the required output schema, instead of writing the answer as text.",
		InputSchema: &schema,
	}, func(_ agent.Context, answer map[string]any) (map[string]any, error) {
		return answer, nil
	})
}
