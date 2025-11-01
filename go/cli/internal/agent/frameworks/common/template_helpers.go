package common

import (
	"strings"
	"unicode"
)

// ToPascalCase converts a string to PascalCase
func ToPascalCase(s string) string {
	// Split by common delimiters
	words := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})

	for i, word := range words {
		if len(word) > 0 {
			// Capitalize first letter, lowercase rest
			runes := []rune(word)
			runes[0] = unicode.ToUpper(runes[0])
			for j := 1; j < len(runes); j++ {
				runes[j] = unicode.ToLower(runes[j])
			}
			words[i] = string(runes)
		}
	}

	return strings.Join(words, "")
}

// ToUpper converts string to uppercase
func ToUpper(s string) string {
	return strings.ToUpper(s)
}

// TemplateHelpers returns a map of helper functions for templates
func TemplateHelpers() map[string]interface{} {
	return map[string]interface{}{
		"ToPascalCase": ToPascalCase,
		"ToUpper":      ToUpper,
	}
}
