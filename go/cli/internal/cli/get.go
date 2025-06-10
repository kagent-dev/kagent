package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func GetAgentCmd(cfg *config.Config, resourceName string) {
	client := autogen_client.New(cfg.APIURL)

	if resourceName == "" {
		agentList, err := client.ListTeams(cfg.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get agents: %v\n", err)
			return
		}

		if len(agentList) == 0 {
			fmt.Println("No agents found")
			return
		}

		if err := printTeams(agentList); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print agents: %v\n", err)
			return
		}
	} else {
		agent, err := client.GetTeam(resourceName, cfg.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get agent %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(agent, "", "  ")
		fmt.Fprintln(os.Stdout, string(byt))
	}
}

func GetRunCmd(cfg *config.Config, resourceName string) {
	client := autogen_client.New(cfg.APIURL)
	if resourceName == "" {
		runList, err := client.ListRuns(cfg.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get runs: %v\n", err)
			return
		}

		if len(runList) == 0 {
			fmt.Println("No runs found")
			return
		}

		if err := printRuns(runList); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print runs: %v\n", err)
			return
		}
	} else {
		// Convert run ID from string to integer
		runID, err := strconv.Atoi(resourceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid run ID: %s, must be a number: %v\n", resourceName, err)
			return
		}

		run, err := client.GetRun(runID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get run %d: %v\n", runID, err)
			return
		}
		byt, _ := json.MarshalIndent(run, "", "  ")
		fmt.Fprintln(os.Stdout, string(byt))
	}
}

func GetSessionCmd(cfg *config.Config, resourceName string) {
	client := autogen_client.New(cfg.APIURL)
	if resourceName == "" {
		sessionList, err := client.ListSessions(cfg.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get sessions: %v\n", err)
			return
		}

		if len(sessionList) == 0 {
			fmt.Println("No sessions found")
			return
		}

		if err := printSessions(sessionList); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print sessions: %v\n", err)
			return
		}
	} else {
		sessionID, err := strconv.Atoi(resourceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to convert session name to ID: %v\n", err)
			return
		}
		session, err := client.GetSessionById(sessionID, cfg.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get session %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(session, "", "  ")
		fmt.Fprintln(os.Stdout, string(byt))
	}
}

func GetToolCmd(cfg *config.Config) {
	client := autogen_client.New(cfg.APIURL)
	toolList, err := client.ListTools(cfg.UserID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tools: %v\n", err)
		return
	}
	if err := printTools(toolList); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to print tools: %v\n", err)
		return
	}
}

func printTools(tools []*autogen_client.Tool) error {
	headers := []string{"#", "ID", "PROVIDER", "LABEL", "CREATED"}
	rows := make([][]string, len(tools))
	for i, tool := range tools {
		rows[i] = []string{
			strconv.Itoa(i + 1),
			strconv.Itoa(tool.Id),
			tool.Component.Provider,
			tool.Component.Label,
			tool.CreatedAt,
		}
	}

	return printOutput(tools, headers, rows)
}
func printRuns(runs []*autogen_client.Run) error {
	headers := []string{"#", "ID", "CONTENT", "MESSAGES", "STATUS", "CREATED"}
	rows := make([][]string, len(runs))
	for i, run := range runs {
		contentStr := "[N/A]" // Default content if type assertion fails or content is nil
		if run.Task.Content != nil {
			if content, ok := run.Task.Content.(string); ok {
				if len(content) > 10 {
					contentStr = content[:10] + "..."
				} else {
					contentStr = content
				}
			} else {
				contentStr = "[non-string]"
			}
		}

		rows[i] = []string{
			strconv.Itoa(i + 1),
			strconv.Itoa(run.ID),
			contentStr,
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
			strconv.Itoa(i + 1),
			team.Component.Label,
			strconv.Itoa(team.Id),
			team.CreatedAt,
		}
	}

	return printOutput(teams, headers, rows)
}

func printSessions(sessions []*autogen_client.Session) error {
	headers := []string{"#", "ID", "NAME", "TEAM"}
	rows := make([][]string, len(sessions))
	for i, session := range sessions {
		rows[i] = []string{
			strconv.Itoa(i + 1),
			strconv.Itoa(session.ID),
			session.Name,
			strconv.Itoa(session.TeamID),
		}
	}

	return printOutput(sessions, headers, rows)
}
