package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initTypeScriptCmd = &cobra.Command{
	Use:   "typescript [project-name]",
	Short: "Initialize a new TypeScript MCP server project",
	Long: `Initialize a new MCP server project using the TypeScript framework.

This command will create a new directory with a basic TypeScript MCP project structure,
including a package.json file, tsconfig.json, and an example tool.`,
	Args: cobra.ExactArgs(1),
	RunE: runInitTypeScript,
}

func init() {
	InitCmd.AddCommand(initTypeScriptCmd)
}

func runInitTypeScript(_ *cobra.Command, args []string) error {
	projectName := args[0]
	framework := "typescript"

	if err := runInitFramework(projectName, framework, nil); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully created TypeScript MCP server project: %s\n", projectName)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("1. cd %s\n", projectName)
	fmt.Printf("2. npm install\n")
	fmt.Printf("3. npm run dev\n")
	fmt.Printf("4. Add tools with: kagent mcp add-tool myTool\n")
	return nil
}
