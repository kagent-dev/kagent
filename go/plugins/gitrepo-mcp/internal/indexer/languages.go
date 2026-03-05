package indexer

import (
	"path/filepath"
	"strings"
)

var extToLang = map[string]string{
	".go":          "go",
	".py":          "python",
	".js":          "javascript",
	".jsx":         "javascript",
	".mjs":         "javascript",
	".ts":          "typescript",
	".tsx":         "typescript",
	".java":        "java",
	".rs":          "rust",
	".md":          "markdown",
	".mdx":         "markdown",
	".yaml":        "yaml",
	".yml":         "yaml",
	".toml":        "toml",
	".groovy":      "groovy",
	".gradle":      "groovy",
	".jenkinsfile": "groovy",
}

// DetectLanguage returns the language identifier for a file path based on extension.
// Returns empty string for unknown extensions.
func DetectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	base := strings.ToLower(filepath.Base(filePath))
	switch base {
	case "jenkinsfile", "groovyfile":
		return "groovy"
	case "makefile", "dockerfile":
		return ""
	}
	return ""
}
