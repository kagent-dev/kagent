package outputschema

import (
	"encoding/json"
	"fmt"
	"slices"
)

// CheckStrict reports the first place where a portable output schema breaks
// the rules OpenAI applies when a request sets strict: the root must not use
// anyOf, every node must set type (or use anyOf, enum or $ref), and
// every object must set additionalProperties to false and list all of its
// properties in required. OpenAI rejects such a strict request outright,
// so kagent checks at compile time instead.
func CheckStrict(raw json.RawMessage) error {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("decode schema: %w", err)
	}
	if _, ok := schema["anyOf"]; ok {
		return fmt.Errorf("strict output schema: the root must not use anyOf")
	}
	return checkStrict(schema, "")
}

func checkStrict(schema map[string]any, path string) error {
	if !hasTypeOrAlternative(schema) {
		return fmt.Errorf("strict output schema: %s must set type", pointer(path))
	}
	properties, _ := schema["properties"].(map[string]any)
	if schema["type"] == "object" || properties != nil {
		if schema["additionalProperties"] != false {
			return fmt.Errorf("strict output schema: %s must set additionalProperties to false", pointer(path))
		}
		required := map[string]bool{}
		if names, ok := schema["required"].([]any); ok {
			for _, name := range names {
				if name, ok := name.(string); ok {
					required[name] = true
				}
			}
		}
		for _, name := range sortedKeys(properties) {
			if !required[name] {
				return fmt.Errorf("strict output schema: %s must list property %q in required", pointer(path), name)
			}
		}
	}

	for _, name := range sortedKeys(properties) {
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
	for _, name := range sortedKeys(defs) {
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

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
