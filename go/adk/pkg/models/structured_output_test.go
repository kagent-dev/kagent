package models

import (
	"testing"

	"google.golang.org/genai"
)

func TestStructuredOutputSchemaPrefersRawJSONSchema(t *testing.T) {
	largeInteger := ^uint64(0)
	schema, err := structuredOutputSchema(&genai.GenerateContentConfig{
		ResponseSchema: &genai.Schema{Type: genai.TypeObject},
		ResponseJsonSchema: map[string]any{
			"type": "object", "additionalProperties": false, "x-large-integer": largeInteger,
			"properties": map[string]any{"result": map[string]any{"const": map[string]any{"type": "MixedCase"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("schema = %#v", schema)
	}
	if schema["x-large-integer"] != largeInteger {
		t.Fatalf("raw schema value was coerced: %#v", schema["x-large-integer"])
	}
	constant := schema["properties"].(map[string]any)["result"].(map[string]any)["const"].(map[string]any)
	if constant["type"] != "MixedCase" {
		t.Fatalf("const value was modified: %#v", constant)
	}
}

func TestStructuredOutputSchemaRejectsNonObjectRawSchema(t *testing.T) {
	_, err := structuredOutputSchema(&genai.GenerateContentConfig{ResponseJsonSchema: "not an object"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestStructuredOutputSchemaNormalizesGenAITypeNames(t *testing.T) {
	schema, err := structuredOutputSchema(&genai.GenerateContentConfig{ResponseSchema: &genai.Schema{
		Type:       genai.TypeObject,
		Properties: map[string]*genai.Schema{"answer": {Type: genai.TypeInteger}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	property := schema["properties"].(map[string]any)["answer"].(map[string]any)
	if schema["type"] != "object" || property["type"] != "integer" {
		t.Fatalf("schema = %#v", schema)
	}
}
