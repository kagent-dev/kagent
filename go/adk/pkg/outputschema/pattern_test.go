package outputschema

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

var childSchemas = []struct {
	name   string
	schema func(node string) string
}{
	{
		name:   "properties",
		schema: func(node string) string { return `{"type":"object","properties":{"v":` + node + `}}` },
	},
	{
		name: "items",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"type":"array","items":` + node + `}}}`
		},
	},
	{
		name: "additionalProperties",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"type":"object","additionalProperties":` + node + `}}}`
		},
	},
	{
		name: "anyOf",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"anyOf":[` + node + `,{"type":"null"}]}}}`
		},
	},
	{
		name: "$defs",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"$ref":"#/$defs/v"}},"$defs":{"v":` + node + `}}`
		},
	},
}

func TestCheckPatternsRejectsWordBoundaryInEveryChildSchema(t *testing.T) {
	for _, child := range childSchemas {
		t.Run(child.name, func(t *testing.T) {
			var schema jsonschema.Schema
			require.NoError(t, json.Unmarshal([]byte(child.schema(`{"type":"string","pattern":"\\bA"}`)), &schema))
			require.ErrorContains(t, CheckPatterns(&schema), "uses a word boundary")
		})
	}
}

func TestCheckPatternsReportsTheFirstPatternInSortedOrder(t *testing.T) {
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal([]byte(`{"type":"object","properties":{
		"d":{"type":"string","pattern":"\\bD"},
		"c":{"type":"string","pattern":"\\bC"},
		"b":{"type":"string","pattern":"\\bB"},
		"a":{"type":"string","pattern":"\\bA"}
	}}`), &schema))
	for range 50 {
		require.ErrorContains(t, CheckPatterns(&schema), `pattern "\\bA"`)
	}
}

func TestCheckPatternsBoundsProgramSize(t *testing.T) {
	properties := func(count int, node func(int) string) string {
		entries := make([]string, count)
		for i := range entries {
			entries[i] = fmt.Sprintf(`"p%d":%s`, i, node(i))
		}
		return `{"type":"object","properties":{` + strings.Join(entries, ",") + `}}`
	}
	digits := func(int) string { return `{"type":"string","pattern":"^[0-9]{0,1000}$"}` }
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{name: "every format", schema: properties(len(SupportedStringFormats()), func(i int) string {
			return fmt.Sprintf(`{"type":"string","format":%q}`, SupportedStringFormats()[i])
		})},
		{name: "long bounded repeat", schema: `{"type":"object","properties":{"note":{"type":"string","pattern":"^.{0,1000}$"}}}`},
		{name: "49 long patterns", schema: properties(49, digits)},
		{name: "50 long patterns", schema: properties(50, digits), wantErr: true},
		{name: "one pattern with 101 repeats of 1000", schema: `{"type":"object","properties":{"code":{"type":"string","pattern":"^` + strings.Repeat(`[a-z]{1000}`, 101) + `$"}}}`, wantErr: true},
		{name: "long patterns under items, anyOf and additionalProperties", schema: properties(51, func(i int) string {
			return [...]string{
				`{"type":"array","items":{"type":"string","pattern":"^[0-9]{0,1000}$"}}`,
				`{"anyOf":[{"type":"string","pattern":"^[0-9]{0,1000}$"},{"type":"null"}]}`,
				`{"type":"object","additionalProperties":{"type":"string","pattern":"^[0-9]{0,1000}$"}}`,
			}[i%3]
		}), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var schema jsonschema.Schema
			require.NoError(t, json.Unmarshal([]byte(test.schema), &schema))
			err := CheckPatterns(&schema)
			if !test.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "patterns compile to more than 100000 regexp instructions")
		})
	}
}
