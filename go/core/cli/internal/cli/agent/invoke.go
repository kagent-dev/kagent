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

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	clia2a "github.com/kagent-dev/kagent/go/core/cli/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
)

type InvokeCfg struct {
	Config      *config.Config
	Task        string
	File        string
	Session     string
	Agent       string
	Stream      bool
	URLOverride string
	Token       string
}

// bearerTokenTransport is an http.RoundTripper that injects an Authorization: Bearer header.
type bearerTokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
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

	clientOpts := clia2a.ClientOptions{Timeout: cfg.Config.Timeout}
	if cfg.Token != "" {
		clientOpts.HTTPClient = &http.Client{
			Timeout: cfg.Config.Timeout,
			Transport: &bearerTokenTransport{
				base:  http.DefaultTransport,
				token: cfg.Token,
			},
		}
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

		agentResponse, err := clientSet.Agent.GetAgent(ctx, fmt.Sprintf("%s/%s", cfg.Config.Namespace, cfg.Agent))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting agent metadata: %v\n", err)
			return
		}

		a2aURL = buildA2AURL(cfg.Config.KAgentURL, cfg.Config.Namespace, cfg.Agent, agentResponse.Data)
	}

	a2aClient, err := clia2a.NewClient(ctx, a2aURL, clientOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating A2A client: %v\n", err)
		return
	}

	msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(task))
	if cfg.Session != "" {
		msg.ContextID = cfg.Session
	}
	req := &a2atype.SendMessageRequest{Message: msg}

	// Use A2A client to send message
	if cfg.Stream {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		ch, err := clia2a.StreamToChannel(ctx, a2aClient, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error invoking session: %v\n", err)
			return
		}
		StreamA2AEvents(ch, cfg.Config.Verbose)
	} else {
		ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()

		result, err := a2aClient.SendMessage(ctx, req)
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

func buildA2AURL(baseURL, namespace, agent string, agentResponse *api.AgentResponse) string {
	a2aPath := "api/a2a"
	if agentResponse != nil && agentResponse.WorkloadMode == v1alpha2.WorkloadModeSandbox {
		a2aPath = "api/a2a-sandboxes"
	}
	return fmt.Sprintf("%s/%s/%s/%s", baseURL, a2aPath, namespace, agent)
}
