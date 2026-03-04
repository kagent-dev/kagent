package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initPythonCmd = &cobra.Command{
	Use:   "python [project-name]",
	Short: "Initialize a new Python MCP server project",
	Long: `Initialize a new MCP server project using the fastmcp-python framework.

This command will create a new directory with a basic fastmcp-python project structure,
including a pyproject.toml file, a main.py file, and an example tool.`,
	Args: cobra.ExactArgs(1),
	RunE: runInitPython,
}

func init() {
	InitCmd.AddCommand(initPythonCmd)
}

func runInitPython(_ *cobra.Command, args []string) error {
	projectName := args[0]
	framework := frameworkFastMCPPython

	if err := runInitFramework(projectName, framework, nil); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully created Python MCP server project: %s\n", projectName)
	return nil
}
