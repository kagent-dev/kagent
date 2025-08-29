package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type InvokeCfg struct {
	Config      *config.Config
	Task        string
	File        string
	Session     string
	Agent       string
	Stream      bool
	URLOverride string
}

func InvokeCmd(ctx context.Context, cfg *InvokeCfg) {

	clientSet := cfg.Config.Client()

	if err := CheckServerConnection(clientSet); err != nil {
		// If a connection does not exist, start a short-lived port-forward.
		pf, err := NewPortForward(ctx, cfg.Config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			return
		}
		defer pf.Stop()
	}

	var task string
	// If task is set, use it. Otherwise, read from file or stdin.
	if cfg.Task != "" {
		task = cfg.Task
	} else if cfg.File != "" {
		switch cfg.File {
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
			content, err := os.ReadFile(cfg.File)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading from file: %v\n", err)
				return
			}
			task = string(content)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Task or file is required")
		return
	}

	var a2aClient *a2aclient.A2AClient
	var err error
	if cfg.URLOverride != "" {

		a2aClient, err = a2aclient.NewA2AClient(cfg.URLOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
			return
		}
	} else {
		if cfg.Agent == "" {
			fmt.Fprintln(os.Stderr, "Agent is required")
			return
		}

		a2aURL := fmt.Sprintf("%s/api/a2a/%s/%s", cfg.Config.KAgentURL, cfg.Config.Namespace, cfg.Agent)
		a2aClient, err = a2aclient.NewA2AClient(a2aURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
			return
		}
	}

	var sessionID *string
	if cfg.Session != "" {
		sessionID = &cfg.Session
	}

	// Use A2A client to send message
	if cfg.Stream {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:      protocol.KindMessage,
				Role:      protocol.MessageRoleUser,
				ContextID: sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(task)},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}
		StreamA2AEvents(result, cfg.Config.Verbose)
	} else {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
			Message: protocol.Message{
				Kind:      protocol.KindMessage,
				Role:      protocol.MessageRoleUser,
				ContextID: sessionID,
				Parts:     []protocol.Part{protocol.NewTextPart(task)},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}

		jsn, err := result.MarshalJSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling result: %v\n", err)
			return
		}

		fmt.Fprintf(os.Stdout, "%+v\n", string(jsn)) //nolint:errcheck
	}
}
