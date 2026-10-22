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

func TestToGenAISchemaDoesNotNarrowUnsupportedConstraints(t *testing.T) {
	schema, err := ToGenAISchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"mixed":{"enum":["a",1]},
			"numeric":{"type":"integer","const":1}
		},
		"additionalProperties":false
	}`))
	require.NoError(t, err)
	require.Empty(t, schema.Properties["mixed"].Enum)
	require.Empty(t, schema.Properties["numeric"].Enum)
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

func TestToGenAISchemaKeepsPatternAndSupportedFormats(t *testing.T) {
	for _, format := range SupportedStringFormats() {
		t.Run(format, func(t *testing.T) {
			schema, err := ToGenAISchema(json.RawMessage(`{"type":"object","properties":{"value":{"type":"string","format":"` + format + `","pattern":"^."}}}`))
			require.NoError(t, err)
			require.Equal(t, format, schema.Properties["value"].Format)
			require.Equal(t, "^.", schema.Properties["value"].Pattern)
		})
	}
	schema, err := ToGenAISchema(json.RawMessage(`{"type":"object","properties":{"site":{"type":"string","format":"uri"}}}`))
	require.NoError(t, err)
	require.Empty(t, schema.Properties["site"].Format)
}

func TestToGenAISchemaProjectsTypeArrays(t *testing.T) {
	schema, err := ToGenAISchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"due":{"type":["string","null"],"format":"date-time"},
			"none":{"type":["null"]},
			"either":{"type":["string","integer"]},
			"eitherOrNull":{"type":["string","integer","null"]},
			"plain":{"type":"string"}
		}
	}`))
	require.NoError(t, err)

	due := schema.Properties["due"]
	require.Equal(t, genai.TypeString, due.Type)
	require.Equal(t, new(true), due.Nullable)
	require.Equal(t, "date-time", due.Format)

	require.Equal(t, genai.TypeNULL, schema.Properties["none"].Type)
	require.Nil(t, schema.Properties["none"].Nullable)

	either := schema.Properties["either"]
	require.Empty(t, either.Type, "genai.Schema holds one type; the final validator still enforces both")
	require.Nil(t, either.Nullable)

	eitherOrNull := schema.Properties["eitherOrNull"]
	require.Empty(t, eitherOrNull.Type)
	require.Equal(t, new(true), eitherOrNull.Nullable)

	require.Equal(t, genai.TypeString, schema.Properties["plain"].Type)
	require.Nil(t, schema.Properties["plain"].Nullable)
}

func TestToGenAISchemaProjectsNestedTypeArrays(t *testing.T) {
	schema, err := ToGenAISchema(json.RawMessage(`{
		"type":"object",
		"properties":{
			"null":{"type":"null"},
			"tags":{"type":"array","items":{"type":["string","null"]}},
			"meta":{"type":["object","null"],"properties":{"id":{"type":["integer","null"]}}},
			"due":{"$ref":"#/$defs/instant"}
		},
		"$defs":{"instant":{"type":["string","null"],"format":"date-time"}}
	}`))
	require.NoError(t, err)
	require.Equal(t, genai.TypeNULL, schema.Properties["null"].Type)
	require.Nil(t, schema.Properties["null"].Nullable)

	items := schema.Properties["tags"].Items
	require.Equal(t, genai.TypeString, items.Type)
	require.Equal(t, new(true), items.Nullable)

	meta := schema.Properties["meta"]
	require.Equal(t, genai.TypeObject, meta.Type)
	require.Equal(t, new(true), meta.Nullable)
	require.Equal(t, genai.TypeInteger, meta.Properties["id"].Type)
	require.Equal(t, new(true), meta.Properties["id"].Nullable)

	due := schema.Properties["due"]
	require.Equal(t, genai.TypeString, due.Type)
	require.Equal(t, new(true), due.Nullable)
	require.Equal(t, "date-time", due.Format)
}

func TestToGenAISchemaRejectsUnknownTypeName(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"x":{"type":"strng"}}}`,
		`{"type":"object","properties":{"x":{"type":["strng","null"]}}}`,
	} {
		_, err := ToGenAISchema(json.RawMessage(schema))
		require.ErrorContains(t, err, `unsupported output schema type "strng"`, schema)
	}
}
