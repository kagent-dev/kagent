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
)

func checkServerConnection(client *autogen_client.Client) error {
	// Only check if we have a valid client
	if client == nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}

	_, err := client.GetVersion()
	if err != nil {
		return fmt.Errorf("Error connecting to server. Please run 'install' command first.")
	}
	return nil
}

func main() {

	// Initialize config
	if err := config.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting config: %v\n", err)
		os.Exit(1)
	}

	client := autogen_client.New(cfg.APIURL, cfg.WSURL)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8081:8081")
	if err != nil {
		// Error connecting to server, port-forward the server
		go func() {

			if err := cmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
				os.Exit(1)
			}
		}()
	}

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

	// create new shell.
	// by default, new shell includes 'exit', 'help' and 'clear' commands.
	shell := ishell.New()
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
			if err := checkServerConnection(client); err != nil {
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
  get session [session_name]
  get session
  `,
		Func: func(c *ishell.Context) {
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.GetSessionCmd(c)
		},
	})

	getCmd.AddCmd(&ishell.Cmd{
		Name:    "run",
		Aliases: []string{"r", "runs"},
		Help:    "get a run.",
		LongHelp: `get a run.

If no resource name is provided, then a list of available resources will be returned.
Examples:
  get run [run_name]
  get run
  `,
		Func: func(c *ishell.Context) {
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.GetRunCmd(c)
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
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.GetAgentCmd(c)
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
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.GetToolCmd(c)
		},
	})

	shell.AddCmd(getCmd)

	shell.NotFound(func(c *ishell.Context) {
		// Hidden create command
		if len(c.Args) > 0 && c.Args[0] == "create" {
			c.Args = c.Args[1:]
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.CreateCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		} else if len(c.Args) > 0 && c.Args[0] == "delete" {
			c.Args = c.Args[1:]
			if err := checkServerConnection(client); err != nil {
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
			cli.VersionCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "install",
		Aliases: []string{"i"},
		Help:    "Install kagent.",
		Func: func(c *ishell.Context) {
			cli.InstallCmd(ctx, c)
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "uninstall",
		Aliases: []string{"u"},
		Help:    "Uninstall kagent.",
		Func: func(c *ishell.Context) {
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.UninstallCmd(ctx, c)
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "dashboard",
		Aliases: []string{"d"},
		Help:    "Open the kagent dashboard.",
		Func: func(c *ishell.Context) {
			if err := checkServerConnection(client); err != nil {
				c.Println(err)
				return
			}
			cli.DashboardCmd(ctx, c)
		},
	})

	shell.Run()
}
