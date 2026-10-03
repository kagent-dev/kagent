package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	clioutput "github.com/kagent-dev/kagent/go/core/cli/internal/output"
	"github.com/spf13/cobra"
)

// processEvent keeps command output separate from metadata in JSON mode. Data
// is base64 so binary output round-trips. Offsets count bytes successfully emitted.
type processEvent struct {
	Event        string `json:"event"`
	SandboxID    string `json:"sandbox_id"`
	ProcessID    string `json:"process_id"`
	Status       string `json:"status,omitempty"`
	ExitCode     *int32 `json:"exit_code,omitempty"`
	Source       string `json:"source,omitempty"`
	Data         []byte `json:"data,omitempty"`
	StdoutOffset int64  `json:"stdout_offset"`
	StderrOffset int64  `json:"stderr_offset"`
}

type processExitError struct{ code int32 }

func (e *processExitError) Error() string {
	return fmt.Sprintf("sandbox process exited with code %d", e.code)
}

func (e *processExitError) ExitCode() int {
	if e.code > 0 && e.code < 256 {
		return int(e.code)
	}
	return 1
}

func newExecCmd() *cobra.Command {
	var cwd string
	var env map[string]string
	var wait bool
	cmd := &cobra.Command{
		Use: "exec ID -- COMMAND [ARG...]", Short: "Start a command once and wait for its result",
		Long: "Start a process once. By default, stream available output while waiting. A timeout stops waiting, not the remote process. Retain its process ID and use sandbox wait to reconnect. An uncertain start is never retried. JSON output is a stream of started/output/finished/interrupted records.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(2)(cmd, args); err != nil {
				return err
			}
			if cmd.ArgsLenAtDash() != 1 {
				return errors.New("separate the sandbox ID and command with --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				started, err := c.StartProcess(ctx, args[0], &guestpb.StartProcessRequest{Command: args[1:], Cwd: cwd, Env: env})
				if err != nil {
					return fmt.Errorf("start process in sandbox %s: command may have started; do not blindly retry: %w", args[0], err)
				}
				if started.GetProcessId() == "" {
					return errors.New("start returned no process ID; command may have started")
				}
				event := processEvent{Event: "started", SandboxID: args[0], ProcessID: started.ProcessId}
				if err := emitProcessEvent(cmd, format, event); err != nil {
					return fmt.Errorf("record process %s in sandbox %s: %w", started.ProcessId, args[0], err)
				}
				if !wait {
					return nil
				}
				return waitProcess(ctx, cmd, c, format, event)
			})
		},
	}
	cmd.Flags().StringVar(&cwd, "cwd", "/data/workspace", "Working directory inside the sandbox")
	cmd.Flags().StringToStringVar(&env, "env", nil, "Process environment (KEY=VALUE)")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for completion and return the process exit code")
	return cmd
}

func newWaitCmd() *cobra.Command {
	var stdoutOffset, stderrOffset int64
	cmd := &cobra.Command{
		Use: "wait ID PROCESS_ID", Short: "Collect output and wait for an existing process without restarting it", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if stdoutOffset < 0 || stderrOffset < 0 {
				return errors.New("output offsets must be nonnegative")
			}
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				return waitProcess(ctx, cmd, c, format, processEvent{SandboxID: args[0], ProcessID: args[1], StdoutOffset: stdoutOffset, StderrOffset: stderrOffset})
			})
		},
	}
	cmd.Flags().Int64Var(&stdoutOffset, "stdout-offset", 0, "Resume stdout at this byte offset")
	cmd.Flags().Int64Var(&stderrOffset, "stderr-offset", 0, "Resume stderr at this byte offset")
	return cmd
}

func newProcessCmd() *cobra.Command {
	return &cobra.Command{
		Use: "process ID PROCESS_ID", Short: "Inspect process status without waiting", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				process, err := c.GetProcess(ctx, args[0], &guestpb.GetProcessRequest{ProcessId: args[1]})
				if err != nil {
					return err
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteProto(cmd.OutOrStdout(), process)
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s", process.ProcessId, process.Status)
				if err != nil {
					return err
				}
				if process.Status != guestpb.ProcessStatus_PROCESS_STATUS_RUNNING {
					_, err = fmt.Fprintf(cmd.OutOrStdout(), "\texit_code=%d", process.ExitCode)
					if err != nil {
						return err
					}
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout())
				return err
			})
		},
	}
}

func newKillCmd() *cobra.Command {
	return &cobra.Command{
		Use: "kill ID PROCESS_ID", Short: "Terminate a process and its children", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, func(ctx context.Context, c *client.SandboxClient, _ connection.Options, format clioutput.Format) error {
				response, err := c.KillProcess(ctx, args[0], &guestpb.KillProcessRequest{ProcessId: args[1]})
				if err != nil {
					return err
				}
				if format == clioutput.FormatJSON {
					return clioutput.WriteProto(cmd.OutOrStdout(), response)
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\texit_code=%d\n", args[1], response.ExitCode)
				return err
			})
		},
	}
}

func waitProcess(ctx context.Context, cmd *cobra.Command, c *client.SandboxClient, format clioutput.Format, event processEvent) (err error) {
	defer func() {
		var exited *processExitError
		if err != nil && !errors.As(err, &exited) {
			event.Event = "interrupted"
			err = errors.Join(err, emitProcessEvent(cmd, format, event))
			err = fmt.Errorf("wait interrupted; sandbox %s process %s (stdout offset %d, stderr offset %d); use sandbox wait to continue: %w", event.SandboxID, event.ProcessID, event.StdoutOffset, event.StderrOffset, err)
		}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		process, err := c.GetProcess(ctx, event.SandboxID, &guestpb.GetProcessRequest{ProcessId: event.ProcessID})
		if err != nil {
			return err
		}
		switch process.Status {
		case guestpb.ProcessStatus_PROCESS_STATUS_RUNNING, guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED, guestpb.ProcessStatus_PROCESS_STATUS_FAILED, guestpb.ProcessStatus_PROCESS_STATUS_TERMINATED:
		default:
			return fmt.Errorf("unknown process status %s", process.Status)
		}
		err = c.ReadProcessOutputs(ctx, event.SandboxID, &guestpb.StreamProcessOutputsRequest{ProcessId: event.ProcessID, StdoutOffset: event.StdoutOffset, StderrOffset: event.StderrOffset}, func(chunk *guestpb.OutputChunk) error {
			next := event
			next.Event, next.Source, next.Data = "output", chunk.Source.String(), chunk.Data
			switch chunk.Source {
			case guestpb.OutputSource_OUTPUT_SOURCE_STDOUT:
				next.StdoutOffset += int64(len(chunk.Data))
			case guestpb.OutputSource_OUTPUT_SOURCE_STDERR:
				next.StderrOffset += int64(len(chunk.Data))
			default:
				return fmt.Errorf("unknown output source %s", chunk.Source)
			}
			if err := emitProcessEvent(cmd, format, next); err != nil {
				return err
			}
			event.StdoutOffset, event.StderrOffset = next.StdoutOffset, next.StderrOffset
			return nil
		})
		if err != nil {
			return err
		}
		if process.Status != guestpb.ProcessStatus_PROCESS_STATUS_RUNNING {
			event.Event, event.Status, event.ExitCode = "finished", process.Status.String(), &process.ExitCode
			if err := emitProcessEvent(cmd, format, event); err != nil {
				return err
			}
			if process.ExitCode != 0 || process.Status != guestpb.ProcessStatus_PROCESS_STATUS_COMPLETED {
				return &processExitError{code: process.ExitCode}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func emitProcessEvent(cmd *cobra.Command, format clioutput.Format, event processEvent) error {
	if format == clioutput.FormatJSON {
		return clioutput.WriteJSON(cmd.OutOrStdout(), event)
	}
	if event.Event == "output" {
		out := cmd.OutOrStdout()
		if event.Source == guestpb.OutputSource_OUTPUT_SOURCE_STDERR.String() {
			out = cmd.ErrOrStderr()
		}
		n, err := out.Write(event.Data)
		if err == nil && n != len(event.Data) {
			return io.ErrShortWrite
		}
		return err
	}
	if event.ExitCode != nil {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "Process %s: %s, exit_code=%d\n", event.ProcessID, event.Status, *event.ExitCode)
		return err
	}
	_, err := fmt.Fprintf(cmd.ErrOrStderr(), "Process %s in sandbox %s: %s (stdout offset %d, stderr offset %d)\n", event.ProcessID, event.SandboxID, event.Event, event.StdoutOffset, event.StderrOffset)
	return err
}
