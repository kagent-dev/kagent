package cli

import (
	"context"
	"fmt"
	"github.com/abiosoft/ishell/v2"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/controller/utils/a2autils"
	"github.com/spf13/pflag"
	"os"
	"os/exec"
	"strings"
	"time"
	"trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func A2ACmd(ctx context.Context) *ishell.Cmd {
	a2aCmd := &ishell.Cmd{
		Name: "a2a",
		Help: "Interact with an Agent over the A2A protocol.",
	}
	a2aCmd.AddCmd(&ishell.Cmd{
		Name: "run",
		Help: "Run a task with an agent using the A2A protocol.",
		LongHelp: `Run a task with an agent using the A2A protocol.
The task is sent to the agent, and the result is printed to the console.

Example:
a2a run [--namespace <agent-namespace>] <agent-name> <task>
`,
		Func: func(c *ishell.Context) {
			if len(c.RawArgs) < 4 {
				c.Println("Usage: a2a run [--namespace <agent-namespace>] <agent-name> <task>")
				return
			}
			flagSet := pflag.NewFlagSet(c.RawArgs[0], pflag.ContinueOnError)
			agentNamespace := flagSet.String("namespace", "kagent", "Agent namespace")
			timeout := flagSet.Duration("timeout", 300*time.Second, "Timeout for the task")
			if err := flagSet.Parse(c.Args); err != nil {
				c.Printf("Failed to parse flags: %v\n", err)
				return
			}
			agentName := flagSet.Arg(0)
			prompt := flagSet.Arg(1)

			cancel := startPortForward(ctx)
			defer cancel()

			result, err := runTask(ctx, *agentNamespace, agentName, prompt, *timeout)
			if err != nil {
				c.Err(err)
				return
			}

			switch result.Status.State {
			case protocol.TaskStateUnknown:
				c.Println("Task state is unknown.")
				if result.Status.Message != nil {
					c.Println("Message:", a2autils.ExtractText(*result.Status.Message))
				} else {
					c.Println("No message provided.")
				}
			case protocol.TaskStateCanceled:
				c.Println("Task was canceled.")
			case protocol.TaskStateFailed:
				c.Println("Task failed.")
				if result.Status.Message != nil {
					c.Println("Error:", a2autils.ExtractText(*result.Status.Message))
				} else {
					c.Println("No error message provided.")
				}
			case protocol.TaskStateCompleted:
				c.Println("Task completed successfully:")
				for _, artifact := range result.Artifacts {
					var text string
					for _, part := range artifact.Parts {
						if textPart, ok := part.(protocol.TextPart); ok {
							text += textPart.Text
						}
					}
					c.Println(text)
				}
			}
		},
	})

	return a2aCmd
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
	timeout time.Duration,
) (*protocol.Task, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, err
	}
	a2aURL := fmt.Sprintf("%s/%s/%s", cfg.A2AURL, agentNamespace, agentName)
	a2a, err := client.NewA2AClient(a2aURL)
	if err != nil {
		return nil, err
	}
	task, err := a2a.SendTasks(ctx, protocol.SendTaskParams{
		ID:        "kagent-task-" + uuid.NewString(),
		SessionID: nil,
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
