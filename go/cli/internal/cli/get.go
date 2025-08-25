package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/pkg/client"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

func GetAgentCmd(cfg *config.Config, resourceName string) {
	client := client.New(cfg.KAgentURL)

	if resourceName == "" {
		agentList, err := client.Agent.ListAgents(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get agents: %v\n", err)
			return
		}

		if len(agentList.Data) == 0 {
			fmt.Println("No agents found")
			return
		}

		if err := printAgents(agentList.Data); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print agents: %v\n", err)
			return
		}
	} else {
		agent, err := client.Agent.GetAgent(context.Background(), resourceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get agent %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(agent, "", "  ")
		fmt.Fprintln(os.Stdout, string(byt)) //nolint:errcheck
	}
}

func GetSessionCmd(cfg *config.Config, resourceName string) {
	client := client.New(cfg.KAgentURL)
	if resourceName == "" {
		sessionList, err := client.Session.ListSessions(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get sessions: %v\n", err)
			return
		}

		if len(sessionList.Data) == 0 {
			fmt.Println("No sessions found")
			return
		}

		if err := printSessions(sessionList.Data); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print sessions: %v\n", err)
			return
		}
	} else {
		session, err := client.Session.GetSession(context.Background(), resourceName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get session %s: %v\n", resourceName, err)
			return
		}
		byt, _ := json.MarshalIndent(session, "", "  ")
		fmt.Fprintln(os.Stdout, string(byt)) //nolint:errcheck
	}
}

func GetToolCmd(cfg *config.Config) {
	client := client.New(cfg.KAgentURL)
	toolList, err := client.Tool.ListTools(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get tools: %v\n", err)
		return
	}
	if err := printTools(toolList); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to print tools: %v\n", err)
		return
	}
}

func printTools(tools []database.Tool) error {
	headers := []string{"#", "NAME", "SERVER_NAME", "DESCRIPTION", "CREATED"}
	rows := make([][]string, len(tools))
	for i, tool := range tools {
		rows[i] = []string{
			strconv.Itoa(i + 1),
			tool.ID,
			tool.ServerName,
			tool.Description,
			tool.CreatedAt.Format(time.RFC3339),
		}
	}

	return printOutput(tools, headers, rows)
}

func printAgents(agents []api.AgentResponse) error {
	// Prepare table data
	headers := []string{"#", "NAME", "CREATED", "DEPLOYMENT_READY", "ACCEPTED"}
	rows := make([][]string, len(agents))
	for i, agent := range agents {
		rows[i] = []string{
			strconv.Itoa(i + 1),
			utils.GetObjectRef(agent.Agent),
			agent.Agent.CreationTimestamp.Format(time.RFC3339),
			strconv.FormatBool(agent.DeploymentReady),
			strconv.FormatBool(agent.Accepted),
		}
	}

	return printOutput(agents, headers, rows)
}

func printSessions(sessions []*database.Session) error {
	headers := []string{"#", "ID", "NAME", "AGENT", "CREATED"}
	rows := make([][]string, len(sessions))
	for i, session := range sessions {
		agentID := ""
		if session.AgentID != nil {
			agentID = *session.AgentID
		}
		sessionName := ""
		if session.Name != nil {
			sessionName = *session.Name
		}
		rows[i] = []string{
			strconv.Itoa(i + 1),
			session.ID,
			sessionName,
			agentID,
			session.CreatedAt.Format(time.RFC3339),
		}
	}

	return printOutput(sessions, headers, rows)
}
