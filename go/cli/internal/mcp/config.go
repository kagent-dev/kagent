package mcp

import "github.com/kagent-dev/kagent/go/cli/internal/mcp/manifests"

// ProjectConfig contains all the information needed to generate a project
type ProjectConfig struct {
	ProjectName  string
	Framework    string
	Version      string
	Description  string
	Author       string
	Email        string
	Tools        map[string]manifests.ToolConfig
	Secrets      manifests.SecretsConfig
	Directory    string
	NoGit        bool
	Verbose      bool
	GoModuleName string
}

type ToolConfig struct {
	ToolName    string
	Description string
}
