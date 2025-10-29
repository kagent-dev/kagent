package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	cli "github.com/kagent-dev/kagent/go/cli/internal/cli/agent"
	"github.com/kagent-dev/kagent/go/cli/internal/cli/mcp"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/profiles"
	"github.com/kagent-dev/kagent/go/cli/internal/tui"
	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// listen for signals to cancel the context throughout the application
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-done

		fmt.Fprintf(os.Stderr, "kagent aborted.\n")
		fmt.Fprintf(os.Stderr, "Exiting.\n")

		cancel()
	}()
	cfg := &config.Config{}

	rootCmd := &cobra.Command{
		Use:   "kagent",
		Short: "kagent is a CLI and TUI for kagent",
		Long:  "kagent is a CLI and TUI for kagent",
		Run:   runInteractive,
	}

	rootCmd.PersistentFlags().StringVar(&cfg.KAgentURL, "kagent-url", "http://localhost:8083", "KAgent URL")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", "kagent", "Namespace")
	rootCmd.PersistentFlags().StringVarP(&cfg.OutputFormat, "output-format", "o", "table", "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 300*time.Second, "Timeout")
	installCfg := &cli.InstallCfg{
		Config: cfg,
	}

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install kagent",
		Long:  `Install kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.InstallCmd(cmd.Context(), installCfg)
		},
	}
	installCmd.Flags().StringVar(&installCfg.Profile, "profile", "", "Installation profile (minimal|demo)")
	_ = installCmd.RegisterFlagCompletionFunc("profile", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return profiles.Profiles, cobra.ShellCompDirectiveNoFileComp
	})

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall kagent",
		Long:  `Uninstall kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.UninstallCmd(cmd.Context(), cfg)
		},
	}

	invokeCfg := &cli.InvokeCfg{
		Config: cfg,
	}

	invokeCmd := &cobra.Command{
		Use:   "invoke",
		Short: "Invoke a kagent agent",
		Long:  `Invoke a kagent agent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.InvokeCmd(cmd.Context(), invokeCfg)
		},
		Example: `kagent invoke --agent "k8s-agent" --task "Get all the pods in the kagent namespace"`,
	}

	invokeCmd.Flags().StringVarP(&invokeCfg.Task, "task", "t", "", "Task")
	invokeCmd.Flags().StringVarP(&invokeCfg.Session, "session", "s", "", "Session")
	invokeCmd.Flags().StringVarP(&invokeCfg.Agent, "agent", "a", "", "Agent")
	invokeCmd.Flags().BoolVarP(&invokeCfg.Stream, "stream", "S", false, "Stream the response")
	invokeCmd.Flags().StringVarP(&invokeCfg.File, "file", "f", "", "File to read the task from")
	invokeCmd.Flags().StringVarP(&invokeCfg.URLOverride, "url-override", "u", "", "URL override")
	invokeCmd.Flags().MarkHidden("url-override") //nolint:errcheck

	bugReportCmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Generate a bug report",
		Long:  `Generate a bug report`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := cli.CheckServerConnection(cmd.Context(), cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(cmd.Context(), cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
					return
				}
				defer pf.Stop()
			}
			cli.BugReportCmd(cfg)
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the kagent version",
		Long:  `Print the kagent version`,
		Run: func(cmd *cobra.Command, args []string) {
			// print out kagent CLI version regardless if a port-forward to kagent server succeeds
			// versions unable to obtain from the remote kagent will be reported as "unknown"
			defer cli.VersionCmd(cfg)

			if err := cli.CheckServerConnection(cmd.Context(), cfg.Client()); err != nil {
				if pf, e := cli.NewPortForward(cmd.Context(), cfg); e == nil {
					defer pf.Stop()
				}
			}
		},
	}

	dashboardCmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.DashboardCmd(cmd.Context(), cfg)
		},
	}

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get a kagent resource",
		Long:  `Get a kagent resource`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(os.Stderr, "No resource type provided\n\n")
			cmd.Help() //nolint:errcheck
			os.Exit(1)
		},
	}

	getSessionCmd := &cobra.Command{
		Use:   "session [session_id]",
		Short: "Get a session or list all sessions",
		Long:  `Get a session by ID or list all sessions`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := cli.CheckServerConnection(cmd.Context(), cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(cmd.Context(), cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
					return
				}
				defer pf.Stop()
			}
			resourceName := ""
			if len(args) > 0 {
				resourceName = args[0]
			}
			cli.GetSessionCmd(cfg, resourceName)
		},
	}

	getAgentCmd := &cobra.Command{
		Use:   "agent [agent_name]",
		Short: "Get an agent or list all agents",
		Long:  `Get an agent by name or list all agents`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := cli.CheckServerConnection(cmd.Context(), cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(cmd.Context(), cfg)
				if err != nil {
					return
				}
				defer pf.Stop()
			}
			resourceName := ""
			if len(args) > 0 {
				resourceName = args[0]
			}
			cli.GetAgentCmd(cfg, resourceName)
		},
	}

	getToolCmd := &cobra.Command{
		Use:   "tool",
		Short: "Get tools",
		Long:  `List all available tools`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := cli.CheckServerConnection(cmd.Context(), cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(cmd.Context(), cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
					return
				}
				defer pf.Stop()
			}
			cli.GetToolCmd(cfg)
		},
	}

	getCmd.AddCommand(getSessionCmd, getAgentCmd, getToolCmd)

	initCfg := &cli.InitCfg{
		Config: cfg,
	}

	initCmd := &cobra.Command{
		Use:   "init [framework] [language] [agent-name]",
		Short: "Initialize a new agent project",
		Long: `Initialize a new agent project using the specified framework and language.

You can customize the root agent instructions using the --instruction-file flag.
You can select a specific model using --model-provider and --model-name flags.
If no custom instruction file is provided, a default dice-rolling instruction will be used.
If no model is specified, the agent will need to be configured later.

Examples:
  kagent init adk python dice
  kagent init adk python dice --instruction-file instructions.md
  kagent init adk python dice --model-provider Gemini --model-name gemini-2.0-flash`,
		Args: cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			initCfg.Framework = args[0]
			initCfg.Language = args[1]
			initCfg.AgentName = args[2]

			if err := cli.InitCmd(initCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `kagent init adk python dice`,
	}

	// Add flags for custom instructions and model selection
	initCmd.Flags().StringVar(&initCfg.InstructionFile, "instruction-file", "", "Path to file containing custom instructions for the root agent")
	initCmd.Flags().StringVar(&initCfg.ModelProvider, "model-provider", "Gemini", "Model provider (OpenAI, Anthropic, Gemini)")
	initCmd.Flags().StringVar(&initCfg.ModelName, "model-name", "gemini-2.0-flash", "Model name (e.g., gpt-4, claude-3-5-sonnet, gemini-2.0-flash)")
	initCmd.Flags().StringVar(&initCfg.Description, "description", "", "Description for the agent")

	buildCfg := &cli.BuildCfg{
		Config: cfg,
	}

	buildCmd := &cobra.Command{
		Use:   "build [project-directory]",
		Short: "Build a Docker images for an agent project",
		Long: `Build Docker images for an agent project created with the init command.

This command will look for a kagent.yaml file in the specified project directory and build Docker images using docker build. The images can optionally be pushed to a registry.

Image naming:
- If --image is provided, it will be used as the full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)
- Otherwise, defaults to localhost:5001/{agentName}:latest where agentName is loaded from kagent.yaml

Examples:
  kagent build ./my-agent
  kagent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0
  kagent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0 --push`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			buildCfg.ProjectDir = args[0]

			if err := cli.BuildCmd(buildCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `kagent build ./my-agent`,
	}

	// Add flags for build command
	buildCmd.Flags().StringVar(&buildCfg.Image, "image", "", "Full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)")
	buildCmd.Flags().BoolVar(&buildCfg.Push, "push", false, "Push the image to the registry")
	buildCmd.Flags().StringVar(&buildCfg.Platform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")

	deployCfg := &cli.DeployCfg{
		Config: cfg,
	}

	deployCmd := &cobra.Command{
		Use:   "deploy [project-directory]",
		Short: "Deploy an agent to Kubernetes",
		Long: `Deploy an agent to Kubernetes.

This command will read the kagent.yaml file from the specified project directory,
load environment variables from a .env file, and create an Agent CRD with necessary secrets.

The command will:
1. Load the agent configuration from kagent.yaml
2. Load environment variables from a .env file (including the model provider API key)
3. Create Kubernetes secrets for environment variables and API keys
4. Create an Agent CRD with the appropriate configuration

API Key Requirements:
  The .env file MUST contain the API key for your model provider:
  - Anthropic: ANTHROPIC_API_KEY=your-key-here
  - OpenAI: OPENAI_API_KEY=your-key-here
  - Gemini: GOOGLE_API_KEY=your-key-here

Environment Variables:
  --env-file: REQUIRED. Path to a .env file containing environment variables (including API keys).
              Variables will be stored in a Kubernetes secret and mounted as environment variables.

Dry-Run Mode:
  --dry-run: Output YAML manifests without applying them to the cluster. This is useful
             for previewing changes or for use with GitOps workflows.

Examples:
  kagent deploy ./my-agent --env-file .env
  kagent deploy ./my-agent --env-file .env --image "myregistry/myagent:v1.0"
  kagent deploy ./my-agent --env-file .env --namespace "my-namespace"
  kagent deploy ./my-agent --env-file .env --dry-run > manifests.yaml`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			deployCfg.ProjectDir = args[0]

			if err := cli.DeployCmd(cmd.Context(), deployCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `kagent deploy ./my-agent --env-file .env`,
	}

	// Add flags for deploy command
	deployCmd.Flags().StringVarP(&deployCfg.Image, "image", "i", "", "Image to use (defaults to localhost:5001/{agentName}:latest)")
	deployCmd.Flags().StringVar(&deployCfg.EnvFile, "env-file", "", "Path to .env file containing environment variables (including API keys)")
	deployCmd.Flags().StringVar(&deployCfg.Config.Namespace, "namespace", "", "Kubernetes namespace to deploy to")
	deployCmd.Flags().BoolVar(&deployCfg.DryRun, "dry-run", false, "Output YAML manifests without applying them to the cluster")
	deployCmd.Flags().StringVar(&deployCfg.Platform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")

	// add-mcp command
	addMcpCfg := &cli.AddMcpCfg{Config: cfg}
	addMcpCmd := &cobra.Command{
		Use:   "add-mcp [name] [args...]",
		Short: "Add an MCP server entry to kagent.yaml",
		Long:  `Add an MCP server entry to kagent.yaml. Use flags for non-interactive setup or run without flags to open the wizard.`,
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				addMcpCfg.Name = args[0]
				if len(args) > 1 && addMcpCfg.Command != "" {
					addMcpCfg.Args = append(addMcpCfg.Args, args[1:]...)
				}
			}
			if err := cli.AddMcpCmd(addMcpCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	// Flags for non-interactive usage
	addMcpCmd.Flags().StringVar(&addMcpCfg.ProjectDir, "project-dir", "", "Project directory (default: current directory)")
	addMcpCmd.Flags().StringVar(&addMcpCfg.RemoteURL, "remote", "", "Remote MCP server URL (http/https)")
	addMcpCmd.Flags().StringSliceVar(&addMcpCfg.Headers, "header", nil, "HTTP header for remote MCP in KEY=VALUE format (repeatable, supports ${VAR} for env vars)")
	addMcpCmd.Flags().StringVar(&addMcpCfg.Command, "command", "", "Command to run MCP server (e.g., npx, uvx, kmcp, or a binary)")
	addMcpCmd.Flags().StringSliceVar(&addMcpCfg.Args, "arg", nil, "Command argument (repeatable)")
	addMcpCmd.Flags().StringSliceVar(&addMcpCfg.Env, "env", nil, "Environment variable in KEY=VALUE format (repeatable)")
	addMcpCmd.Flags().StringVar(&addMcpCfg.Image, "image", "", "Container image (optional; mutually exclusive with --build)")
	addMcpCmd.Flags().StringVar(&addMcpCfg.Build, "build", "", "Container build (optional; mutually exclusive with --image)")

	runCfg := &cli.RunCfg{
		Config: cfg,
	}

	runCmd := &cobra.Command{
		Use:   "run [project-directory]",
		Short: "Run agent project locally with docker-compose and launch chat interface",
		Long: `Run an agent project locally using docker-compose and launch an interactive chat session.

Examples:
  kagent run ./my-agent
  kagent run .`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				runCfg.ProjectDir = args[0]
			} else {
				runCfg.ProjectDir = "."
			}

			if err := cli.RunCmd(cmd.Context(), runCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `kagent run ./my-agent`,
	}

	runCmd.Flags().StringVar(&runCfg.ProjectDir, "project-dir", "", "Project directory (default: current directory)")

	rootCmd.AddCommand(installCmd, uninstallCmd, invokeCmd, bugReportCmd, versionCmd, dashboardCmd, getCmd, initCmd, buildCmd, deployCmd, addMcpCmd, runCmd, mcp.NewMCPCmd())

	// Initialize config
	if err := config.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)

		os.Exit(1)
	}

}

func runInteractive(cmd *cobra.Command, args []string) {
	cfg, err := config.Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting config: %v\n", err)
		os.Exit(1)
	}

	client := cfg.Client()

	// Start port forward and ensure it is healthy.
	var pf *cli.PortForward
	if err := cli.CheckServerConnection(cmd.Context(), client); err != nil {
		pf, err = cli.NewPortForward(cmd.Context(), cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			return
		}
		defer pf.Stop()
	}

	if err := tui.RunWorkspace(cfg, cfg.Client(), cfg.Verbose); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
	}
}
