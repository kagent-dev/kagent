package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type A2ACfg struct {
	SessionID string
	AgentName string
	Task      string
	Timeout   time.Duration
	Config    *config.Config
	Stream    bool
}

func A2ARun(ctx context.Context, cfg *A2ACfg) {

	cancel := startPortForward(ctx)
	defer cancel()

	var sessionID *string
	if cfg.SessionID != "" {
		sessionID = &cfg.SessionID
	}

	if !cfg.Stream {
		err := runTask(ctx, cfg.Config.Namespace, cfg.AgentName, cfg.Task, sessionID, cfg.Timeout, cfg.Config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running task: %v\n", err)
			return
		}
	} else {
		if err := runTaskStream(ctx, cfg.Config.Namespace, cfg.AgentName, cfg.Task, sessionID, cfg.Timeout, cfg.Config); err != nil {
			fmt.Fprintf(os.Stderr, "Error running task: %v\n", err)
			return
		}
	}

}

func startPortForward(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	a2aPortFwdCmd := exec.CommandContext(ctx, "kubectl", "-n", "kagent", "port-forward", "service/kagent", "8083:8083")
	// Error connecting to server, port-forward the server
	go func() {
		if err := a2aPortFwdCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting port-forward: %v\n", err)
			os.Exit(1)
		}
	}()

	// Ensure the context is cancelled when the shell is closed
	return func() {
		cancel()
		if err := a2aPortFwdCmd.Wait(); err != nil {
			// These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}
}

func runTaskStream(
	ctx context.Context,
	agentNamespace, agentName string,
	userPrompt string,
	sessionID *string,
	timeout time.Duration,
	cfg *config.Config,
) error {

	a2aURL := fmt.Sprintf("%s/%s/%s", cfg.A2AURL, agentNamespace, agentName)
	a2a, err := client.NewA2AClient(a2aURL)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := a2a.StreamMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      protocol.MessageRoleUser,
			ContextID: sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(userPrompt)},
		},
	})
	if err != nil {
		return err
	}

	for event := range result {
		json, err := event.MarshalJSON()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%+v\n", string(json))
	}

	return nil
}

func runTask(
	ctx context.Context,
	agentNamespace, agentName string,
	userPrompt string,
	sessionID *string,
	timeout time.Duration,
	cfg *config.Config,
) error {
	a2aURL := fmt.Sprintf("%s/%s/%s", cfg.A2AURL, agentNamespace, agentName)
	a2a, err := client.NewA2AClient(a2aURL)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := a2a.SendMessage(ctx, protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      protocol.MessageRoleUser,
			ContextID: sessionID,
			Parts:     []protocol.Part{protocol.NewTextPart(userPrompt)},
		},
	})
	if err != nil {
		return err
	}

	jsn, err := result.MarshalJSON()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%+v\n", string(jsn))

	return nil
}
