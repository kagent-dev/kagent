package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// structuredOutputName identifies kagent's response format to providers. It is
// an API request label, not a field in the generated result.
const structuredOutputName = "kagent_output"

// structuredOutputSchema returns the JSON Schema that a provider request
// should send.
//
// Kagent places the canonical schema in ResponseJsonSchema for adapters that
// support raw JSON Schema, so prefer that value: it preserves keywords such as
// $defs, const, and additionalProperties that genai.Schema cannot represent.
// The ResponseSchema fallback also makes this helper safe for callers using
// ADK's typed schema directly.
func structuredOutputSchema(config *genai.GenerateContentConfig) (map[string]any, error) {
	if config == nil || (config.ResponseJsonSchema == nil && config.ResponseSchema == nil) {
		return nil, nil
	}
	if config.ResponseJsonSchema != nil {
		schema, ok := config.ResponseJsonSchema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("response JSON schema has type %T, want map[string]any", config.ResponseJsonSchema)
		}
		return schema, nil
	}

	encoded, err := json.Marshal(config.ResponseSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal response schema: %w", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("decode response schema: %w", err)
	}
	normalizeSchemaTypes(schema)
	return schema, nil
}

// normalizeSchemaTypes converts genai.Type's JSON values (for example,
// "OBJECT") to JSON Schema spelling ("object"). It is used only after
// marshaling a typed genai.Schema; raw JSON schemas must not be normalized,
// because an object-valued const or enum may legitimately contain a field
// named "type" whose value is case-sensitive user data.
func normalizeSchemaTypes(value any) {
	switch value := value.(type) {
	case map[string]any:
		if typeName, ok := value["type"].(string); ok {
			value["type"] = strings.ToLower(typeName)
		}
		for _, child := range value {
			normalizeSchemaTypes(child)
		}
	case []any:
		for _, child := range value {
			normalizeSchemaTypes(child)
		}
	}
}
