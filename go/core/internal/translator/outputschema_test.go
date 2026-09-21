package translator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOutputSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{
			name: "portable object schema",
			schema: `{
				"type":"object",
				"properties":{"status":{"$ref":"#/$defs/status"},"payload":{"type":"object","additionalProperties":false}},
				"required":["status","payload"],
				"additionalProperties":false,
				"$defs":{"status":{"type":"string","enum":["success","error"]}}
			}`,
		},
		{
			name: "all portable schema forms",
			schema: `{
				"type":"object",
				"title":"Result",
				"description":"A structured result",
				"properties":{
					"value":{"anyOf":[{"$ref":"#/$defs/value"},{"type":"null","const":null}]},
					"labels":{"type":"array","items":{"type":"string","enum":["one","two"]}},
					"metadata":{"type":"object","additionalProperties":{"type":"string"}}
				},
				"required":["value","labels"],
				"additionalProperties":false,
				"$defs":{"value":{"type":"integer","enum":[1,2]}}
			}`,
		},
		{name: "root is not object", schema: `{"type":"array","items":{"type":"string"}}`, wantErr: "does not equal object"},
		{name: "unknown keyword", schema: `{"type":"object","oneOf":[]}`, wantErr: "unexpected additional properties"},
		{name: "external ref", schema: `{"type":"object","properties":{"x":{"$ref":"https://example.com/schema"}}}`, wantErr: "does not match regular expression"},
		{name: "missing ref", schema: `{"type":"object","properties":{"x":{"$ref":"#/$defs/missing"}}}`, wantErr: `no key "missing"`},
		{name: "duplicate required", schema: `{"type":"object","required":["x","x"]}`, wantErr: "uniqueItems"},
		{name: "recursive ref is valid before ADK projection", schema: `{"type":"object","$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},"properties":{"node":{"$ref":"#/$defs/node"}}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canonical, err := validateOutputSchema([]byte(test.schema))
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, test.schema, string(canonical))
		})
	}
}

func TestValidateOutputSchemaBoundsInputSize(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"type":        "object",
		"description": string(make([]byte, maxOutputSchemaBytes)),
	})
	require.NoError(t, err)

	_, err = validateOutputSchema(raw)
	require.ErrorContains(t, err, "schema exceeds")
}
