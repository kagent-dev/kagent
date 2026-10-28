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
// can check a value against. For any other type, including an unset one, the
// tool rejects every non-null value.
var setModelResponseTypes = map[genai.Type]bool{
	genai.TypeString:  true,
	genai.TypeInteger: true,
	genai.TypeNumber:  true,
	genai.TypeBoolean: true,
	genai.TypeArray:   true,
	genai.TypeObject:  true,
}

// UsesSetModelResponse reports whether compilation checks the schema against
// Go ADK's set_model_response tool: for a model whose name starts with
// "gemini-", and for Gemini with no model name. GeminiVertexAI models are not
// checked.
func UsesSetModelResponse(model *configv1.ADKModelConfig) bool {
	name := model.GetModel()
	switch model.GetProvider().(type) {
	case *configv1.ADKModelConfig_GeminiVertexAi:
		return false
	case *configv1.ADKModelConfig_Gemini:
		if name == "" {
			return true
		}
	}
	name = name[strings.LastIndex(name, "/")+1:]
	return strings.HasPrefix(strings.ToLower(name), "gemini-")
}

// CheckSetModelResponse reports the first property of a ToGenAISchema
// projection that Go ADK's set_model_response tool cannot check. The tool
// checks each answer property against exactly one type, so it rejects every
// non-null value of a property that uses a type list with two or more non-null
// types, and every value, null included, of a property that uses anyOf or type
// null.
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
		return fmt.Errorf(`output schema for Gemini: %s must have exactly one type, such as "string" or ["string", "null"] (Go ADK's set_model_response check rejects a node with no type, a type list with more than one non-null type, anyOf, and type null)`, path)
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
