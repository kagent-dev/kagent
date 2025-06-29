package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

type InvokeCfg struct {
	Config  *config.Config
	Task    string
	Session string
	Agent   string
	Stream  bool
}

func InvokeCmd(ctx context.Context, cfg *InvokeCfg) {

	client := autogen_client.New(cfg.Config.APIURL)

	var pf *portForward
	if err := CheckServerConnection(client); err != nil {
		pf = NewPortForward(ctx, cfg.Config)
		defer pf.Stop()
	}

	var task string
	switch cfg.Task {
	case "":
		fmt.Fprintln(os.Stderr, "Task is required")
		return
	case "-":
		// Read from stdin
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
			return
		}
		task = string(content)
	default:
		// Read from file
		content, err := os.ReadFile(cfg.Task)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from file: %v\n", err)
			return
		}
		task = string(content)
	}
	// If session is set invoke within a session.
	if cfg.Session != "" {
		session, err := client.GetSession(cfg.Session, cfg.Config.UserID)
		var team *autogen_client.Team

		if err != nil {
			if errors.Is(err, autogen_client.NotFoundError) {
				if cfg.Agent == "" {
					fmt.Fprintln(os.Stderr, "Agent is required when creating a new session")
					return
				}
				// If the session is not found, create it

				session, err = client.CreateSession(&autogen_client.CreateSession{
					Name:   cfg.Session,
					UserID: cfg.Config.UserID,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
					return
				}
			} else {
				fmt.Fprintf(os.Stderr, "Error getting session: %v\n", err)
				return
			}
		}

		team, err = client.GetTeam(cfg.Agent, cfg.Config.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting team: %v\n", err)
			return
		}

		if cfg.Stream {
			usage := &autogen_client.ModelsUsage{}
			ch, err := client.InvokeSessionStream(session.ID, cfg.Config.UserID, &autogen_client.InvokeRequest{
				Task:       task,
				TeamConfig: team.Component,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
				return
			}
			StreamEvents(ch, usage, cfg.Config.Verbose)
		} else {
			result, err := client.InvokeSession(session.ID, cfg.Config.UserID, &autogen_client.InvokeRequest{
				Task:       task,
				TeamConfig: team.Component,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
				return
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result.TaskResult); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding task result: %v\n", err)
				return
			}
		}

	} else {

		team, err := client.GetTeam(cfg.Agent, cfg.Config.UserID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting team: %v\n", err)
			return
		}

		req := &autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: team.Component,
		}

		if cfg.Stream {
			usage := &autogen_client.ModelsUsage{}
			ch, err := client.InvokeTaskStream(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking task: %v\n", err)
				return
			}
			StreamEvents(ch, usage, cfg.Config.Verbose)
		} else {
			result, err := client.InvokeTask(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking task: %v\n", err)
				return
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result.TaskResult); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding task result: %v\n", err)
				return
			}
		}
	}
}
