package mcp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/frameworks"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp/manifests"
)

const (
	frameworkFastMCPPython = "fastmcp-python"
	frameworkMCPGo         = "mcp-go"
	frameworkTypeScript    = "typescript"
	frameworkJava          = "java"
)

// InitMcpCfg contains configuration for MCP init command
type InitMcpCfg struct {
	Force          bool
	NoGit          bool
	Author         string
	Email          string
	Description    string
	NonInteractive bool
	Namespace      string
}

var InitCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new MCP server project",
	Long: `Initialize a new MCP server project with dynamic tool loading.

This command provides subcommands to initialize a new MCP server project
using one of the supported frameworks.`,
	RunE: runInit,
}

var initMcpCfg = &InitMcpCfg{}

func init() {
	InitCmd.PersistentFlags().BoolVar(&initMcpCfg.Force, "force", false, "Overwrite existing directory")
	InitCmd.PersistentFlags().BoolVar(&initMcpCfg.NoGit, "no-git", false, "Skip git initialization")
	InitCmd.PersistentFlags().StringVar(&initMcpCfg.Author, "author", "", "Author name for the project")
	InitCmd.PersistentFlags().StringVar(&initMcpCfg.Email, "email", "", "Author email for the project")
	InitCmd.PersistentFlags().StringVar(&initMcpCfg.Description, "description", "", "Description for the project")
	InitCmd.PersistentFlags().BoolVar(&initMcpCfg.NonInteractive, "non-interactive", false, "Run in non-interactive mode")
	InitCmd.PersistentFlags().StringVar(&initMcpCfg.Namespace, "namespace", "default", "Default namespace for project resources")
}

func runInit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	return nil
}

func runInitFramework(
	cfg *InitMcpCfg,
	projectName, framework string,
	customizeProjectConfig func(*mcp.ProjectConfig) error,
) error {
	// Validate project name
	if err := validateProjectName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	if !cfg.NonInteractive {
		if cfg.Description == "" {
			cfg.Description = promptForDescription()
		}
		if cfg.Author == "" {
			cfg.Author = promptForAuthor()
		}
		if cfg.Email == "" {
			cfg.Email = promptForEmail()
		}
	}

	// Create project manifest
	projectmanifests := manifests.GetDefault(projectName, framework, cfg.Description, cfg.Author, cfg.Email, cfg.Namespace)

	// Check if directory exists
	projectPath, err := filepath.Abs(projectName)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for project: %w", err)
	}

	// Create project configuration
	projectConfig := mcp.ProjectConfig{
		ProjectName: projectmanifests.Name,
		Version:     projectmanifests.Version,
		Description: projectmanifests.Description,
		Author:      projectmanifests.Author,
		Email:       projectmanifests.Email,
		Tools:       projectmanifests.Tools,
		Secrets:     projectmanifests.Secrets,
		Directory:   projectPath,
		NoGit:       cfg.NoGit,
	}

	// Customize project config for the specific framework
	if customizeProjectConfig != nil {
		if err := customizeProjectConfig(&projectConfig); err != nil {
			return fmt.Errorf("failed to customize project config: %w", err)
		}
	}

	// Generate project files
	generator, err := frameworks.GetGenerator(framework)
	if err != nil {
		return err
	}
	if err := generator.GenerateProject(projectConfig); err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	fmt.Printf("  To run the server locally:\n")
	fmt.Printf("  kagent mcp run local --project-dir %s\n", projectPath)

	return manifests.NewManager(projectPath).Save(projectmanifests)
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	// Check for invalid characters
	if strings.ContainsAny(name, " \t\n\r/\\:*?\"<>|") {
		return fmt.Errorf("project name contains invalid characters")
	}

	// Check if it starts with a dot
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("project name cannot start with a dot")
	}

	return nil
}

// Prompts for user input

func promptForAuthor() string {
	fmt.Print("Enter author name (optional): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

func promptForEmail() string {
	for {
		fmt.Print("Enter author email (optional): ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}
		email := strings.TrimSpace(input)

		// If empty, allow it (optional field)
		if email == "" {
			return email
		}

		// Basic email validation
		if isValidEmail(email) {
			return email
		}

		fmt.Println("Invalid email format. Please enter a valid email address or leave empty.")
	}
}

func promptForDescription() string {
	fmt.Print("Enter description (optional): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

// isValidEmail performs basic email validation
func isValidEmail(email string) bool {
	// Basic email regex pattern
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}
