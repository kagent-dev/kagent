package outputschema

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"regexp/syntax"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// The set is the intersection of the formats OpenAI and Anthropic structured
// outputs document. jsonschema-go ignores format during validation, so each
// format is asserted as a pattern.
var stringFormatPatterns = func() map[string]string {
	const (
		leapYear = `(?:\d\d(?:0[48]|[2468][048]|[13579][26])|(?:[02468][048]|[13579][26])00)`
		fullDate = `(?:\d{4}-(?:(?:0[1-9]|1[0-2])-(?:0[1-9]|1\d|2[0-8])|(?:0[13-9]|1[0-2])-(?:29|30)|(?:0[13578]|1[02])-31)|` + leapYear + `-02-29)`
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
		atom     = "[A-Za-z0-9!#$%&'*+/=?^_`{|}~-]+"
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
		"email":     anchored(atom + `(?:\.` + atom + `)*@` + hostname),
		"hostname":  anchored(hostname),
		"ipv4":      anchored(ipv4),
		"ipv6":      anchored(ipv6),
		"time":      anchored(partial + offset + `?`),
		"uuid":      anchored(hex + `{8}-` + hex + `{4}-` + hex + `{4}-` + hex + `{4}-` + hex + `{12}`),
	}
}()

// SupportedStringFormats returns the string formats accepted in a portable
// output schema, sorted.
func SupportedStringFormats() []string {
	return slices.Sorted(maps.Keys(stringFormatPatterns))
}

type Validator struct {
	*jsonschema.Resolved
	formatByAllOfPath map[string]formatAssertion
}

type formatAssertion struct {
	node   string
	format string
}

// Format reports the schema node and string format that the subschema at path
// asserts, if the subschema asserts a format.
func (v *Validator) Format(path string) (node, format string, ok bool) {
	assertion, ok := v.formatByAllOfPath[path]
	return assertion.node, assertion.format, ok
}

// ResolveValidator resolves a canonical output schema into a validator that
// asserts every supported string format in addition to the schema's own
// keywords.
func ResolveValidator(raw json.RawMessage) (*Validator, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	formatByAllOfPath := map[string]formatAssertion{}
	if err := assertFormats(&schema, "", formatByAllOfPath); err != nil {
		return nil, err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve output schema: %w", err)
	}
	return &Validator{Resolved: resolved, formatByAllOfPath: formatByAllOfPath}, nil
}

// Paths follow the JSON Pointers that jsonschema-go reports in its errors.
func assertFormats(schema *jsonschema.Schema, path string, formatByAllOfPath map[string]formatAssertion) error {
	if schema == nil {
		return nil
	}
	if schema.Format != "" {
		pattern, ok := stringFormatPatterns[schema.Format]
		if !ok {
			return fmt.Errorf("unsupported output schema format %q", schema.Format)
		}
		node := path
		if node == "" {
			node = "root"
		}
		formatByAllOfPath[fmt.Sprintf("%s/allOf/%d", path, len(schema.AllOf))] = formatAssertion{node: node, format: schema.Format}
		schema.AllOf = append(schema.AllOf, &jsonschema.Schema{Pattern: pattern})
	}
	for childPath, child := range subschemas(schema) {
		if err := assertFormats(child, path+childPath, formatByAllOfPath); err != nil {
			return err
		}
	}
	return nil
}

// maxPatternProgramSize bounds the regexp instructions that the patterns of one
// output schema compile to, counting each format as the pattern that asserts it.
// Compiling takes about 300 bytes per instruction, so the bound keeps a schema
// near 30 MB.
const maxPatternProgramSize = 100_000

var formatProgramSizes = func() map[string]int64 {
	sizes := make(map[string]int64, len(stringFormatPatterns))
	for format, pattern := range stringFormatPatterns {
		parsed, err := syntax.Parse(pattern, syntax.Perl)
		if err != nil {
			panic(fmt.Sprintf("format %s pattern: %v", format, err))
		}
		sizes[format] = programSize(parsed)
	}
	return sizes
}()

// CheckPatterns rejects patterns that compile as RE2 but that a provider cannot
// constrain. Claude documents word boundaries as unsupported, and Bedrock
// accepts them but then degenerates, so \b and \B are rejected. It also rejects
// a schema whose patterns and formats compile to more than
// maxPatternProgramSize instructions. It parses patterns without compiling
// them, so it must run before Resolve, which compiles every pattern.
func CheckPatterns(schema *jsonschema.Schema) error {
	var size int64
	return checkPatterns(schema, &size)
}

func checkPatterns(schema *jsonschema.Schema, size *int64) error {
	if schema == nil {
		return nil
	}
	if schema.Pattern != "" {
		parsed, err := syntax.Parse(schema.Pattern, syntax.Perl)
		if err != nil {
			return fmt.Errorf("pattern %q: %w", schema.Pattern, err)
		}
		if hasWordBoundary(parsed) {
			return fmt.Errorf(`pattern %q uses a word boundary (\b or \B), which Claude structured outputs do not support`, schema.Pattern)
		}
		*size += programSize(parsed)
	}
	*size += formatProgramSizes[schema.Format]
	if *size > maxPatternProgramSize {
		return fmt.Errorf("patterns and formats compile to more than %d regexp instructions; lower large repeat counts such as {1000} or use fewer pattern and format properties", maxPatternProgramSize)
	}
	for _, child := range subschemas(schema) {
		if err := checkPatterns(child, size); err != nil {
			return err
		}
	}
	return nil
}

// programSize estimates the instructions that expression compiles to once its
// repeats are expanded, using the same rules regexp/syntax uses to reject an
// expression that is too large.
func programSize(expression *syntax.Regexp) int64 {
	var size int64
	switch expression.Op {
	case syntax.OpLiteral:
		size = int64(len(expression.Rune))
	case syntax.OpCapture, syntax.OpStar:
		size = 2 + programSize(expression.Sub[0])
	case syntax.OpPlus, syntax.OpQuest:
		size = 1 + programSize(expression.Sub[0])
	case syntax.OpConcat, syntax.OpAlternate:
		for _, sub := range expression.Sub {
			size += programSize(sub)
		}
		if expression.Op == syntax.OpAlternate {
			size += int64(len(expression.Sub)) - 1
		}
	case syntax.OpRepeat:
		sub := programSize(expression.Sub[0])
		switch {
		case expression.Max == -1 && expression.Min == 0:
			size = 2 + sub
		case expression.Max == -1:
			size = 1 + int64(expression.Min)*sub
		default:
			size = int64(expression.Max)*sub + int64(expression.Max-expression.Min)
		}
	}
	return max(1, size)
}

func hasWordBoundary(expression *syntax.Regexp) bool {
	if expression.Op == syntax.OpWordBoundary || expression.Op == syntax.OpNoWordBoundary {
		return true
	}
	return slices.ContainsFunc(expression.Sub, hasWordBoundary)
}

func subschemas(schema *jsonschema.Schema) iter.Seq2[string, *jsonschema.Schema] {
	return func(yield func(string, *jsonschema.Schema) bool) {
		for _, name := range slices.Sorted(maps.Keys(schema.Defs)) {
			if !yield("/$defs/"+name, schema.Defs[name]) {
				return
			}
		}
		for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
			if !yield("/properties/"+name, schema.Properties[name]) {
				return
			}
		}
		for i, child := range schema.AnyOf {
			if !yield(fmt.Sprintf("/anyOf/%d", i), child) {
				return
			}
		}
		if !yield("/items", schema.Items) {
			return
		}
		yield("/additionalProperties", schema.AdditionalProperties)
	}
}
