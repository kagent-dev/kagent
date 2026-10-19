package outputschema

import (
	"encoding/json"
	"fmt"
	"testing"

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
			valid:   []string{"2026-10-19T12:00:00Z", "2026-10-19t12:00:00.123+02:00", "2024-02-29T23:59:60-05:30"},
			invalid: []string{"not-a-date", "2026-10-19", "2026-10-19T12:00:00", "2026-13-01T00:00:00Z", "2026-02-30T00:00:00Z", "2026-10-19T24:00:00Z"},
		},
		{
			format:  "date",
			valid:   []string{"2026-10-19", "2026-12-31", "2024-02-29"},
			invalid: []string{"not-a-date", "2026-10-19T12:00:00Z", "2026-04-31", "2026-00-10", "26-10-19"},
		},
		{
			format:  "time",
			valid:   []string{"12:00:00", "23:59:59.5Z", "08:30:00+01:00"},
			invalid: []string{"noon", "24:00:00", "12:60:00", "12:00"},
		},
		{
			format:  "duration",
			valid:   []string{"P1D", "PT1H30M", "P1Y2M3DT4H5M6S", "P2W"},
			invalid: []string{"P", "PT", "1D", "P1DT", "P1H"},
		},
		{
			format:  "email",
			valid:   []string{"user@example.com", "first.last+tag@sub.example.io"},
			invalid: []string{"user", "user@", "@example.com", "user@-example.com"},
		},
		{
			format:  "hostname",
			valid:   []string{"example.com", "localhost", "a-b.c1"},
			invalid: []string{"-example.com", "exa mple.com", "example..com"},
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

func TestResolveValidatorRejectsUnsupportedFormat(t *testing.T) {
	_, err := ResolveValidator(json.RawMessage(`{"type":"object","properties":{"site":{"type":"string","format":"uri"}}}`))
	require.ErrorContains(t, err, `unsupported output schema format "uri"`)
}
