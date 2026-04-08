package mcp

import (
	"fmt"

	commonprompt "github.com/kagent-dev/kagent/go/core/cli/internal/common/prompt"
	"github.com/kagent-dev/kagent/go/core/cli/internal/mcp"
	"github.com/spf13/cobra"
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
	mcpCmd.AddCommand(newDeployCmd())
	mcpCmd.AddCommand(newAddToolCmd())
	mcpCmd.AddCommand(newRunCmd())
	mcpCmd.AddCommand(newSecretsCmd())

	return mcpCmd
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
		RunE: func(_ *cobra.Command, _ []string) error {
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

func newDeployCmd() *cobra.Command {
	cfg := &DeployCfg{}

	currentNamespace := GetCurrentNamespace()

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy MCP server to Kubernetes",
		Long: `Deploy an MCP server to Kubernetes by generating MCPServer CRDs.

This command generates MCPServer Custom Resource Definitions (CRDs) based on:
- Project configuration from manifest.yaml
- Docker image built with 'kagent mcp build --docker'
- Deployment configuration options

The generated MCPServer will include:
- Docker image reference from the build
- Transport configuration (stdio/http)
- Port and command configuration
- Environment variables and secrets

The command can also apply Kubernetes secret YAML files to the cluster before deploying the MCPServer.
The secrets will be referenced in the MCPServer CRD for mounting as volumes to the MCP server container.
Secret namespace will be overridden with the deployment namespace to avoid the need for reference grants
to enable cross-namespace references.

Examples:
  kagent mcp deploy                               # Deploy with project name to cluster
  kagent mcp deploy my-server                     # Deploy with custom name
  kagent mcp deploy --namespace staging           # Deploy to staging namespace
  kagent mcp deploy --dry-run                     # Generate manifest without applying to cluster
  kagent mcp deploy --image custom:tag            # Use custom image
  kagent mcp deploy --transport http              # Use HTTP transport
  kagent mcp deploy --output deploy.yaml          # Save to file
  kagent mcp deploy --file /path/to/manifest.yaml # Use custom manifest.yaml file
  kagent mcp deploy --environment staging         # Target environment for deployment (e.g., staging, production)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return DeployMcp(cfg)
		},
	}

	cmd.Flags().StringVarP(&cfg.Namespace, "namespace", "n", currentNamespace, "Kubernetes namespace")
	cmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Generate manifest without applying to cluster")
	cmd.Flags().StringVarP(&cfg.Output, "output", "", "", "Output file for the generated YAML")
	cmd.Flags().StringVar(&cfg.Image, "image", "", "Docker image to deploy (overrides build image)")
	cmd.Flags().StringVar(&cfg.Transport, "transport", "", "Transport type (stdio, http)")
	cmd.Flags().IntVar(&cfg.Port, "port", 0, "Container port (default: from project config)")
	cmd.Flags().StringVar(&cfg.Command, "command", "", "Command to run (overrides project config)")
	cmd.Flags().StringSliceVar(&cfg.Args, "args", []string{}, "Command arguments")
	cmd.Flags().StringSliceVar(&cfg.Env, "env", []string{}, "Environment variables (KEY=VALUE)")
	cmd.Flags().BoolVar(&cfg.Force, "force", false, "Force deployment even if validation fails")
	cmd.Flags().StringVarP(&cfg.File, "file", "f", "", "Path to manifest.yaml file (default: current directory)")
	cmd.Flags().BoolVar(&cfg.NoInspector, "no-inspector", true, "Do not start the MCP inspector after deployment")
	cmd.Flags().StringVar(&cfg.Environment, "environment", "staging",
		"Target environment for deployment (e.g., staging, production)")

	// Package subcommand
	packageCfg := &DeployCfg{}
	packageCmd := &cobra.Command{
		Use:   "package",
		Short: "Deploy an MCP server using a package manager (npx, uvx)",
		Long: `Deploy an MCP server using a package manager to run Model Context Protocol servers.

This subcommand creates an MCPServer Custom Resource Definition (CRD) that runs
an MCP server using npx (for npm packages) or uvx (for Python packages).

The deployment name, manager, and args are required. The package manager must be either 'npx' or 'uvx'.

Examples:
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github                             # Deploy GitHub MCP server
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github  --dry-run                  # Print YAML without deploying
  kagent mcp deploy package --deployment-name my-server --manager npx --args my-package --env "KEY1=value1,KEY2=value2"                          # Set environment variables
  kagent mcp deploy package --deployment-name github-server --manager npx --args @modelcontextprotocol/server-github  --secrets secret1,secret2  # Mount Kubernetes secrets
  kagent mcp deploy package --deployment-name my-server --manager npx --args my-package --no-inspector                                           # Deploy without starting inspector
  kagent mcp deploy package --deployment-name my-server --manager uvx --args mcp-server-git                                                      # Use UV and write managed tools and installables to /tmp directories`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return PackageDeployMcp(packageCfg)
		},
	}

	packageCmd.Flags().StringVar(&packageCfg.PackageName, "deployment-name", "", "Name for the deployment (required)")
	packageCmd.Flags().StringVar(&packageCfg.PackageManager, "manager", "", "Package manager to use (npx or uvx) (required)")
	packageCmd.Flags().StringSliceVar(&packageCfg.PackageSecrets, "secrets", []string{}, "List of Kubernetes secret names to mount")
	packageCmd.Flags().StringSliceVar(&packageCfg.Args, "args", []string{}, "Arguments to pass to the package manager (e.g., package names) (required)")
	packageCmd.Flags().StringSliceVar(&packageCfg.Env, "env", []string{}, "Environment variables (KEY=VALUE)")
	packageCmd.Flags().BoolVar(&packageCfg.DryRun, "dry-run", false, "Generate manifest without applying to cluster")
	packageCmd.Flags().StringVarP(&packageCfg.Namespace, "namespace", "n", "", "Kubernetes namespace")
	packageCmd.Flags().StringVar(&packageCfg.Image, "image", "", "Docker image to deploy (overrides default)")
	packageCmd.Flags().StringVar(&packageCfg.Transport, "transport", "", "Transport type (stdio, http)")
	packageCmd.Flags().IntVar(&packageCfg.Port, "port", 0, "Container port (default: 3000)")
	packageCmd.Flags().BoolVar(&packageCfg.NoInspector, "no-inspector", true, "Do not start the MCP inspector after deployment")
	packageCmd.Flags().StringVarP(&packageCfg.Output, "output", "", "", "Output file for the generated YAML")

	_ = packageCmd.MarkFlagRequired("deployment-name")
	_ = packageCmd.MarkFlagRequired("manager")
	_ = packageCmd.MarkFlagRequired("args")

	cmd.AddCommand(packageCmd)

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
		RunE: func(_ *cobra.Command, args []string) error {
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
		RunE: func(_ *cobra.Command, _ []string) error {
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
		RunE: func(_ *cobra.Command, args []string) error {
			return SyncSecretsMcp(cfg, args[0])
		},
	}

	syncCmd.Flags().StringVar(&cfg.SourceFile, "from-file", ".env", "Source .env file to sync from")
	syncCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Output the generated secret YAML instead of applying it")
	syncCmd.Flags().StringVarP(&cfg.ProjectDir, "project-dir", "d", "", "Project directory (default: current directory)")

	cmd.AddCommand(syncCmd)

	return cmd
}
