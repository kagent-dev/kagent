package translator

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOutputSchemaRejectsLargePatternsBeforeCompiling(t *testing.T) {
	properties := make([]string, 5)
	for i := range properties {
		properties[i] = fmt.Sprintf(`"p%d":{"type":"string","pattern":"^%s$"}`, i, strings.Repeat(`[a-z]{1000}`, 1000))
	}
	raw := []byte(`{"type":"object","properties":{` + strings.Join(properties, ",") + `}}`)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := validateOutputSchema(raw)
	runtime.ReadMemStats(&after)

	require.ErrorContains(t, err, "regexp instructions")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<20))
}

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
			name:   "standard schema identity keywords",
			schema: `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://example.com/result.schema.json","type":"object"}`,
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
		{
			name:   "string format and pattern",
			schema: `{"type":"object","properties":{"due":{"type":"string","format":"date-time"},"day":{"anyOf":[{"$ref":"#/$defs/day"},{"type":"null"}]}},"$defs":{"day":{"type":"string","format":"date","pattern":"^2026-"}}}`,
		},
		{name: "nullable string with format", schema: `{"type":"object","properties":{"due":{"type":["string","null"],"format":"date-time"}}}`},
		{name: "nullable nested object", schema: `{"type":"object","properties":{"meta":{"type":["object","null"],"properties":{"id":{"type":"string"}}}}}`},
		{name: "nullable string with pattern", schema: `{"type":"object","properties":{"ticket":{"type":["string","null"],"pattern":"^OPS-[0-9]+$"}}}`},
		{name: "misspelled type name in array", schema: `{"type":"object","properties":{"x":{"type":["strng"]}}}`, wantErr: "enum: strng does not equal any of"},
		{name: "empty type array", schema: `{"type":"object","properties":{"x":{"type":[]}}}`, wantErr: "minItems"},
		{name: "duplicate type entries", schema: `{"type":"object","properties":{"x":{"type":["string","string"]}}}`, wantErr: "uniqueItems"},
		{name: "unknown type name in array", schema: `{"type":"object","properties":{"x":{"type":["string","date"]}}}`, wantErr: "enum: date does not equal any of"},
		{name: "format on nullable non-string", schema: `{"type":"object","properties":{"count":{"type":["integer","null"],"format":"date"}}}`, wantErr: "contains: [integer null] does not have an item matching"},
		{name: "root type array", schema: `{"type":["object","null"]}`, wantErr: "does not equal object"},
		{name: "unsupported format", schema: `{"type":"object","properties":{"site":{"type":"string","format":"uri"}}}`, wantErr: `format: enum: uri does not equal any of: [date date-time duration email hostname ipv4 ipv6 time uuid]`},
		{name: "format on non-string", schema: `{"type":"object","properties":{"count":{"type":"integer","format":"date"}}}`, wantErr: "does not equal string"},
		{name: "pattern without type", schema: `{"type":"object","properties":{"code":{"pattern":"^[A-Z]+$"}}}`, wantErr: `missing properties: ["type"]`},
		{name: "pattern not RE2", schema: `{"type":"object","properties":{"code":{"type":"string","pattern":"^(?=A)"}}}`, wantErr: "error parsing regexp"},
		{name: "pattern with word boundary", schema: `{"type":"object","properties":{"code":{"type":"string","pattern":"\\b[A-Z]{3}\\b"}}}`, wantErr: "uses a word boundary"},
		{name: "pattern with non-word boundary in $defs", schema: `{"type":"object","$defs":{"code":{"type":"string","pattern":"^A\\B"}},"properties":{"code":{"$ref":"#/$defs/code"}}}`, wantErr: "uses a word boundary"},
		{name: "pattern with escaped backslash before b", schema: `{"type":"object","properties":{"path":{"type":"string","pattern":"^C:\\\\bin$"}}}`},
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
