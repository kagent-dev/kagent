package cli

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"

	"github.com/abiosoft/ishell/v2"
	"github.com/abiosoft/readline"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/spf13/pflag"
)

const (
	sessionCreateNew = "[New Session]"
)

func ChatCmd(c *ishell.Context) {
	verbose := false
	var sessionName string
	flagSet := pflag.NewFlagSet(c.RawArgs[0], pflag.ContinueOnError)
	flagSet.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	flagSet.StringVarP(&sessionName, "session", "s", "", "Session name to use")
	if err := flagSet.Parse(c.Args); err != nil {
		c.Printf("Failed to parse flags: %v\n", err)
		return
	}

	cfg := config.GetCfg(c)
	client := config.GetClient(c)

	var team *autogen_client.Team
	if len(flagSet.Args()) > 0 {
		teamName := flagSet.Args()[0]
		var err error
		team, err = client.GetTeam(teamName, cfg.UserID)
		if err != nil {
			c.Println(err)
			return
		}
	}
	// If team is not found or not passed as an argument, prompt the user to select from available teams
	if team == nil {
		c.Printf("Please select from available teams.\n")
		// Get the teams based on the input + userID
		teams, err := client.ListTeams(cfg.UserID)
		if err != nil {
			c.Println(err)
			return
		}

		if len(teams) == 0 {
			c.Println("No teams found, please create one via the web UI or CRD before chatting.")
			return
		}

		teamNames := make([]string, len(teams))
		for i, team := range teams {
			if team.Component.Label == "" {
				continue
			}
			teamNames[i] = team.Component.Label
		}

		selectedTeamIdx := c.MultiChoice(teamNames, "Select an agent:")
		team = teams[selectedTeamIdx]
	}

	sessions, err := client.ListSessions(cfg.UserID)
	if err != nil {
		c.Println(err)
		return
	}

	existingSessions := slices.Collect(Filter(slices.Values(sessions), func(session *autogen_client.Session) bool { return true }))

	existingSessionNames := slices.Collect(Map(slices.Values(existingSessions), func(session *autogen_client.Session) string {
		return session.Name
	}))

	// Add the new session option to the beginning of the list
	existingSessionNames = append([]string{sessionCreateNew}, existingSessionNames...)
	var selectedSessionIdx int
	if sessionName != "" {
		selectedSessionIdx = slices.Index(existingSessionNames, sessionName)
	} else {
		selectedSessionIdx = c.MultiChoice(existingSessionNames, "Select a session:")
	}

	var session *autogen_client.Session
	if selectedSessionIdx == 0 {
		c.ShowPrompt(false)
		c.Print("Enter a session name: ")
		sessionName, err := c.ReadLineErr()
		if err != nil {
			c.Printf("Failed to read session name: %v\n", err)
			c.ShowPrompt(true)
			return
		}
		c.ShowPrompt(true)
		session, err = client.CreateSession(&autogen_client.CreateSession{
			UserID: cfg.UserID,
			Name:   sessionName,
		})
		if err != nil {
			c.Printf("Failed to create session: %v\n", err)
			return
		}
	} else {
		session = existingSessions[selectedSessionIdx-1]
	}

	promptStr := config.BoldGreen(fmt.Sprintf("%s--%s> ", team.Component.Label, session.Name))
	c.SetPrompt(promptStr)
	c.ShowPrompt(true)

	for {
		task, err := c.ReadLineErr()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) {
				c.Println("exiting chat session...")
				return
			}
			c.Printf("Failed to read task: %v\n", err)
			return
		}
		if task == "exit" {
			c.Println("exiting chat session...")
			return
		}
		if task == "help" {
			c.Println("Available commands:")
			c.Println("  exit - exit the chat session")
			c.Println("  help - show this help message")
			continue
		}

		usage := &autogen_client.ModelsUsage{}

		ch, err := client.InvokeSessionStream(session.ID, cfg.UserID, &autogen_client.InvokeRequest{
			Task:       task,
			TeamConfig: team.Component,
		})
		if err != nil {
			c.Printf("Failed to invoke session: %v\n", err)
			return
		}

		StreamEvents(ch, usage, verbose)
	}
}

// Yes, this is AI generated, and so is this comment.
var thinkingVerbs = []string{"thinking", "processing", "mulling over", "pondering", "reflecting", "evaluating", "analyzing", "synthesizing", "interpreting", "inferring", "deducing", "reasoning", "evaluating", "synthesizing", "interpreting", "inferring", "deducing", "reasoning"}

func getThinkingVerb() string {
	return thinkingVerbs[rand.Intn(len(thinkingVerbs))]
}
