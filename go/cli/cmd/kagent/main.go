package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/abiosoft/ishell/v2"
	"github.com/kagent-dev/kagent/go/cli/internal/cli"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/profiles"
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
		Short: "kagent is a CLI for kagent",
		Long:  "kagent is a CLI for kagent",
		Run: func(cmd *cobra.Command, args []string) {
			runInteractive()
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfg.KAgentURL, "kagent-url", "http://localhost:8083", "KAgent URL")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", "kagent", "Namespace")
	rootCmd.PersistentFlags().StringVarP(&cfg.OutputFormat, "output-format", "o", "table", "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Verbose output")

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
			if err := cli.CheckServerConnection(cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(ctx, cfg)
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
			if err := cli.CheckServerConnection(cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(ctx, cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
					return
				}
				defer pf.Stop()
			}
			cli.VersionCmd(cfg)
		},
	}

	dashboardCmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the kagent dashboard",
		Long:  `Open the kagent dashboard`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.DashboardCmd(ctx, cfg)
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
			if err := cli.CheckServerConnection(cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(ctx, cfg)
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
			if err := cli.CheckServerConnection(cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(ctx, cfg)
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
			if err := cli.CheckServerConnection(cfg.Client()); err != nil {
				pf, err := cli.NewPortForward(ctx, cfg)
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
		Short: "Build a Docker image for an agent project",
		Long: `Build a Docker image for an agent project created with the init command.

This command will look for a Dockerfile in the specified project directory and build
a Docker image using docker build. The image can optionally be pushed to a registry.

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

	deployCfg := &cli.DeployCfg{
		Config: cfg,
	}

	deployCmd := &cobra.Command{
		Use:   "deploy [project-directory]",
		Short: "Deploy an agent to Kubernetes",
		Long: `Deploy an agent to Kubernetes.

This command will read the kagent.yaml file from the specified project directory,
create or reference a Kubernetes secret with the API key, and create an Agent CRD.

The command will:
1. Load the agent configuration from kagent.yaml
2. Either create a new secret with the provided API key or verify an existing secret
3. Create an Agent CRD with the appropriate configuration

API Key Options:
  --api-key: Convenience option to create a new secret with the provided API key
  --api-key-secret: Canonical way to reference an existing secret by name

Examples:
  kagent deploy ./my-agent --api-key-secret "my-existing-secret"
  kagent deploy ./my-agent --api-key "your-api-key-here" --image "myregistry/myagent:v1.0"
  kagent deploy ./my-agent --api-key-secret "my-secret" --namespace "my-namespace"`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			deployCfg.ProjectDir = args[0]

			if err := cli.DeployCmd(ctx, deployCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `kagent deploy ./my-agent --api-key-secret "my-existing-secret"`,
	}

	// Add flags for deploy command
	deployCmd.Flags().StringVarP(&deployCfg.Image, "image", "i", "", "Image to use (defaults to localhost:5001/{agentName}:latest)")
	deployCmd.Flags().StringVar(&deployCfg.APIKey, "api-key", "", "API key for the model provider (convenience option to create secret)")
	deployCmd.Flags().StringVar(&deployCfg.APIKeySecret, "api-key-secret", "", "Name of existing secret containing API key")
	deployCmd.Flags().StringVar(&deployCfg.Config.Namespace, "namespace", "", "Kubernetes namespace to deploy to")

	rootCmd.AddCommand(installCmd, uninstallCmd, invokeCmd, bugReportCmd, versionCmd, dashboardCmd, getCmd, initCmd, buildCmd, deployCmd)

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

const (
	portForwardKey = "[port-forward]"
)

func runInteractive() {
	cfg, err := config.Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting config: %v\n", err)
		os.Exit(1)
	}

	client := cfg.Client()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start port forward and ensure it is healthy.
	var pf *cli.PortForward
	hasPortForward := true
	if err := cli.CheckServerConnection(client); err != nil {
		pf, err = cli.NewPortForward(ctx, cfg)
		if err != nil {
			// For interactive mode, we don't want to exit the program if the port-forward fails.
			// It is possible to open the interactive shell while kagent is not installed, which would mean that we can't port-forward.

			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			hasPortForward = false
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	// create new shell.
	// by default, new shell includes 'exit', 'help' and 'clear' commands.
	shell := ishell.New()
	if pf != nil {
		shell.Set(portForwardKey, pf)
	}

	config.SetHistoryPath(homeDir, shell)
	if err := shell.ClearScreen(); err != nil {
		fmt.Fprintf(os.Stderr, "Error clearing screen: %v\n", err)
	}

	if !hasPortForward {
		shell.Println(cli.ErrServerConnection)
	}
	shell.Println("Welcome to kagent CLI. Type 'help' to see available commands.", strings.Repeat(" ", 10))

	config.SetCfg(shell, cfg)
	config.SetClient(shell, client)
	shell.SetPrompt(config.BoldBlue("kagent >> "))

	runCmd := &ishell.Cmd{
		Name:    "run",
		Aliases: []string{"r"},
		Help:    "Run a kagent agent",
		LongHelp: `Run a kagent agent.

The available run types are:
- chat: Start a chat with a kagent agent.

Examples:
- run chat [agent_name] -s [session_name]
- run chat
  `,
	}

	runCmd.AddCmd(&ishell.Cmd{
		Name:    "chat",
		Aliases: []string{"c"},
		Help:    "Start a chat with a kagent agent.",
		LongHelp: `Start a chat with a kagent agent.

If no agent name is provided, then a list of available agents will be provided to select from.
If no session name is provided, then a new session will be created and the chat will be associated with it.

Examples:
- chat [agent_name] -s [session_name]
- chat [agent_name]
- chat
`,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.ChatCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(runCmd)

	getCmd := &ishell.Cmd{
		Name:    "get",
		Aliases: []string{"g"},
		Help:    "get kagent resources.",
		LongHelp: `get kagent resources.

		get [resource_type] [resource_name]

Examples:
  get agents
  `,
	}

	getCmd.AddCmd(&ishell.Cmd{
		Name:    "session",
		Aliases: []string{"s", "sessions"},
		Help:    "get a session.",
		LongHelp: `get a session.

If no resource name is provided, then a list of available resources will be returned.
Examples:
  get session [session_id]
  get session
  `,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			if len(c.Args) > 0 {
				cli.GetSessionCmd(cfg, c.Args[0])
			} else {
				cli.GetSessionCmd(cfg, "")
			}
		},
	})

	getCmd.AddCmd(&ishell.Cmd{
		Name:    "agent",
		Aliases: []string{"a", "agents"},
		Help:    "get an agent.",
		LongHelp: `get an agent.

If no resource name is provided, then a list of available resources will be returned.
Examples:
  get agent [agent_name]
  get agent
  `,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			if len(c.Args) > 0 {
				cli.GetAgentCmd(cfg, c.Args[0])
			} else {
				cli.GetAgentCmd(cfg, "")
			}
		},
	})

	getCmd.AddCmd(&ishell.Cmd{
		Name:    "tool",
		Aliases: []string{"t", "tools"},
		Help:    "get a tool.",
		LongHelp: `get a tool.

If no resource name is provided, then a list of available resources will be returned.
Examples:
  get tool [tool_name]
  get tool
  `,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			cli.GetToolCmd(cfg)
		},
	})

	shell.AddCmd(getCmd)

	bugReportCmd := &ishell.Cmd{
		Name:    "bug-report",
		Aliases: []string{"br"},
		Help:    "Generate a bug report with system information",
		LongHelp: `Generate a bug report containing:
- Agent, ModelConfig, and ToolServers YAMLs
- Secret names (without values)
- Pod logs
- Versions and images used

The report will be saved in a new directory with timestamp.

Example:
  bug-report
`,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			cli.BugReportCmd(cfg)
		},
	}

	shell.AddCmd(bugReportCmd)

	shell.AddCmd(&ishell.Cmd{
		Name:    "version",
		Aliases: []string{"v"},
		Help:    "Print the kagent version.",
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.VersionCmd(cfg)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "install",
		Aliases: []string{"i"},
		Help:    "Install kagent.",
		Func: func(c *ishell.Context) {
			if pf := cli.InteractiveInstallCmd(ctx, c); pf != nil {
				// Set the port-forward to the shell.
				shell.Set(portForwardKey, pf)
			}
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "uninstall",
		Aliases: []string{"u"},
		Help:    "Uninstall kagent.",
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			cli.UninstallCmd(ctx, cfg)
			// Safely stop the port-forward if it is running.
			if pf := shell.Get(portForwardKey); pf != nil {
				pf.(*cli.PortForward).Stop()
				shell.Del(portForwardKey)
			}
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "dashboard",
		Aliases: []string{"d"},
		Help:    "Open the kagent dashboard.",
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			cli.DashboardCmd(ctx, cfg)
		},
	})

	defer func() {
		if pf := shell.Get(portForwardKey); pf != nil {
			pf.(*cli.PortForward).Stop()
			shell.Del(portForwardKey)
		}
	}()
	shell.Run()
}
