package config

import "regexp"

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidToolName reports whether a name is a literal Claude permission rule.
func ValidToolName(name string) bool { return toolNamePattern.MatchString(name) }
