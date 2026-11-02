package outputschema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckStrict(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr string
	}{
		{
			name:   "closed object with every property required",
			schema: `{"type":"object","properties":{"due":{"type":"string"}},"required":["due"],"additionalProperties":false}`,
		},
		{
			name: "nested objects in properties, items, anyOf and $defs",
			schema: `{
				"type":"object",
				"properties":{
					"stops":{"type":"array","items":{"$ref":"#/$defs/stop"}},
					"note":{"anyOf":[{"type":"string"},{"type":"null"}]}
				},
				"required":["stops","note"],
				"additionalProperties":false,
				"$defs":{"stop":{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}}
			}`,
		},
		{
			name:    "root object left open",
			schema:  `{"type":"object","properties":{"due":{"type":"string"}},"required":["due"]}`,
			wantErr: "the root object must set additionalProperties to false",
		},
		{
			name:    "optional property",
			schema:  `{"type":"object","properties":{"due":{"type":"string"},"code":{"type":"string"}},"required":["due"],"additionalProperties":false}`,
			wantErr: `the root object must list property "code" in required`,
		},
		{
			name: "open object inside an array",
			schema: `{
				"type":"object",
				"properties":{"stops":{"type":"array","items":{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}}},
				"required":["stops"],
				"additionalProperties":false
			}`,
			wantErr: "/properties/stops/items must set additionalProperties to false",
		},
		{
			name: "open object in $defs",
			schema: `{
				"type":"object","properties":{},"additionalProperties":false,
				"$defs":{"stop":{"type":"object","additionalProperties":true}}
			}`,
			wantErr: "/$defs/stop must set additionalProperties to false",
		},
		{
			name: "untyped map of strings",
			schema: `{
				"type":"object",
				"properties":{"labels":{"additionalProperties":{"type":"string"}}},
				"required":["labels"],
				"additionalProperties":false
			}`,
			wantErr: "/properties/labels must set type",
		},
		{
			name: "untyped closed object",
			schema: `{
				"type":"object",
				"properties":{"meta":{"properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}},
				"required":["meta"],
				"additionalProperties":false
			}`,
			wantErr: "/properties/meta must set type",
		},
		{
			name:    "node with only a description",
			schema:  `{"type":"object","properties":{"note":{"description":"any note"}},"required":["note"],"additionalProperties":false}`,
			wantErr: "/properties/note must set type",
		},
		{
			name:    "untyped const",
			schema:  `{"type":"object","properties":{"owner":{"const":"ops"}},"required":["owner"],"additionalProperties":false}`,
			wantErr: "/properties/owner must set type",
		},
		{
			name:   "untyped enum and anyOf",
			schema: `{"type":"object","properties":{"level":{"enum":["a","b"]},"note":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["level","note"],"additionalProperties":false}`,
		},
		{
			name:   "nullable closed object and nullable string",
			schema: `{"type":"object","properties":{"meta":{"type":["object","null"],"properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false},"due":{"type":["string","null"]}},"required":["meta","due"],"additionalProperties":false}`,
		},
		{
			name:   "nullable string satisfies must set type",
			schema: `{"type":"object","properties":{"note":{"type":["string","null"]}},"required":["note"],"additionalProperties":false}`,
		},
		{
			name:    "nullable object left open",
			schema:  `{"type":"object","properties":{"meta":{"type":["object","null"]}},"required":["meta"],"additionalProperties":false}`,
			wantErr: "/properties/meta must set additionalProperties to false",
		},
		{
			name: "typed map of strings",
			schema: `{
				"type":"object",
				"properties":{"labels":{"type":"object","additionalProperties":{"type":"string"}}},
				"required":["labels"],
				"additionalProperties":false
			}`,
			wantErr: "/properties/labels must set additionalProperties to false: strict mode cannot express a map with free-form keys",
		},
		{
			name:    "$ref with a description",
			schema:  `{"type":"object","properties":{"a":{"$ref":"#/$defs/s","description":"a code"}},"required":["a"],"additionalProperties":false,"$defs":{"s":{"type":"string"}}}`,
			wantErr: "/properties/a must not set other keywords next to $ref",
		},
		{
			name:    "$ref with a title inside anyOf",
			schema:  `{"type":"object","properties":{"a":{"anyOf":[{"$ref":"#/$defs/s","title":"Code"},{"type":"null"}]}},"required":["a"],"additionalProperties":false,"$defs":{"s":{"type":"string"}}}`,
			wantErr: "/properties/a/anyOf/0 must not set other keywords next to $ref",
		},
		{
			name:    "root anyOf",
			schema:  `{"type":"object","anyOf":[{"type":"object","properties":{},"additionalProperties":false}]}`,
			wantErr: "the root must not use anyOf",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckStrict(json.RawMessage(tt.schema))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
