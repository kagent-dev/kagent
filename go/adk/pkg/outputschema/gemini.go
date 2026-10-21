package outputschema

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"google.golang.org/genai"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
)

// setModelResponseTypes lists the genai types Go ADK's set_model_response tool
// can check a value against. Any other type, including an unset one, makes
// every call to the tool fail.
var setModelResponseTypes = map[genai.Type]bool{
	genai.TypeString:  true,
	genai.TypeInteger: true,
	genai.TypeNumber:  true,
	genai.TypeBoolean: true,
	genai.TypeArray:   true,
	genai.TypeObject:  true,
}

// UsesSetModelResponse reports whether Go ADK checks the agent's answer with
// its set_model_response tool. ADK does so for every model whose name starts
// with "gemini-" unless the model runs on Vertex AI, where Gemini 2.0 and later
// take the schema natively. kagent agents always have tools, which is the
// other condition ADK requires.
func UsesSetModelResponse(model *configv1.ADKModelConfig) bool {
	if _, vertex := model.GetProvider().(*configv1.ADKModelConfig_GeminiVertexAi); vertex {
		return false
	}
	name := model.GetModel()
	name = name[strings.LastIndex(name, "/")+1:]
	return strings.HasPrefix(strings.ToLower(name), "gemini-")
}

// CheckSetModelResponse reports the first property of a ToGenAISchema
// projection that Go ADK's set_model_response tool cannot check. The tool
// checks each answer property against exactly one type, so a type list with
// two or more non-null types, anyOf and type null all fail every answer.
func CheckSetModelResponse(schema *genai.Schema) error {
	return checkSetModelResponseProperties(schema, "")
}

func checkSetModelResponseProperties(schema *genai.Schema, path string) error {
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		if err := checkSetModelResponse(schema.Properties[name], path+"/properties/"+name); err != nil {
			return err
		}
	}
	return nil
}

func checkSetModelResponse(schema *genai.Schema, path string) error {
	if !setModelResponseTypes[schema.Type] {
		return fmt.Errorf("output schema for Gemini: %s must have exactly one type (Go ADK's set_model_response check does not support type lists with more than one non-null type, anyOf, or type null)", path)
	}
	switch schema.Type {
	case genai.TypeArray:
		if schema.Items == nil {
			return fmt.Errorf("output schema for Gemini: %s must set items (Go ADK's set_model_response check rejects an array without items)", path)
		}
		return checkSetModelResponse(schema.Items, path+"/items")
	case genai.TypeObject:
		return checkSetModelResponseProperties(schema, path)
	}
	return nil
}
