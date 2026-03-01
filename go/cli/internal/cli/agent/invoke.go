package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	a2autil "github.com/kagent-dev/kagent/go/internal/a2a"
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

	if err := CheckServerConnection(ctx, clientSet); err != nil {
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

	var a2aURL string
	if cfg.URLOverride != "" {
		a2aURL = cfg.URLOverride
	} else {
		if cfg.Agent == "" {
			fmt.Fprintln(os.Stderr, "Agent is required")
			return
		}

		// Error out if the agent is provided with the namespace (e.g., namespace/agent-name)
		if strings.Contains(cfg.Agent, "/") {
			fmt.Fprintf(os.Stderr, "Invalid agent format: use --namespace to specify the namespace. Got'%s'\n", cfg.Agent)
			return
		}

		a2aURL = fmt.Sprintf("%s/api/a2a/%s/%s", cfg.Config.KAgentURL, cfg.Config.Namespace, cfg.Agent)
	}

	httpClient := &http.Client{Timeout: cfg.Config.Timeout}
	endpoints := []a2a.AgentInterface{
		{Transport: a2a.TransportProtocolJSONRPC, URL: a2aURL},
	}
	a2aClient, err := a2aclient.NewFromEndpoints(
		ctx,
		endpoints,
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithJSONRPCTransport(httpClient),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
		return
	}

	var sessionID *string
	if cfg.Session != "" {
		sessionID = &cfg.Session
	}

	// Build message
	message := a2a.NewMessage(a2a.MessageRoleUser, &a2a.TextPart{Text: task})
	if sessionID != nil {
		message.ContextID = *sessionID
	}
	params := &a2a.MessageSendParams{Message: message}

	// Use A2A client to send message
	if cfg.Stream {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		eventIter := a2aClient.SendStreamingMessage(ctx, params)
		channel := a2autil.EventIterToChannel(ctx, eventIter)
		StreamA2AEvents(channel, cfg.Config.Verbose)
	} else {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.SendMessage(ctx, params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}

		jsn, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling result: %v\n", err)
			return
		}

		fmt.Fprintf(os.Stdout, "%+v\n", string(jsn))
	}
}
