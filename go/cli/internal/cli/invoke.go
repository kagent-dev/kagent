package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/pkg/client"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type InvokeCfg struct {
	Config  *config.Config
	Task    string
	File    string
	Session string
	Agent   string
	Stream  bool
}

func InvokeCmd(ctx context.Context, cfg *InvokeCfg) {

	clientSet := client.New(cfg.Config.APIURL)

	var pf *portForward
	if err := CheckServerConnection(clientSet); err != nil {
		pf = NewPortForward(ctx, cfg.Config)
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

	// Start port forwarding for A2A
	cancel := startPortForward(ctx)
	defer cancel()

	// If session is set invoke within a session.
	if cfg.Session != "" {

		if cfg.Agent == "" {
			fmt.Fprintln(os.Stderr, "Agent is required")
			return
		}

		// Setup A2A client
		a2aURL := fmt.Sprintf("%s/a2a/%s/%s", cfg.Config.APIURL, cfg.Config.Namespace, cfg.Agent)
		a2aClient, err := a2aclient.NewA2AClient(a2aURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
			return
		}

		// Use A2A client to send message
		if cfg.Stream {
			ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
			defer cancel()

			result, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
				Message: protocol.Message{
					Role:      protocol.MessageRoleUser,
					ContextID: &cfg.Session,
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
					Role:      protocol.MessageRoleUser,
					ContextID: &cfg.Session,
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

			fmt.Fprintf(os.Stdout, "%+v\n", string(jsn))
		}

	} else {

		if cfg.Agent == "" {
			fmt.Fprintln(os.Stderr, "Agent is required")
			return
		}

		// Setup A2A client
		a2aURL := fmt.Sprintf("%s/a2a/%s/%s", cfg.Config.APIURL, cfg.Config.Namespace, cfg.Agent)
		a2aClient, err := a2aclient.NewA2AClient(a2aURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
			return
		}

		// Use A2A client to send message (no session)
		if cfg.Stream {
			ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
			defer cancel()

			result, err := a2aClient.StreamMessage(ctx, protocol.SendMessageParams{
				Message: protocol.Message{
					Role:      protocol.MessageRoleUser,
					ContextID: nil, // No session
					Parts:     []protocol.Part{protocol.NewTextPart(task)},
				},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking task: %v\n", err)
				return
			}
			StreamA2AEvents(result, cfg.Config.Verbose)
		} else {
			ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
			defer cancel()

			result, err := a2aClient.SendMessage(ctx, protocol.SendMessageParams{
				Message: protocol.Message{
					Role:      protocol.MessageRoleUser,
					ContextID: nil, // No session
					Parts:     []protocol.Part{protocol.NewTextPart(task)},
				},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error invoking task: %v\n", err)
				return
			}

			jsn, err := result.MarshalJSON()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling result: %v\n", err)
				return
			}

			fmt.Fprintf(os.Stdout, "%+v\n", string(jsn))
		}
	}
}
