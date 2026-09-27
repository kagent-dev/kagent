// Package outputschema converts kagent's portable output schema into the
// narrower schema representation used by Go ADK.
package outputschema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"
)

const (
	maxConversionDepth = 32
	maxConversionNodes = 1_000
)

// ToGenAISchema converts a canonical kagent output schema to the genai.Schema used
// by Go ADK and also provides the concrete Go ADK compatibility check:
// recursive references and schemas whose expanded form exceeds the configured
// safety limits are rejected here.
//
// The canonical JSON Schema remains authoritative for provider requests that
// accept raw JSON Schema and for final response validation. genai.Schema lacks
// const and additionalProperties, and represents enum values as strings, so
// those constraints may be enforced only by the provider and final validator.
func ToGenAISchema(raw json.RawMessage) (*genai.Schema, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolve output schema: %w", err)
	}
	converter := schemaConverter{root: &schema}
	return converter.convert(&schema, nil, 1)
}

type schemaConverter struct {
	root  *jsonschema.Schema
	nodes int
}

// convert recursively converts a jsonschema.Schema to a genai.Schema, expanding
// local $defs references and enforcing the configured safety limits. The stack
// tracks the current recursion path to detect cycles.
func (c *schemaConverter) convert(schema *jsonschema.Schema, stack map[*jsonschema.Schema]struct{}, depth int) (*genai.Schema, error) {
	if schema == nil {
		return nil, nil
	}
	if depth > maxConversionDepth {
		return nil, fmt.Errorf("output schema conversion exceeds maximum depth %d", maxConversionDepth)
	}
	c.nodes++
	if c.nodes > maxConversionNodes {
		return nil, fmt.Errorf("output schema conversion exceeds maximum node count %d", maxConversionNodes)
	}
	if _, recursive := stack[schema]; recursive {
		return nil, fmt.Errorf("recursive output schema is not supported")
	}
	next := make(map[*jsonschema.Schema]struct{}, len(stack)+1)
	for item := range stack {
		next[item] = struct{}{}
	}
	next[schema] = struct{}{}

	if schema.Ref != "" {
		const prefix = "#/$defs/"
		name := strings.TrimPrefix(schema.Ref, prefix)
		if name == schema.Ref || strings.Contains(name, "/") {
			return nil, fmt.Errorf("unsupported output schema reference %q", schema.Ref)
		}
		name = strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
		target := c.root.Defs[name]
		if target == nil {
			return nil, fmt.Errorf("output schema reference %q not found", schema.Ref)
		}
		return c.convert(target, next, depth+1)
	}

	converted := &genai.Schema{Title: schema.Title, Description: schema.Description}
	switch schema.Type {
	case "":
	case "object":
		converted.Type = genai.TypeObject
	case "array":
		converted.Type = genai.TypeArray
	case "string":
		converted.Type = genai.TypeString
	case "number":
		converted.Type = genai.TypeNumber
	case "integer":
		converted.Type = genai.TypeInteger
	case "boolean":
		converted.Type = genai.TypeBoolean
	case "null":
		converted.Type = genai.TypeNULL
	default:
		return nil, fmt.Errorf("unsupported output schema type %q", schema.Type)
	}
	converted.Required = append([]string(nil), schema.Required...)
	if schema.Items != nil {
		items, err := c.convert(schema.Items, next, depth+1)
		if err != nil {
			return nil, err
		}
		converted.Items = items
	}
	if len(schema.Properties) > 0 {
		converted.Properties = make(map[string]*genai.Schema, len(schema.Properties))
		for name, property := range schema.Properties {
			value, err := c.convert(property, next, depth+1)
			if err != nil {
				return nil, fmt.Errorf("convert output property %q: %w", name, err)
			}
			converted.Properties[name] = value
		}
	}
	for _, candidate := range schema.AnyOf {
		value, err := c.convert(candidate, next, depth+1)
		if err != nil {
			return nil, fmt.Errorf("convert output anyOf: %w", err)
		}
		converted.AnyOf = append(converted.AnyOf, value)
	}
	convertedEnum := make([]string, 0, len(schema.Enum))
	for _, value := range schema.Enum {
		text, ok := value.(string)
		if !ok {
			// genai.Schema can only express string enums. Omitting a mixed or
			// non-string enum keeps the provider schema broader than the canonical
			// contract; final response validation still enforces the full enum.
			convertedEnum = nil
			break
		}
		convertedEnum = append(convertedEnum, text)
	}
	converted.Enum = convertedEnum
	if schema.Const != nil {
		if value, ok := (*schema.Const).(string); ok {
			converted.Enum = []string{value}
		}
	}
	return converted, nil
}
