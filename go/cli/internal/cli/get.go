package cli

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func GetCmd(c *ishell.Context) {
	if len(c.Args) == 0 {
		c.Printf("Usage: get [resource_type] [resource_name]\n")
		return
	}

	cfg, err := config.Get()
	if err != nil {
		c.Printf("Failed to get config: %v\n", err)
		return
	}

	client := autogen_client.New(cfg.APIURL, cfg.WSURL)

	resourceType := ""
	resourceName := ""
	for idx, arg := range c.Args {
		if idx == 0 {
			resourceType = arg
		} else if idx == 1 {
			resourceName = arg
		}
	}

	// Lowercase the resource type and remove the plural "s" if it exists
	resourceType = strings.TrimSuffix(strings.ToLower(resourceType), "s")
	switch resourceType {
	case "run":
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
	case "agent":
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
	default:
		c.Printf("Unknown resource type: %s\n", resourceType)
	}

}

func printRuns(runs []autogen_client.Run) error {
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
			run.ID,
			content,
			strconv.Itoa(len(run.Messages)),
			run.Status,
			run.CreatedAt,
		}
	}

	return printOutput(runs, headers, rows)
}

func printTeams(teams []autogen_client.Team) error {
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
