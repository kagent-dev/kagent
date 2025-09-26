package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initJavaCmd = &cobra.Command{
	Use:   "java [project-name]",
	Short: "Initialize a new Java MCP server project",
	Long: `Initialize a new MCP server project using the Java framework.

This command will create a new directory with a basic Java MCP project structure,
including a pom.xml file, Maven project structure, and an example tool.`,
	Args: cobra.ExactArgs(1),
	RunE: runInitJava,
}

func init() {
	InitCmd.AddCommand(initJavaCmd)
}

func runInitJava(_ *cobra.Command, args []string) error {
	projectName := args[0]
	framework := frameworkJava

	if err := runInitFramework(projectName, framework, nil); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully created Java MCP server project: %s\n", projectName)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("1. cd %s\n", projectName)
	fmt.Printf("2. mvn clean install\n")
	fmt.Printf("3. mvn exec:java -Dexec.mainClass=\"com.example.Main\"\n")
	fmt.Printf("4. Add tools with: kagent mcp add-tool my-tool\n")
	return nil
}
