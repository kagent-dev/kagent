package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	pygen "github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/adk/python"
	"github.com/kagent-dev/kagent/go/cli/internal/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/tui/dialogs"
)

// AddMcpCfg carries inputs for adding an MCP server entry to kagent.yaml
type AddMcpCfg struct {
	ProjectDir string
	Config     *config.Config
	// Non-interactive fields
	Name      string
	RemoteURL string
	Command   string
	Args      []string
	Env       []string
	Image     string
	Build     string
}

// AddMcpCmd runs the interactive flow to append an MCP server to kagent.yaml
func AddMcpCmd(cfg *AddMcpCfg) error {
	// Determine project directory
	projectDir := cfg.ProjectDir
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		projectDir = cwd
	} else if !filepath.IsAbs(projectDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		projectDir = filepath.Join(cwd, projectDir)
	}

	// Load manifest
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err != nil {
		return fmt.Errorf("failed to load kagent.yaml: %w", err)
	}

	if cfg.Config != nil && cfg.Config.Verbose {
		fmt.Printf("Loaded manifest for agent '%s' from %s\n", manifest.Name, projectDir)
	}

	// If flags provided, build non-interactively; else run wizard
	var res common.McpServerType
	if cfg.RemoteURL != "" || cfg.Command != "" || cfg.Image != "" || cfg.Build != "" {
		if cfg.RemoteURL != "" {
			res = common.McpServerType{
				Type: "remote",
				URL:  cfg.RemoteURL,
				Name: cfg.Name,
				Env:  cfg.Env,
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
	serverType := res.Type
	name := res.Name

	// Ensure unique name
	for _, existing := range manifest.McpServers {
		if strings.EqualFold(existing.Name, name) {
			return fmt.Errorf("an MCP server named '%s' already exists in kagent.yaml", name)
		}
	}

	image := res.Image
	build := res.Build
	command := res.Command
	url := res.URL
	args := res.Args
	env := res.Env

	// Construct new entry
	newServer := common.McpServerType{
		Type:    serverType,
		Name:    name,
		Image:   image,
		Build:   build,
		Command: command,
		Args:    args,
		Env:     env,
		URL:     url,
	}

	// Append and validate
	manifest.McpServers = append(manifest.McpServers, newServer)
	if err := manager.Validate(manifest); err != nil {
		return fmt.Errorf("invalid MCP server configuration: %w", err)
	}

	// Save back to disk
	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save kagent.yaml: %w", err)
	}

	// Create mcp_tools.py on first MCP server addition for ADK Python projects
	if err := ensureMcpToolsFile(projectDir, manifest.Name, cfg.Config != nil && cfg.Config.Verbose); err != nil {
		return fmt.Errorf("failed to ensure mcp_tools.py: %w", err)
	}

	fmt.Printf("✓ Added MCP server '%s' (%s) to kagent.yaml\n", name, serverType)
	return nil
}

// ensureMcpToolsFile creates a default mcp_tools.py next to agent.py if it doesn't exist yet.
func ensureMcpToolsFile(projectDir, agentName string, verbose bool) error {
	// Expected agent directory for ADK Python: <projectDir>/<agentName>
	agentDir := filepath.Join(projectDir, agentName)
	if _, err := os.Stat(agentDir); err != nil {
		// If not present, nothing to do (not an ADK Python layout)
		return nil
	}
	target := filepath.Join(agentDir, "mcp_tools.py")
	if _, err := os.Stat(target); err == nil {
		// Already exists
		return nil
	}

	gen := pygen.NewPythonGenerator()
	tmplBytes, err := fs.ReadFile(gen.BaseGenerator.TemplateFiles, "templates/agent/mcp_tools.py.tmpl")
	if err != nil {
		return err
	}

	if err := os.WriteFile(target, tmplBytes, 0o644); err != nil {
		return err
	}
	if verbose {
		fmt.Printf("Created %s\n", target)
	}
	return nil
}
