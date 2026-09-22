package outputschema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestToGenAISchema(t *testing.T) {
	schema, err := ToGenAISchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"status":{"$ref":"#/$defs/status"},
			"payload":{"type":"array","items":{"type":"integer"}}
		},
		"required":["status"],
		"$defs":{"status":{"type":"string","const":"success"}}
	}`))
	require.NoError(t, err)
	require.Equal(t, genai.TypeObject, schema.Type)
	require.Equal(t, []string{"status"}, schema.Required)
	require.Equal(t, []string{"success"}, schema.Properties["status"].Enum)
	require.Equal(t, genai.TypeInteger, schema.Properties["payload"].Items.Type)
}

func TestToGenAISchemaRejectsRecursiveReference(t *testing.T) {
	_, err := ToGenAISchema(json.RawMessage(`{
		"type":"object",
		"$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},
		"properties":{"node":{"$ref":"#/$defs/node"}}
	}`))
	require.ErrorContains(t, err, "recursive output schema")
}

func TestToGenAISchemaBoundsReferenceExpansion(t *testing.T) {
	leafProperties := make(map[string]any, 30)
	for i := range 30 {
		leafProperties[string(rune('a'+i))] = map[string]any{"type": "string"}
	}
	rootProperties := make(map[string]any, 40)
	for i := range 40 {
		rootProperties[string(rune('A'+i))] = map[string]any{"$ref": "#/$defs/leaf"}
	}
	raw, err := json.Marshal(map[string]any{
		"type":       "object",
		"properties": rootProperties,
		"$defs": map[string]any{
			"leaf": map[string]any{"type": "object", "properties": leafProperties},
		},
	})
	require.NoError(t, err)

	_, err = ToGenAISchema(raw)
	require.ErrorContains(t, err, "conversion exceeds maximum node count")
}
