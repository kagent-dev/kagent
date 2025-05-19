package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/cli"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
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

	rootCmd.PersistentFlags().StringVar(&cfg.APIURL, "api-url", "http://localhost:8081/api", "API URL")
	rootCmd.PersistentFlags().StringVar(&cfg.UserID, "user-id", "admin@kagent.dev", "User ID")
	rootCmd.PersistentFlags().StringVarP(&cfg.Namespace, "namespace", "n", "kagent", "Namespace")
	rootCmd.PersistentFlags().StringVar(&cfg.A2AURL, "a2a-url", "http://localhost:8083/api/a2a", "A2A URL")
	rootCmd.PersistentFlags().StringVarP(&cfg.OutputFormat, "output-format", "o", "table", "Output format")
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Verbose output")
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install kagent",
		Long:  `Install kagent`,
		Run: func(cmd *cobra.Command, args []string) {
			cli.InstallCmd(cmd.Context(), cfg)
		},
	}

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
	}

	invokeCmd.Flags().StringVarP(&invokeCfg.Task, "task", "t", "", "Task")
	invokeCmd.Flags().StringVarP(&invokeCfg.Session, "session", "s", "", "Session")
	invokeCmd.Flags().StringVarP(&invokeCfg.Agent, "agent", "a", "", "Agent")
	invokeCmd.Flags().BoolVarP(&invokeCfg.Stream, "stream", "S", false, "Stream the response")
	invokeCmd.MarkFlagRequired("task")

	bugReportCmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Generate a bug report",
		Long:  `Generate a bug report`,
		Run: func(cmd *cobra.Command, args []string) {
			client := autogen_client.New(cfg.APIURL)
			if err := cli.CheckServerConnection(client); err != nil {
				pf := cli.NewPortForward(ctx, cfg)
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
			client := autogen_client.New(cfg.APIURL)
			if err := cli.CheckServerConnection(client); err != nil {
				pf := cli.NewPortForward(ctx, cfg)
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
			client := autogen_client.New(cfg.APIURL)
			if err := cli.CheckServerConnection(client); err != nil {
				pf := cli.NewPortForward(ctx, cfg)
				defer pf.Stop()
			}
			resourceType := ""
			resourceName := ""
			if len(args) > 0 {
				resourceType = args[0]
			} else {
				fmt.Fprintf(os.Stderr, "No resource type provided\n")
				os.Exit(1)
			}
			if len(args) > 1 {
				resourceName = args[1]
			}
			switch strings.TrimSuffix(strings.ToLower(resourceType), "s") {
			case "session":
				cli.GetSessionCmd(cfg, resourceName)
			case "run":
				cli.GetRunCmd(cfg, resourceName)
			case "agent":
				cli.GetAgentCmd(cfg, resourceName)
			case "tool":
				cli.GetToolCmd(cfg)
			default:
				fmt.Fprintf(os.Stderr, "Invalid resource type: %s\n", resourceType)
				os.Exit(1)
			}
		},
	}

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

func runInteractive() {
	cfg, err := config.Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting config: %v\n", err)
		os.Exit(1)
	}

	client := autogen_client.New(cfg.APIURL)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8081:8081")
	// Error connecting to server, port-forward the server
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()
	// Ensure the context is cancelled when the shell is closed
	defer func() {
		cancel()
		// cmd.Wait()
		if err := cmd.Wait(); err != nil {
			// These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}

	// create new shell.
	// by default, new shell includes 'exit', 'help' and 'clear' commands.
	shell := ishell.New()
	config.SetHistoryPath(homeDir, shell)
	if err := shell.ClearScreen(); err != nil {
		fmt.Fprintf(os.Stderr, "Error clearing screen: %v\n", err)
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
- run chat [team_name] -s [session_name]
- run chat
  `,
	}

	runCmd.AddCmd(&ishell.Cmd{
		Name:    "chat",
		Aliases: []string{"c"},
		Help:    "Start a chat with a kagent agent.",
		LongHelp: `Start a chat with a kagent agent.

If no team name is provided, then a list of available teams will be provided to select from.
If no session name is provided, then a new session will be created and the chat will be associated with it.

Examples:
- chat [team_name] -s [session_name]
- chat [team_name]
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
  get run
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
		Name:    "run",
		Aliases: []string{"r", "runs"},
		Help:    "get a run.",
		LongHelp: `get a run.

If no resource name is provided, then a list of available resources will be returned.
Examples:
  get run [run_id]
  get run
  `,
		Func: func(c *ishell.Context) {
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cfg := config.GetCfg(c)
			if len(c.Args) > 0 {
				cli.GetRunCmd(cfg, c.Args[0])
			} else {
				cli.GetRunCmd(cfg, "")
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

	shell.NotFound(func(c *ishell.Context) {
		// Hidden create command
		if len(c.Args) > 0 && c.Args[0] == "create" {
			c.Args = c.Args[1:]
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.CreateCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		} else if len(c.Args) > 0 && c.Args[0] == "delete" {
			c.Args = c.Args[1:]
			if err := cli.CheckServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.DeleteCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		} else {
			c.Println("Command not found. Type 'help' to see available commands.")
		}
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "version",
		Aliases: []string{"v"},
		Help:    "Print the kagent version.",
		Func: func(c *ishell.Context) {
			cli.VersionCmd(cfg)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "install",
		Aliases: []string{"i"},
		Help:    "Install kagent.",
		Func: func(c *ishell.Context) {
			cfg := config.GetCfg(c)
			cli.InstallCmd(ctx, cfg)
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

	shell.AddCmd(cli.A2ACmd(ctx))

	shell.Run()
}
