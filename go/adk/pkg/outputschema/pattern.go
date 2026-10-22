package outputschema

import (
	"fmt"
	"maps"
	"regexp/syntax"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// supportedStringFormats are the formats that both OpenAI and Anthropic
// structured outputs document. kagent forwards format to the provider and does
// not assert it on the answer.
var supportedStringFormats = []string{"date", "date-time", "duration", "email", "hostname", "ipv4", "ipv6", "time", "uuid"}

// SupportedStringFormats returns the string formats accepted in a portable
// output schema, sorted.
func SupportedStringFormats() []string {
	return slices.Clone(supportedStringFormats)
}

// Compiling takes about 300 bytes per instruction, so the bound keeps a schema
// near 30 MB.
const maxPatternProgramSize = 100_000

// CheckPatterns rejects patterns that compile as RE2 but that a provider cannot
// constrain. Claude documents word boundaries as unsupported, and Bedrock
// accepts them but then degenerates, so \b and \B are rejected. It also rejects
// a schema whose patterns compile to more than maxPatternProgramSize
// instructions. It parses patterns without compiling them, so it must run
// before Resolve, which compiles every pattern.
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
	if *size > maxPatternProgramSize {
		return fmt.Errorf("patterns compile to more than %d regexp instructions; lower large repeat counts such as {1000} or use fewer pattern properties", maxPatternProgramSize)
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

func subschemas(schema *jsonschema.Schema) []*jsonschema.Schema {
	return slices.Concat(
		sortedValues(schema.Defs),
		sortedValues(schema.Properties),
		schema.AnyOf,
		[]*jsonschema.Schema{schema.Items, schema.AdditionalProperties},
	)
}

func sortedValues(schemas map[string]*jsonschema.Schema) []*jsonschema.Schema {
	values := make([]*jsonschema.Schema, 0, len(schemas))
	for _, name := range slices.Sorted(maps.Keys(schemas)) {
		values = append(values, schemas[name])
	}
	return values
}
