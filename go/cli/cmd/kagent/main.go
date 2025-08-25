package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/abiosoft/ishell/v2"
	"github.com/kagent-dev/kagent/go/cli/internal/cli"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/cli/internal/profiles"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rootCmd := &cobra.Command{
		Use:   "kagent",
		Short: "kagent is a CLI for kagent",
		Long:  `kagent is a CLI for kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			runInteractive()
		},
	}

	cfg := &config.Config{}

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
			client := client.New(cfg.KAgentURL)
			if err := cli.CheckServerConnection(client); err != nil {
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
			client := client.New(cfg.KAgentURL)
			if err := cli.CheckServerConnection(client); err != nil {
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
			client := client.New(cfg.KAgentURL)
			if err := cli.CheckServerConnection(client); err != nil {
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
			client := client.New(cfg.KAgentURL)
			if err := cli.CheckServerConnection(client); err != nil {
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
			client := client.New(cfg.KAgentURL)
			if err := cli.CheckServerConnection(client); err != nil {
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

	rootCmd.AddCommand(installCmd, uninstallCmd, invokeCmd, bugReportCmd, versionCmd, dashboardCmd, getCmd)

	// Initialize config
	if err := config.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		os.Exit(1)
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
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

	client := client.New(cfg.KAgentURL, client.WithUserID("admin@kagent.dev"))
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
