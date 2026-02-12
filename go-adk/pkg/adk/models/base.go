package models

import (
	"time"
)

// Tool holds function declarations (used when converting MCP registry to genai tools).
type Tool struct {
	FunctionDeclarations []FunctionDeclaration
}

// FunctionDeclaration represents a function declaration (MCP/OpenAI schema).
type FunctionDeclaration struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON schema
}

// Default execution timeout (30 minutes)
const DefaultExecutionTimeout = 30 * time.Minute
