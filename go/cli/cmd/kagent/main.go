package main

import (
	"fmt"
	"os"

	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/cli"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

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
		Func: cli.GetSessionCmd,
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
		Func: cli.GetRunCmd,
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
		Func: cli.GetAgentCmd,
	})

	shell.AddCmd(getCmd)

	shell.NotFound(func(c *ishell.Context) {
		// Hidden create command
		if len(c.Args) > 0 && c.Args[0] == "create" {
			c.Args = c.Args[1:]
			cli.CreateCmd(c)
			c.SetPrompt(config.BoldBlue("kagent >> "))
		} else if len(c.Args) > 0 && c.Args[0] == "delete" {
			c.Args = c.Args[1:]
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

	shell.Run()
}
