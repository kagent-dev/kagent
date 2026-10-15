package mcp

import (
	"fmt"

	"github.com/spf13/cobra"

	commonprompt "github.com/kagent-dev/kagent/go/core/cli/internal/common/prompt"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp"
)

// NewMCPCmd creates the root MCP command with all subcommands
func NewMCPCmd() *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP (Model Context Protocol) server management",
		Long: `MCP server management commands for creating and managing
Model Context Protocol servers with dynamic tool loading.`,
	}

	mcpCmd.AddCommand(newInitCmd())
	mcpCmd.AddCommand(newBuildCmd())
	mcpCmd.AddCommand(newAddToolCmd())
	mcpCmd.AddCommand(newRunCmd())
	mcpCmd.AddCommand(newSecretsCmd())

	return mcpCmd
}

func setVerbose(cmd *cobra.Command, target *bool) error {
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return err
	}
	*target = verbose
	return nil
}

func newInitCmd() *cobra.Command {
	cfg := &InitMcpCfg{}

	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: "Initialize a new MCP server project",
		Long: `Initialize a new MCP server project with dynamic tool loading.

This command provides subcommands to initialize a new MCP server project
using one of the supported frameworks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return nil
		},
	}

	cmd.PersistentFlags().BoolVar(&cfg.Force, "force", false, "Overwrite existing directory")
	cmd.PersistentFlags().BoolVar(&cfg.NoGit, "no-git", false, "Skip git initialization")
	cmd.PersistentFlags().StringVar(&cfg.Author, "author", "", "Author name for the project")
	cmd.PersistentFlags().StringVar(&cfg.Email, "email", "", "Author email for the project")
	cmd.PersistentFlags().StringVar(&cfg.Description, "description", "", "Description for the project")
	cmd.PersistentFlags().BoolVar(&cfg.NonInteractive, "non-interactive", false, "Run in non-interactive mode")
	cmd.PersistentFlags().StringVar(&cfg.Namespace, "namespace", "default", "Default namespace for project resources")

	// Python subcommand
	pythonCmd := &cobra.Command{
		Use:   "python [project-name]",
		Short: "Initialize a new Python MCP server project",
		Long: `Initialize a new MCP server project using the fastmcp-python framework.

This command will create a new directory with a basic fastmcp-python project structure,
including a pyproject.toml file, a main.py file, and an example tool.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := InitMcp(cfg, args[0], frameworkFastMCPPython, nil); err != nil {
				return err
			}
			fmt.Printf("✓ Successfully created Python MCP server project: %s\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(pythonCmd)

	// Go subcommand
	var goModuleName string
	goCmd := &cobra.Command{
		Use:   "go [project-name]",
		Short: "Initialize a new Go MCP server project",
		Long: `Initialize a new MCP server project using the mcp-go framework.

This command will create a new directory with a basic mcp-go project structure,
including a go.mod file, a main.go file, and an example tool.

You must provide a valid Go module name for the project.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			projectName := args[0]

			customize := func(p *mcp.ProjectConfig) error {
				if goModuleName == "" && !cfg.NonInteractive {
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

			if err := InitMcp(cfg, projectName, frameworkMCPGo, customize); err != nil {
				return err
			}
			fmt.Printf("✓ Successfully created Go MCP server project: %s\n", projectName)
			return nil
		},
	}
	goCmd.Flags().StringVar(&goModuleName, "go-module-name", "", "The Go module name for the project (e.g., github.com/my-org/my-project)")
	cmd.AddCommand(goCmd)

	// Java subcommand
	javaCmd := &cobra.Command{
		Use:   "java [project-name]",
		Short: "Initialize a new Java MCP server project",
		Long: `Initialize a new MCP server project using the Java MCP framework.

This command will create a new directory with a basic Java MCP project structure,
including a pom.xml file, a Main.java file, and an example tool.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := InitMcp(cfg, args[0], frameworkJava, nil); err != nil {
				return err
			}
			fmt.Printf("✓ Successfully created Java MCP server project: %s\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(javaCmd)

	// TypeScript subcommand
	tsCmd := &cobra.Command{
		Use:   "typescript [project-name]",
		Short: "Initialize a new TypeScript MCP server project",
		Long: `Initialize a new MCP server project using the TypeScript MCP framework.

This command will create a new directory with a basic TypeScript MCP project structure,
including a package.json file, a tsconfig.json file, and an example tool.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := InitMcp(cfg, args[0], frameworkTypeScript, nil); err != nil {
				return err
			}
			fmt.Printf("✓ Successfully created TypeScript MCP server project: %s\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(tsCmd)

	return cmd
}

func newBuildCmd() *cobra.Command {
	cfg := &BuildCfg{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build MCP server as a Docker image",
		Long: `Build an MCP server from the current project.

This command will detect the project type and build the appropriate
MCP server Docker image.

Examples:
  kagent mcp build                    # Build Docker image from current directory
  kagent mcp build --project-dir ./my-project  # Build Docker image from specific directory`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := setVerbose(cmd, &cfg.Verbose); err != nil {
				return err
			}
			return BuildMcp(cfg)
		},
	}

	cmd.Flags().StringVarP(&cfg.Tag, "tag", "t", "", "Docker image tag (alias for --output)")
	cmd.Flags().BoolVar(&cfg.Push, "push", false, "Push Docker image to registry")
	cmd.Flags().BoolVar(&cfg.KindLoad, "kind-load", false, "Load image into kind cluster (requires kind)")
	cmd.Flags().StringVar(&cfg.KindLoadCluster, "kind-load-cluster", "",
		"Name of the kind cluster to load image into (default: current cluster)")
	cmd.Flags().StringVarP(&cfg.ProjectDir, "project-dir", "d", "", "Build directory (default: current directory)")
	cmd.Flags().StringVar(&cfg.Platform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")

	return cmd
}

func newAddToolCmd() *cobra.Command {
	cfg := &AddToolCfg{}

	cmd := &cobra.Command{
		Use:   "add-tool [tool-name]",
		Short: "Add a new MCP tool to your project",
		Long: `Generate a new MCP tool that will be automatically loaded by the server.

This command creates a new tool file in src/tools/ with a generic template.
The tool will be automatically discovered and loaded when the server starts.

Each tool is a Python file containing a function decorated with @mcp.tool().
The function should use the @mcp.tool() decorator from FastMCP.

Examples:
  kagent mcp add-tool weather
  kagent mcp add-tool database --description "Database operations tool"
  kagent mcp add-tool weather --force  # Overwrite existing tool
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := setVerbose(cmd, &cfg.Verbose); err != nil {
				return err
			}
			return AddToolMcp(cfg, args[0])
		},
	}

	cmd.Flags().StringVarP(&cfg.Description, "description", "d", "", "Tool description")
	cmd.Flags().BoolVarP(&cfg.Force, "force", "f", false, "Overwrite existing tool file")
	cmd.Flags().BoolVarP(&cfg.Interactive, "interactive", "i", false, "Interactive tool creation")
	cmd.Flags().StringVar(&cfg.ProjectDir, "project-dir", "", "Project directory (default: current directory)")

	return cmd
}

func newRunCmd() *cobra.Command {
	cfg := &RunCfg{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run MCP server locally",
		Long: `Run an MCP server locally using the Model Context Protocol inspector.

By default, this command will:
1. Load the manifest.yaml configuration from the project directory
2. Determine the framework type and create the appropriate mcp inspector configuration
3. Launch the MCP inspector and select STDIO as the transport type, the server will start when you click "Connect"

If you want to run the server directly without the inspector, use the --no-inspector flag.
This will execute the server directly using the appropriate framework command.

Supported frameworks:
- fastmcp-python: Requires uv to be installed
- mcp-go: Requires Go to be installed

Examples:
  kagent run mcp --project-dir ./my-project     # Run with inspector (default)
  kagent run mcp --no-inspector                 # Run server directly without inspector
  kagent run mcp --transport http               # Run with HTTP transport`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := setVerbose(cmd, &cfg.Verbose); err != nil {
				return err
			}
			return RunMcp(cfg)
		},
	}

	cmd.Flags().StringVarP(&cfg.ProjectDir, "project-dir", "d", "",
		"Project directory to use (default: current directory)")
	cmd.Flags().BoolVar(&cfg.NoInspector, "no-inspector", false,
		"Run the server directly without launching the MCP inspector")
	cmd.Flags().StringVar(&cfg.Transport, "transport", "stdio",
		"Transport mode (stdio or http)")

	return cmd
}

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage project secrets",
		Long:  `Manage secrets for MCP server projects.`,
	}

	cfg := &SecretsCfg{}

	syncCmd := &cobra.Command{
		Use:   "sync [environment]",
		Short: "Sync secrets to a Kubernetes environment from a local .env file",
		Long: `Sync secrets from a local .env file to a Kubernetes secret.

This command reads a .env file and the project's manifest.yaml file to determine
the correct secret name and namespace for the specified environment. It then
creates or updates the Kubernetes secret directly in the cluster.

The command will look for a ".env" file in the project root by default.

Examples:
  # Sync secrets to the "staging" environment defined in manifest.yaml
  kagent mcp secrets sync staging

  # Sync secrets from a custom .env file
  kagent mcp secrets sync staging --from-file .env.staging

  # Sync secrets from a specific project directory
  kagent mcp secrets sync staging --project-dir ./my-project

  # Perform a dry run to see the generated secret without applying it
  kagent mcp secrets sync production --dry-run
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return SyncSecretsMcp(cmd.Context(), cfg, args[0])
		},
	}

	syncCmd.Flags().StringVar(&cfg.SourceFile, "from-file", ".env", "Source .env file to sync from")
	syncCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Output the generated secret YAML instead of applying it")
	syncCmd.Flags().StringVarP(&cfg.ProjectDir, "project-dir", "d", "", "Project directory (default: current directory)")

	cmd.AddCommand(syncCmd)

	return cmd
}
