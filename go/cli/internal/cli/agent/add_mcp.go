package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/tui/dialogs"
)

// mcpTarget represents an MCP server target for config.yaml template
type mcpTarget struct {
	Name  string
	Cmd   string
	Args  []string
	Env   []string
	Image string
	Build string
}

// AddMcpCfg carries inputs for adding an MCP server entry to kagent.yaml
type AddMcpCfg struct {
	ProjectDir string
	Config     *config.Config
	// Non-interactive fields
	Name      string
	RemoteURL string
	Headers   []string // KEY=VALUE pairs
	Command   string
	Args      []string
	Env       []string
	Image     string
	Build     string
}

// AddMcpCmd runs the interactive flow to append an MCP server to kagent.yaml
func AddMcpCmd(cfg *AddMcpCfg) error {
	// Determine project directory
	projectDir, err := ResolveProjectDir(cfg.ProjectDir)
	if err != nil {
		return err
	}

	// Load manifest
	manifest, err := LoadManifest(projectDir)
	if err != nil {
		return err
	}

	verbose := IsVerbose(cfg.Config)
	if verbose {
		fmt.Printf("Loaded manifest for agent '%s' from %s\n", manifest.Name, projectDir)
	}

	// If flags provided, build non-interactively; else run wizard
	var res common.McpServerType
	if cfg.RemoteURL != "" || cfg.Command != "" || cfg.Image != "" || cfg.Build != "" {
		if cfg.RemoteURL != "" {
			headers := parseKeyValuePairs(cfg.Headers)
			res = common.McpServerType{
				Type:    "remote",
				URL:     cfg.RemoteURL,
				Name:    cfg.Name,
				Headers: headers,
			}
		} else {
			if cfg.Image != "" && cfg.Build != "" {
				return fmt.Errorf("only one of --image or --build may be set")
			}
			res = common.McpServerType{
				Type:    "command",
				Name:    cfg.Name,
				Command: cfg.Command,
				Args:    cfg.Args,
				Env:     cfg.Env,
				Image:   cfg.Image,
				Build:   cfg.Build,
			}
		}
	} else {
		// Prefer the wizard experience
		wiz := dialogs.NewMcpServerWizard()
		p := tea.NewProgram(wiz)
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run TUI: %w", err)
		}
		if !wiz.Ok() {
			fmt.Println("Canceled.")
			return nil
		}
		res = wiz.Result()
		if cfg.Name != "" {
			res.Name = cfg.Name
		}
	}

	// Ensure unique name
	for _, existing := range manifest.McpServers {
		if strings.EqualFold(existing.Name, res.Name) {
			return fmt.Errorf("an MCP server named '%s' already exists in kagent.yaml", res.Name)
		}
	}

	// Append and validate
	manifest.McpServers = append(manifest.McpServers, res)
	manager := common.NewManifestManager(projectDir)
	if err := manager.Validate(manifest); err != nil {
		return fmt.Errorf("invalid MCP server configuration: %w", err)
	}

	// Save back to disk
	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save kagent.yaml: %w", err)
	}

	// Regenerate mcp_tools.py with updated MCP servers for ADK Python projects
	if err := regenerateMcpToolsFile(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to regenerate mcp_tools.py: %w", err)
	}

	// Create/update individual MCP server directories with config.yaml
	if err := ensureMcpServerDirectories(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to ensure MCP server directories: %w", err)
	}

	// Regenerate docker-compose.yaml with updated MCP server configuration
	if err := RegenerateDockerCompose(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to regenerate docker-compose.yaml: %w", err)
	}

	fmt.Printf("✓ Added MCP server '%s' (%s) to kagent.yaml\n", res.Name, res.Type)
	return nil
}

func ensureMcpServerDirectories(projectDir string, manifest *common.AgentManifest, verbose bool) error {
	// Create a separate directory for each command-type MCP server
	for _, srv := range manifest.McpServers {
		// Skip remote type servers as they don't need local directories
		if srv.Type != "command" {
			continue
		}

		// Create directory named after the MCP server
		mcpServerDir := filepath.Join(projectDir, srv.Name)
		if err := os.MkdirAll(mcpServerDir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", srv.Name, err)
		}

		// Transform this specific server into a target for config.yaml template
		targets := []mcpTarget{
			{
				Name:  srv.Name,
				Cmd:   srv.Command,
				Args:  srv.Args,
				Env:   srv.Env,
				Image: srv.Image,
				Build: srv.Build,
			},
		}

		// Render and write config.yaml
		templateData := struct {
			Targets []mcpTarget
		}{
			Targets: targets,
		}

		renderedContent, err := RenderTemplate("templates/mcp_server/config.yaml.tmpl", templateData)
		if err != nil {
			return fmt.Errorf("failed to render config.yaml template for %s: %w", srv.Name, err)
		}

		configPath := filepath.Join(mcpServerDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(renderedContent), 0o644); err != nil {
			return fmt.Errorf("failed to write config.yaml for %s: %w", srv.Name, err)
		}

		if verbose {
			fmt.Printf("Created/updated %s\n", configPath)
		}

		// Copy Dockerfile if it doesn't exist
		if err := CopyTemplateIfNotExists(mcpServerDir, "Dockerfile", "templates/mcp_server/Dockerfile", verbose); err != nil {
			return err
		}
	}

	return nil
}

// parseKeyValuePairs parses KEY=VALUE pairs from a string slice
func parseKeyValuePairs(pairs []string) map[string]string {
	result := make(map[string]string)
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				result[key] = value
			}
		}
	}
	return result
}
