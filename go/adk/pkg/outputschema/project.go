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
	maxProjectionDepth = 32
	maxProjectionNodes = 1_000
)

// Project converts a canonical kagent output schema to the genai.Schema used
// by Go ADK and also provides the concrete Go ADK compatibility check:
// recursive references and schemas whose expanded form exceeds the configured
// safety limits are rejected here.
//
// The canonical JSON Schema remains authoritative for provider requests that
// accept raw JSON Schema and for final response validation. genai.Schema lacks
// const and additionalProperties, and represents enum values as strings, so
// those constraints may be enforced only by the provider and final validator.
func Project(raw json.RawMessage) (*genai.Schema, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolve output schema: %w", err)
	}
	projector := schemaProjector{root: &schema}
	return projector.project(&schema, nil, 1)
}

type schemaProjector struct {
	root  *jsonschema.Schema
	nodes int
}

// project recursively converts a jsonschema.Schema to a genai.Schema, expanding
// local $defs references and enforcing the configured safety limits. The stack
// tracks the current recursion path to detect cycles.
func (p *schemaProjector) project(schema *jsonschema.Schema, stack map[*jsonschema.Schema]struct{}, depth int) (*genai.Schema, error) {
	if schema == nil {
		return nil, nil
	}
	if depth > maxProjectionDepth {
		return nil, fmt.Errorf("output schema projection exceeds maximum depth %d", maxProjectionDepth)
	}
	p.nodes++
	if p.nodes > maxProjectionNodes {
		return nil, fmt.Errorf("output schema projection exceeds maximum node count %d", maxProjectionNodes)
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
		target := p.root.Defs[name]
		if target == nil {
			return nil, fmt.Errorf("output schema reference %q not found", schema.Ref)
		}
		return p.project(target, next, depth+1)
	}

	projected := &genai.Schema{Title: schema.Title, Description: schema.Description}
	switch schema.Type {
	case "":
	case "object":
		projected.Type = genai.TypeObject
	case "array":
		projected.Type = genai.TypeArray
	case "string":
		projected.Type = genai.TypeString
	case "number":
		projected.Type = genai.TypeNumber
	case "integer":
		projected.Type = genai.TypeInteger
	case "boolean":
		projected.Type = genai.TypeBoolean
	case "null":
		projected.Type = genai.TypeNULL
	default:
		return nil, fmt.Errorf("unsupported output schema type %q", schema.Type)
	}
	projected.Required = append([]string(nil), schema.Required...)
	if schema.Items != nil {
		items, err := p.project(schema.Items, next, depth+1)
		if err != nil {
			return nil, err
		}
		projected.Items = items
	}
	if len(schema.Properties) > 0 {
		projected.Properties = make(map[string]*genai.Schema, len(schema.Properties))
		for name, property := range schema.Properties {
			value, err := p.project(property, next, depth+1)
			if err != nil {
				return nil, fmt.Errorf("project output property %q: %w", name, err)
			}
			projected.Properties[name] = value
		}
	}
	for _, candidate := range schema.AnyOf {
		value, err := p.project(candidate, next, depth+1)
		if err != nil {
			return nil, fmt.Errorf("project output anyOf: %w", err)
		}
		projected.AnyOf = append(projected.AnyOf, value)
	}
	for _, value := range schema.Enum {
		text, ok := value.(string)
		if !ok {
			continue
		}
		projected.Enum = append(projected.Enum, text)
	}
	if schema.Const != nil {
		if value, ok := (*schema.Const).(string); ok {
			projected.Enum = []string{value}
		}
	}
	return projected, nil
}
