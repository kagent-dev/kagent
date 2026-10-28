package outputschema

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// CheckStrict reports the first place where a portable output schema breaks
// the rules OpenAI applies when a request sets strict: the root must not use
// anyOf, every node must set type (or use anyOf, enum or $ref), a $ref must
// have no other keywords next to it, and every object must set
// additionalProperties to false and list all of its properties in required.
// OpenAI rejects such a strict request outright.
func CheckStrict(raw json.RawMessage) error {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	if _, ok := schema["anyOf"]; ok {
		return fmt.Errorf("the root must not use anyOf")
	}
	return checkStrict(schema, "")
}

func checkStrict(schema map[string]any, path string) error {
	if _, ok := schema["$ref"]; ok && len(schema) > 1 {
		return fmt.Errorf("%s must not set other keywords next to $ref; move them into the referenced definition, or wrap the $ref in anyOf, such as anyOf: [{$ref: ...}, {type: \"null\"}]", pointer(path))
	}
	if !hasTypeOrAlternative(schema) {
		return fmt.Errorf("%s must set type", pointer(path))
	}
	properties, _ := schema["properties"].(map[string]any)
	names := slices.Sorted(maps.Keys(properties))
	if hasType(schema, "object") || properties != nil {
		if _, isMap := schema["additionalProperties"].(map[string]any); isMap {
			return fmt.Errorf("%s must set additionalProperties to false: strict mode cannot express a map with free-form keys, so list the keys as properties or set outputSchemaStrict to false", pointer(path))
		}
		if schema["additionalProperties"] != false {
			return fmt.Errorf("%s must set additionalProperties to false", pointer(path))
		}
		required := map[string]bool{}
		if listed, ok := schema["required"].([]any); ok {
			for _, name := range listed {
				if name, ok := name.(string); ok {
					required[name] = true
				}
			}
		}
		for _, name := range names {
			if !required[name] {
				return fmt.Errorf("%s must list property %q in required", pointer(path), name)
			}
		}
	}

	for _, name := range names {
		if err := checkChild(properties[name], path+"/properties/"+name); err != nil {
			return err
		}
	}
	if err := checkChild(schema["items"], path+"/items"); err != nil {
		return err
	}
	if anyOf, ok := schema["anyOf"].([]any); ok {
		for i, child := range anyOf {
			if err := checkChild(child, fmt.Sprintf("%s/anyOf/%d", path, i)); err != nil {
				return err
			}
		}
	}
	defs, _ := schema["$defs"].(map[string]any)
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		if err := checkChild(defs[name], path+"/$defs/"+name); err != nil {
			return err
		}
	}
	return nil
}

func hasTypeOrAlternative(schema map[string]any) bool {
	for _, key := range []string{"type", "anyOf", "enum", "$ref"} {
		if _, ok := schema[key]; ok {
			return true
		}
	}
	return false
}

func hasType(schema map[string]any, name string) bool {
	switch value := schema["type"].(type) {
	case string:
		return value == name
	case []any:
		return slices.Contains(value, any(name))
	}
	return false
}

func checkChild(value any, path string) error {
	child, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return checkStrict(child, path)
}

func pointer(path string) string {
	if path == "" {
		return "the root object"
	}
	return path
}
