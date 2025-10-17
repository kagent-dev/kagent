package mcp

import (
	"fmt"

	commonprompt "github.com/kagent-dev/kagent/go/cli/internal/common/prompt"
	"github.com/kagent-dev/kagent/go/cli/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	goModuleName string
)

var initGoCmd = &cobra.Command{
	Use:   "go [project-name]",
	Short: "Initialize a new Go MCP server project",
	Long: `Initialize a new MCP server project using the mcp-go framework.

This command will create a new directory with a basic mcp-go project structure,
including a go.mod file, a main.go file, and an example tool.

You must provide a valid Go module name for the project.`,
	Args: cobra.ExactArgs(1),
	RunE: runInitGo,
}

func init() {
	InitCmd.AddCommand(initGoCmd)
	initGoCmd.Flags().StringVar(
		&goModuleName,
		"go-module-name",
		"",
		"The Go module name for the project (e.g., github.com/my-org/my-project)",
	)
}

func runInitGo(_ *cobra.Command, args []string) error {
	projectName := args[0]
	framework := frameworkMCPGo

	customize := func(p *mcp.ProjectConfig) error {
		// Interactively get module name if not provided
		if goModuleName == "" && !initNonInteractive {
			var err error
			goModuleName, err = commonprompt.PromptForInput("Enter Go module name (e.g., github.com/my-org/my-project): ")
			if err != nil {
				return fmt.Errorf("failed to read module name: %w", err)
			}
		}
		if goModuleName == "" {
			return fmt.Errorf("--module-name is required")
		}
		p.GoModuleName = goModuleName
		return nil
	}

	if err := runInitFramework(projectName, framework, customize); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully created Go MCP server project: %s\n", projectName)
	return nil
}
