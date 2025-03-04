package main

import (
	"fmt"
	"os"

	"github.com/abiosoft/ishell/v2"
	"github.com/kagent-dev/kagent/go/cli/internal/cli"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func main() {

	// Initialize config
	if err := config.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing config: %v\n", err)
		os.Exit(1)
	}

	// create new shell.
	// by default, new shell includes 'exit', 'help' and 'clear' commands.
	shell := ishell.New()

	shell.SetPrompt(cli.BoldBlue("kagent >> "))
	shell.AddCmd(&ishell.Cmd{
		Name:    "chat",
		Aliases: []string{"c"},
		Help:    "Start a chat with a kagent agent.",
		LongHelp: `Start a chat with a kagent agent.

Examples:
  chat
  chat [team_name]

If no team name is provided, then a list of available teams will be provided to select from.
  `,
		Func: func(c *ishell.Context) {

			cli.ChatCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "get",
		Aliases: []string{"g"},
		Help:    "get kagent resources.",
		LongHelp: `get kagent resources.

		get [resource_type] [resource_name]

Examples:
  get run
  get agents
  `,
		Func: func(c *ishell.Context) {
			cli.GetCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		},
	})

	shell.NotFound(func(c *ishell.Context) {
		// Hidden create command
		if len(c.Args) > 0 && c.Args[0] == "create" {
			c.Args = c.Args[1:]
			cli.CreateCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		} else if len(c.Args) > 0 && c.Args[0] == "delete" {
			c.Args = c.Args[1:]
			cli.DeleteCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		} else {
			c.Println("Command not found. Type 'help' to see available commands.")
		}
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "install",
		Aliases: []string{"i"},
		Help:    "Install kagent to the current cluster.",
		Func: func(c *ishell.Context) {
			cli.InstallCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "uninstall",
		Aliases: []string{"u"},
		Help:    "Uninstall kagent from the current cluster.",
		Func: func(c *ishell.Context) {
			cli.UninstallCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		},
	})

	shell.AddCmd(&ishell.Cmd{
		Name:    "version",
		Aliases: []string{"v"},
		Help:    "Print the kagent version.",
		Func: func(c *ishell.Context) {
			cli.VersionCmd(c)
			c.SetPrompt(cli.BoldBlue("kagent >> "))
		},
	})

	shell.Run()
}
