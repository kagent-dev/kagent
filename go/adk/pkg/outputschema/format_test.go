package outputschema

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestResolveValidatorAssertsStringFormats(t *testing.T) {
	tests := []struct {
		format  string
		valid   []string
		invalid []string
	}{
		{
			format:  "date-time",
			valid:   []string{"2026-10-19T12:00:00Z", "2026-10-19t12:00:00.123+02:00", "2024-02-29T23:59:60-05:30", "2000-02-29T00:00:00Z"},
			invalid: []string{"not-a-date", "2026-10-19", "2026-10-19T12:00:00", "2026-13-01T00:00:00Z", "2026-02-30T00:00:00Z", "2026-10-19T24:00:00Z", "2025-02-29T00:00:00Z"},
		},
		{
			format:  "date",
			valid:   []string{"2026-10-19", "2026-12-31", "2024-02-29", "2000-02-29", "2400-02-29"},
			invalid: []string{"not-a-date", "2026-10-19T12:00:00Z", "2026-04-31", "2026-00-10", "26-10-19", "2025-02-29", "1900-02-29", "2100-02-29"},
		},
		{
			format:  "time",
			valid:   []string{"23:59:59.5Z", "08:30:00+01:00", "12:34:56-00:00"},
			invalid: []string{"noon", "24:00:00", "12:60:00", "12:00", "12:00:00", "12:00:00.52"},
		},
		{
			format:  "duration",
			valid:   []string{"P1D", "PT1H30M", "P1Y2M3DT4H5M6S", "P2W", "P1Y2M", "PT1H2M", "PT1M2S"},
			invalid: []string{"P", "PT", "1D", "P1DT", "P1H", "P1Y2D", "PT1H2S"},
		},
		{
			format: "email",
			valid: []string{
				"user@example.com", "first.last+tag@sub.example.io",
				`"joe bloggs"@example.com`, `"joe..bloggs"@example.com`, `"joe@bloggs"@example.com`,
				`""@iana.org`, `" "@iana.org`, `"\a"@iana.org`, `"\""@iana.org`, `"\\"@iana.org`,
				"joe.bloggs@[127.0.0.1]", "joe.bloggs@[IPv6:::1]", "a@[ipv6:::1]",
			},
			invalid: []string{
				"user", "user@", "@example.com", "user@-example.com", ".user@example.com", "user.@example.com", "a..b@example.com",
				`"test"test@iana.org`, `"test@iana.org`, `test"@iana.org`, "\"test\\\u00a9\"@iana.org",
				"joe.bloggs@[127.0.0.300]", "a@[]", "a@[1.2.3]", "test@[RFC-5322-domain-literal]", "test@a[255.255.255.255]", "[1.2.3.4]@iana.org",
			},
		},
		{
			format:  "hostname",
			valid:   []string{"example.com", "localhost", "a-b.c1", strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 61)},
			invalid: []string{"-example.com", "exa mple.com", "example..com", strings.Repeat(strings.Repeat("a", 63)+".", 3) + strings.Repeat("a", 62)},
		},
		{
			format:  "ipv4",
			valid:   []string{"192.168.0.1", "0.0.0.0", "255.255.255.255"},
			invalid: []string{"256.0.0.1", "1.2.3", "01.2.3.4", "::1"},
		},
		{
			format:  "ipv6",
			valid:   []string{"::1", "::", "2001:db8::8a2e:370:7334", "2001:0db8:0000:0000:0000:ff00:0042:8329", "::ffff:192.0.2.1"},
			invalid: []string{"192.168.0.1", "2001:db8:::1", "12345::", "1:2:3:4:5:6:7:8:9"},
		},
		{
			format:  "uuid",
			valid:   []string{"123e4567-e89b-12d3-a456-426614174000", "123E4567-E89B-12D3-A456-426614174000"},
			invalid: []string{"123e4567e89b12d3a456426614174000", "not-a-uuid", "123e4567-e89b-12d3-a456-42661417400g"},
		},
	}
	for _, test := range tests {
		t.Run(test.format, func(t *testing.T) {
			validator, err := ResolveValidator(json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"value":{"type":"string","format":%q}}}`, test.format)))
			require.NoError(t, err)
			for _, value := range test.valid {
				require.NoError(t, validator.Validate(map[string]any{"value": value}), "valid %s %q", test.format, value)
			}
			for _, value := range test.invalid {
				require.Error(t, validator.Validate(map[string]any{"value": value}), "invalid %s %q", test.format, value)
			}
		})
	}
	tested := make([]string, 0, len(tests))
	for _, test := range tests {
		tested = append(tested, test.format)
	}
	require.ElementsMatch(t, SupportedStringFormats(), tested, "every supported format has cases")
}

func TestResolveValidatorAssertsFormatInsideNullableAnyOf(t *testing.T) {
	validator, err := ResolveValidator(json.RawMessage(`{
		"type":"object",
		"properties":{"due":{"anyOf":[{"$ref":"#/$defs/instant"},{"type":"null"}]}},
		"$defs":{"instant":{"type":"string","format":"date-time"}}
	}`))
	require.NoError(t, err)
	require.NoError(t, validator.Validate(map[string]any{"due": "2026-10-19T12:00:00Z"}))
	require.NoError(t, validator.Validate(map[string]any{"due": nil}))
	require.Error(t, validator.Validate(map[string]any{"due": "not-a-date"}))
}

func TestResolveValidatorAssertsFormatOnNullableTypeArray(t *testing.T) {
	validator, err := ResolveValidator(json.RawMessage(`{
		"type":"object",
		"properties":{"due":{"type":["string","null"],"format":"date-time"}}
	}`))
	require.NoError(t, err)
	require.NoError(t, validator.Validate(map[string]any{"due": "2026-10-19T12:00:00Z"}))
	require.NoError(t, validator.Validate(map[string]any{"due": nil}))
	require.Error(t, validator.Validate(map[string]any{"due": "not-a-date"}))
	require.Error(t, validator.Validate(map[string]any{"due": 1}))
}

func TestResolveValidatorAcceptsNullableTypeArray(t *testing.T) {
	validator, err := ResolveValidator(json.RawMessage(`{"type":"object","properties":{"note":{"type":["string","null"]}}}`))
	require.NoError(t, err)
	require.NoError(t, validator.Validate(map[string]any{"note": nil}))
	require.NoError(t, validator.Validate(map[string]any{"note": "ok"}))
	require.Error(t, validator.Validate(map[string]any{"note": 1.5}))
}

func TestResolveValidatorAssertsPatternAndFormatTogether(t *testing.T) {
	validator, err := ResolveValidator(json.RawMessage(`{
		"type":"object",
		"properties":{"day":{"type":"string","format":"date","pattern":"^2026-"}}
	}`))
	require.NoError(t, err)
	require.NoError(t, validator.Validate(map[string]any{"day": "2026-10-19"}))
	require.Error(t, validator.Validate(map[string]any{"day": "2025-10-19"}), "pattern mismatch")
	require.Error(t, validator.Validate(map[string]any{"day": "2026-not-a-date"}), "format mismatch")
}

var childSchemas = []struct {
	name     string
	path     string
	schema   func(node string) string
	instance func(value string) map[string]any
}{
	{
		name:     "properties",
		path:     "/properties/v",
		schema:   func(node string) string { return `{"type":"object","properties":{"v":` + node + `}}` },
		instance: func(value string) map[string]any { return map[string]any{"v": value} },
	},
	{
		name: "items",
		path: "/properties/v/items",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"type":"array","items":` + node + `}}}`
		},
		instance: func(value string) map[string]any { return map[string]any{"v": []any{value}} },
	},
	{
		name: "additionalProperties",
		path: "/properties/v/additionalProperties",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"type":"object","additionalProperties":` + node + `}}}`
		},
		instance: func(value string) map[string]any { return map[string]any{"v": map[string]any{"k": value}} },
	},
	{
		name: "anyOf",
		path: "/properties/v/anyOf/0",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"anyOf":[` + node + `,{"type":"null"}]}}}`
		},
		instance: func(value string) map[string]any { return map[string]any{"v": value} },
	},
	{
		name: "$defs",
		path: "/$defs/v",
		schema: func(node string) string {
			return `{"type":"object","properties":{"v":{"$ref":"#/$defs/v"}},"$defs":{"v":` + node + `}}`
		},
		instance: func(value string) map[string]any { return map[string]any{"v": value} },
	},
}

func TestResolveValidatorAssertsFormatInEveryChildSchema(t *testing.T) {
	for _, child := range childSchemas {
		t.Run(child.name, func(t *testing.T) {
			validator, err := ResolveValidator(json.RawMessage(child.schema(`{"type":"string","format":"date"}`)))
			require.NoError(t, err)
			require.NoError(t, validator.Validate(child.instance("2026-10-19")))
			assertion := child.path + "/allOf/0"
			require.ErrorContains(t, validator.Validate(child.instance("Monday")), "validating "+assertion+": pattern: ")
			node, format, ok := validator.Format(assertion)
			require.True(t, ok)
			require.Equal(t, child.path, node)
			require.Equal(t, "date", format)
		})
	}
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

func TestResolveValidatorRejectsUnsupportedFormat(t *testing.T) {
	_, err := ResolveValidator(json.RawMessage(`{"type":"object","properties":{"site":{"type":"string","format":"uri"}}}`))
	require.ErrorContains(t, err, `unsupported output schema format "uri"`)
}

func TestCheckPatternsBoundsProgramSize(t *testing.T) {
	properties := func(count int, node func(int) string) string {
		entries := make([]string, count)
		for i := range entries {
			entries[i] = fmt.Sprintf(`"p%d":%s`, i, node(i))
		}
		return `{"type":"object","properties":{` + strings.Join(entries, ",") + `}}`
	}
	ipv6 := func(int) string { return `{"type":"string","format":"ipv6"}` }
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{name: "every format", schema: properties(len(SupportedStringFormats()), func(i int) string {
			return fmt.Sprintf(`{"type":"string","format":%q}`, SupportedStringFormats()[i])
		})},
		{name: "long bounded repeat", schema: `{"type":"object","properties":{"note":{"type":"string","pattern":"^.{0,1000}$"}}}`},
		{name: "100 ipv6 formats", schema: properties(100, ipv6)},
		{name: "103 ipv6 formats", schema: properties(103, ipv6), wantErr: true},
		{name: "one pattern with 101 repeats of 1000", schema: `{"type":"object","properties":{"code":{"type":"string","pattern":"^` + strings.Repeat(`[a-z]{1000}`, 101) + `$"}}}`, wantErr: true},
		{name: "ipv6 formats under items, anyOf and additionalProperties", schema: properties(105, func(i int) string {
			return [...]string{
				`{"type":"array","items":{"type":"string","format":"ipv6"}}`,
				`{"anyOf":[{"type":"string","format":"ipv6"},{"type":"null"}]}`,
				`{"type":"object","additionalProperties":{"type":"string","format":"ipv6"}}`,
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
			require.ErrorContains(t, err, "patterns and formats compile to more than 100000 regexp instructions")
		})
	}
}
