package cli

import (
	"encoding/json"
	"strconv"

	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func GetAgentCmd(c *ishell.Context) {
	var resourceName string
	if len(c.Args) > 0 {
		resourceName = c.Args[0]
	}
	client := config.GetClient(c)
	cfg := config.GetCfg(c)

	if resourceName == "" {
		agentList, err := client.ListTeams(cfg.UserID)
		if err != nil {
			c.Printf("Failed to get agents: %v\n", err)
			return
		}
		if err := printTeams(agentList); err != nil {
			c.Printf("Failed to print agents: %v\n", err)
			return
		}
	} else {
		agent, err := client.GetTeam(resourceName, cfg.UserID)
		if err != nil {
			c.Printf("Failed to get agent %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(agent, "", "  ")
		c.Println(string(byt))
	}
}

func GetRunCmd(c *ishell.Context) {
	var resourceName string
	if len(c.Args) > 0 {
		resourceName = c.Args[0]
	}
	client := config.GetClient(c)
	cfg := config.GetCfg(c)
	if resourceName == "" {
		runList, err := client.ListRuns(cfg.UserID)
		if err != nil {
			c.Printf("Failed to get runs: %v\n", err)
			return
		}

		if err := printRuns(runList); err != nil {
			c.Printf("Failed to print runs: %v\n", err)
			return
		}
	} else {
		run, err := client.GetRun(resourceName)
		if err != nil {
			c.Printf("Failed to get run %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(run, "", "  ")
		c.Println(string(byt))
	}
}

func GetSessionCmd(c *ishell.Context) {
	var resourceName string
	if len(c.Args) > 0 {
		resourceName = c.Args[0]
	}
	client := config.GetClient(c)
	cfg := config.GetCfg(c)
	if resourceName == "" {
		sessionList, err := client.ListSessions(cfg.UserID)
		if err != nil {
			c.Printf("Failed to get sessions: %v\n", err)
			return
		}
		if err := printSessions(sessionList); err != nil {
			c.Printf("Failed to print sessions: %v\n", err)
			return
		}
	} else {
		sessionID, err := strconv.Atoi(resourceName)
		if err != nil {
			c.Printf("Failed to convert session name to ID: %v\n", err)
			return
		}
		session, err := client.GetSession(sessionID, cfg.UserID)
		if err != nil {
			c.Printf("Failed to get session %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(session, "", "  ")
		c.Println(string(byt))
	}
}

func printRuns(runs []*autogen_client.Run) error {
	headers := []string{"#", "ID", "CONTENT", "MESSAGES", "STATUS", "CREATED"}
	rows := make([][]string, len(runs))
	for i, run := range runs {

		// Truncate task content to first 10 characters if possible
		content := run.Task.Content
		if len(content) > 10 {
			content = content[:10] + "..."
		}

		rows[i] = []string{
			strconv.Itoa(i),
			run.ID.String(),
			content,
			strconv.Itoa(len(run.Messages)),
			run.Status,
			run.CreatedAt,
		}
	}

	return printOutput(runs, headers, rows)
}

func printTeams(teams []*autogen_client.Team) error {
	// Prepare table data
	headers := []string{"#", "NAME", "ID", "CREATED"}
	rows := make([][]string, len(teams))
	for i, team := range teams {
		rows[i] = []string{
			strconv.Itoa(i),
			*team.Component.Label,
			strconv.Itoa(team.Id),
			*team.CreatedAt,
		}
	}

	return printOutput(teams, headers, rows)
}

func printSessions(sessions []*autogen_client.Session) error {
	headers := []string{"#", "ID", "NAME", "TEAM"}
	rows := make([][]string, len(sessions))
	for i, session := range sessions {
		rows[i] = []string{
			strconv.Itoa(i),
			strconv.Itoa(session.ID),
			session.Name,
			strconv.Itoa(session.TeamID),
		}
	}

	return printOutput(sessions, headers, rows)
}
