package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/controller/utils/a2autils"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type A2ACfg struct {
	SessionID string
	AgentName string
	Task      string
	Timeout   time.Duration
	Config    *config.Config
}

func A2ARun(ctx context.Context, cfg *A2ACfg) {

	cancel := startPortForward(ctx)
	defer cancel()

	var sessionID *string
	if cfg.SessionID != "" {
		sessionID = &cfg.SessionID
	}

	result, err := runTask(ctx, cfg.Config.Namespace, cfg.AgentName, cfg.Task, sessionID, cfg.Timeout, cfg.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running task: %v\n", err)
		return
	}

	switch result.Status.State {
	case protocol.TaskStateUnknown:
		fmt.Fprintln(os.Stderr, "Task state is unknown.")
		if result.Status.Message != nil {
			fmt.Fprintln(os.Stderr, "Message:", a2autils.ExtractText(*result.Status.Message))
		} else {
			fmt.Fprintln(os.Stderr, "No message provided.")
		}
	case protocol.TaskStateCanceled:
		fmt.Fprintln(os.Stderr, "Task was canceled.")
	case protocol.TaskStateFailed:
		fmt.Fprintln(os.Stderr, "Task failed.")
		if result.Status.Message != nil {
			fmt.Fprintln(os.Stderr, "Error:", a2autils.ExtractText(*result.Status.Message))
		} else {
			fmt.Fprintln(os.Stderr, "No error message provided.")
		}
	case protocol.TaskStateCompleted:
		fmt.Fprintln(os.Stderr, "Task completed successfully:")
		for _, artifact := range result.Artifacts {
			var text string
			for _, part := range artifact.Parts {
				if textPart, ok := part.(protocol.TextPart); ok {
					text += textPart.Text
				}
			}
			fmt.Fprintln(os.Stdout, text)
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
		// cmd.Wait()
		if err := a2aPortFwdCmd.Wait(); err != nil {
			// These 2 errors are expected
			if !strings.Contains(err.Error(), "signal: killed") && !strings.Contains(err.Error(), "exec: not started") {
				fmt.Fprintf(os.Stderr, "Error waiting for port-forward to exit: %v\n", err)
			}
		}
	}
}

func runTask(
	ctx context.Context,
	agentNamespace, agentName string,
	userPrompt string,
	sessionID *string,
	timeout time.Duration,
	cfg *config.Config,
) (*protocol.Task, error) {
	a2aURL := fmt.Sprintf("%s/%s/%s", cfg.A2AURL, agentNamespace, agentName)
	a2a, err := client.NewA2AClient(a2aURL)
	if err != nil {
		return nil, err
	}
	task, err := a2a.SendTasks(ctx, protocol.SendTaskParams{
		ID:        "kagent-task-" + uuid.NewString(),
		SessionID: sessionID,
		Message: protocol.Message{
			Role:  protocol.MessageRoleUser,
			Parts: []protocol.Part{protocol.NewTextPart(userPrompt)},
		},
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Process the task
	return waitForTaskResult(ctx, a2a, task.ID)
}

func waitForTaskResult(ctx context.Context, a2a *client.A2AClient, taskID string) (*protocol.Task, error) {
	// poll task result every 2s
	ticker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ticker.C:
			task, err := a2a.GetTasks(ctx, protocol.TaskQueryParams{
				ID: taskID,
			})
			if err != nil {
				return nil, err
			}

			switch task.Status.State {
			case protocol.TaskStateSubmitted,
				protocol.TaskStateWorking:
				continue
			}

			return task, nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
