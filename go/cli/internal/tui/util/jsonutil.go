package util

import "encoding/json"

// CollectTextFields walks arbitrary JSON and collects values under keys named "text".
func CollectTextFields(node any, acc *[]string) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if k == "text" {
				if s, ok := v.(string); ok && s != "" {
					*acc = append(*acc, s)
				}
			}
			CollectTextFields(v, acc)
		}
	case []any:
		for _, v := range n {
			CollectTextFields(v, acc)
		}
	}
}

// Helpers to safely walk dynamic JSON structures
func GetMap(node any, key string) map[string]any {
	if m, ok := node.(map[string]any); ok {
		if v, ok := m[key]; ok {
			if mm, ok := v.(map[string]any); ok {
				return mm
			}
		}
	}
	return nil
}

func GetArray(node any, key string) []any {
	if m, ok := node.(map[string]any); ok {
		if v, ok := m[key]; ok {
			if arr, ok := v.([]any); ok {
				return arr
			}
		}
	}
	return nil
}

func GetString(node any, key string) string {
	if m, ok := node.(map[string]any); ok {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func GetBool(node any, key string) bool {
	if m, ok := node.(map[string]any); ok {
		if v, ok := m[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}

func CompactJSON(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
