package outputschema

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp/syntax"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// stringFormatPatterns maps each supported JSON Schema string format to an
// anchored RE2 expression that the final output validator asserts. The set is
// the intersection of the formats OpenAI and Anthropic structured outputs
// document. jsonschema-go treats format as an annotation only, so assertion is
// expressed as a pattern, which also keeps anyOf branch selection inside the
// validator.
var stringFormatPatterns = func() map[string]string {
	const (
		fullDate = `\d{4}-(?:(?:0[1-9]|1[0-2])-(?:0[1-9]|1\d|2[0-8])|(?:0[13-9]|1[0-2])-(?:29|30)|(?:0[13578]|1[02])-31|02-29)`
		partial  = `(?:[01]\d|2[0-3]):[0-5]\d:(?:[0-5]\d|60)(?:\.\d+)?`
		offset   = `(?:[Zz]|[+-](?:[01]\d|2[0-3]):[0-5]\d)`
		label    = `[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?`
		hostname = label + `(?:\.` + label + `)*`
		octet    = `(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)`
		ipv4     = octet + `(?:\.` + octet + `){3}`
		h16      = `[0-9A-Fa-f]{1,4}`
		ls32     = `(?:` + h16 + `:` + h16 + `|` + ipv4 + `)`
		durTime  = `T(?:\d+H(?:\d+M)?(?:\d+S)?|\d+M(?:\d+S)?|\d+S)`
		durDate  = `(?:\d+Y(?:\d+M)?(?:\d+D)?|\d+M(?:\d+D)?|\d+D)`
		hex      = `[0-9A-Fa-f]`
	)
	ipv6 := `(?:` +
		`(?:` + h16 + `:){6}` + ls32 +
		`|::(?:` + h16 + `:){5}` + ls32 +
		`|(?:` + h16 + `)?::(?:` + h16 + `:){4}` + ls32 +
		`|(?:(?:` + h16 + `:){0,1}` + h16 + `)?::(?:` + h16 + `:){3}` + ls32 +
		`|(?:(?:` + h16 + `:){0,2}` + h16 + `)?::(?:` + h16 + `:){2}` + ls32 +
		`|(?:(?:` + h16 + `:){0,3}` + h16 + `)?::` + h16 + `:` + ls32 +
		`|(?:(?:` + h16 + `:){0,4}` + h16 + `)?::` + ls32 +
		`|(?:(?:` + h16 + `:){0,5}` + h16 + `)?::` + h16 +
		`|(?:(?:` + h16 + `:){0,6}` + h16 + `)?::` +
		`)`
	anchored := func(expression string) string { return `^` + expression + `$` }
	return map[string]string{
		"date":      anchored(fullDate),
		"date-time": anchored(fullDate + `[Tt]` + partial + offset),
		"duration":  anchored(`P(?:\d+W|` + durDate + `(?:` + durTime + `)?|` + durTime + `)`),
		"email":     anchored("[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@" + hostname),
		"hostname":  anchored(hostname),
		"ipv4":      anchored(ipv4),
		"ipv6":      anchored(ipv6),
		"time":      anchored(partial + offset + `?`),
		"uuid":      anchored(hex + `{8}-` + hex + `{4}-` + hex + `{4}-` + hex + `{4}-` + hex + `{12}`),
	}
}()

// geminiStringFormats are the formats that both the genai.Schema reference and
// the Gemini structured output guide document. Other formats are dropped from
// the Gemini projection and enforced only by the final output validator.
var geminiStringFormats = map[string]bool{"date": true, "date-time": true}

// SupportedStringFormats returns the string formats accepted in a portable
// output schema, sorted.
func SupportedStringFormats() []string {
	return slices.Sorted(maps.Keys(stringFormatPatterns))
}

// ResolveValidator resolves a canonical output schema into a validator that
// asserts every supported string format in addition to the schema's own
// keywords.
func ResolveValidator(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	if err := assertFormats(&schema); err != nil {
		return nil, err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve output schema: %w", err)
	}
	return resolved, nil
}

// assertFormats walks the keywords the portable profile allows to contain
// subschemas and adds each format's pattern. A schema that already has its own
// pattern gets the format pattern through allOf so both must match.
func assertFormats(schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}
	if schema.Format != "" {
		pattern, ok := stringFormatPatterns[schema.Format]
		if !ok {
			return fmt.Errorf("unsupported output schema format %q", schema.Format)
		}
		if schema.Pattern == "" {
			schema.Pattern = pattern
		} else {
			schema.AllOf = append(schema.AllOf, &jsonschema.Schema{Pattern: pattern})
		}
	}
	for _, child := range subschemas(schema) {
		if err := assertFormats(child); err != nil {
			return err
		}
	}
	return nil
}

// CheckPortablePatterns rejects patterns that compile as RE2 but that a
// provider cannot constrain. Claude documents word boundaries as unsupported,
// and Bedrock accepts them but then degenerates, so \b and \B are rejected.
func CheckPortablePatterns(schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}
	if schema.Pattern != "" {
		parsed, err := syntax.Parse(schema.Pattern, syntax.Perl)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", schema.Pattern, err)
		}
		if hasWordBoundary(parsed) {
			return fmt.Errorf(`pattern %q uses a word boundary (\b or \B), which structured output providers do not support; anchor with ^ and $ instead`, schema.Pattern)
		}
	}
	for _, child := range subschemas(schema) {
		if err := CheckPortablePatterns(child); err != nil {
			return err
		}
	}
	return nil
}

func hasWordBoundary(expression *syntax.Regexp) bool {
	if expression.Op == syntax.OpWordBoundary || expression.Op == syntax.OpNoWordBoundary {
		return true
	}
	return slices.ContainsFunc(expression.Sub, hasWordBoundary)
}

// subschemas returns the direct children of the keywords the portable profile
// allows to contain schemas.
func subschemas(schema *jsonschema.Schema) []*jsonschema.Schema {
	return slices.Concat(
		slices.Collect(maps.Values(schema.Defs)),
		slices.Collect(maps.Values(schema.Properties)),
		schema.AnyOf,
		[]*jsonschema.Schema{schema.Items, schema.AdditionalProperties},
	)
}
