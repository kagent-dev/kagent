// Package driver runs `omp --mode rpc` for one turn and translates its frames
// into the runtime-neutral events consumed by the shared A2A executor.
package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
)

// ProcessConfig contains the adapter's inputs for one omp process. Args are
// the run's model, prompt and isolation flags; the driver adds the protocol
// and session flags it owns.
type ProcessConfig struct {
	Executable      string
	ExpectedVersion string
	Args            []string
	Workspace       string
	SessionDir      string
	Environment     []string
	MaxFrameBytes   int
	MaxStderrBytes  int
	InterruptGrace  time.Duration
	// Prepare runs before every omp process. omp's shell can write the
	// directory its configuration lives in, so the adapter rewrites the files
	// it owns and one turn cannot change the next turn's tools or providers.
	Prepare func() error
}

// ProcessDriver starts one omp process per turn. The conversation lives in
// omp's session file under SessionDir, so a turn after a suspend resumes it
// by the session ID the first turn reported.
type ProcessDriver struct {
	config ProcessConfig
}

type processSession struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	client  *rpcClient
	wait    <-chan error
	stderr  *utils.BoundedBuffer
}

// NewProcessDriver constructs an omp process driver.
func NewProcessDriver(config ProcessConfig) *ProcessDriver { return &ProcessDriver{config: config} }

var _ interface {
	Run(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error)
} = (*ProcessDriver)(nil)

// Validate checks that the configured executable is the pinned omp release.
func (d *ProcessDriver) Validate(ctx context.Context) error {
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return fmt.Errorf("find omp executable %q: %w", d.config.Executable, err)
	}
	command := exec.CommandContext(ctx, executable, "--version")
	command.Env = d.config.Environment
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read omp version: %w", err)
	}
	if version := strings.TrimSpace(string(output)); version != "omp/"+d.config.ExpectedVersion {
		return fmt.Errorf("omp version mismatch: got %q, expected %q", version, "omp/"+d.config.ExpectedVersion)
	}
	return nil
}

// Run starts omp on the Actor's session, sends the prompt and streams the
// turn until omp has answered it and its session has settled. The turn's
// usage is the difference of omp's session totals around the prompt.
func (d *ProcessDriver) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	if strings.TrimSpace(turn.Prompt) == "" {
		return runtime.Outcome{}, fmt.Errorf("omp prompt is required")
	}
	if turn.InputResponse != nil {
		return runtime.Outcome{}, fmt.Errorf("omp never waits for input, so there is no turn to answer")
	}
	if d.config.Prepare != nil {
		if err := d.config.Prepare(); err != nil {
			return runtime.Outcome{}, err
		}
	}
	session, err := d.start(turn.ContinuationID)
	if err != nil {
		return runtime.Outcome{}, err
	}
	defer d.stop(session)

	ignore := func(f frame) error { return cancelDialog(session.client, f) }
	stateData, err := session.client.call(ctx, "get_state", ignore)
	if err != nil {
		return runtime.Outcome{}, d.protocolError(err, session.stderr)
	}
	var state struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(stateData, &state); err != nil || state.SessionID == "" {
		return runtime.Outcome{}, fmt.Errorf("omp reported no session")
	}
	if turn.ContinuationID != "" && state.SessionID != turn.ContinuationID {
		return runtime.Outcome{}, fmt.Errorf("omp resumed unexpected session %q", state.SessionID)
	}
	if err := sink.SessionStarted(runtime.SessionStarted{ContinuationID: state.SessionID}); err != nil {
		return runtime.Outcome{}, err
	}
	before, err := d.stats(ctx, session, ignore)
	if err != nil {
		return runtime.Outcome{}, err
	}
	promptID, err := session.client.send("prompt", turn.Prompt)
	if err != nil {
		return runtime.Outcome{}, err
	}
	tracker := &promptTracker{promptID: promptID}
	if err := d.consume(ctx, session, tracker, sink); err != nil {
		return runtime.Outcome{}, err
	}
	outcome := runtime.Outcome{Failure: tracker.failure()}
	after, err := d.stats(ctx, session, ignore)
	if err != nil {
		return runtime.Outcome{}, err
	}
	outcome.Usage = turnUsage(before, after)
	return outcome, nil
}

func (d *ProcessDriver) start(continuationID string) (*processSession, error) {
	executable, err := exec.LookPath(d.config.Executable)
	if err != nil {
		return nil, fmt.Errorf("find omp executable %q: %w", d.config.Executable, err)
	}
	args := append([]string{"--mode", "rpc", "--no-ui", "--session-dir", d.config.SessionDir}, d.config.Args...)
	if continuationID != "" {
		args = append(args, "--resume", continuationID)
	}
	command := exec.Command(executable, args...)
	command.Dir, command.Env = d.config.Workspace, append([]string(nil), d.config.Environment...)
	utils.ConfigureProcessGroup(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open omp stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open omp stdout: %w", err)
	}
	stderr := utils.NewBoundedBuffer(d.config.MaxStderrBytes)
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start omp: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait(); close(wait) }()
	return &processSession{
		command: command, stdin: stdin, wait: wait, stderr: stderr,
		client: newRPCClient(stdin, stdout, d.config.MaxFrameBytes),
	}, nil
}

func (d *ProcessDriver) stats(ctx context.Context, session *processSession, ignore func(frame) error) (sessionStats, error) {
	data, err := session.client.call(ctx, "get_session_stats", ignore)
	if err != nil {
		return sessionStats{}, d.protocolError(err, session.stderr)
	}
	var stats sessionStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return sessionStats{}, fmt.Errorf("decode omp session stats: %w", err)
	}
	return stats, nil
}

func (d *ProcessDriver) consume(ctx context.Context, session *processSession, tracker *promptTracker, sink runtime.EventSink) error {
	for {
		select {
		case <-ctx.Done():
			// abort ends the run; the session file keeps what omp recorded.
			_, _ = session.client.send("abort", "")
			return ctx.Err()
		case result, ok := <-session.client.frames:
			if !ok {
				return d.exited(session)
			}
			if result.err != nil {
				return result.err
			}
			done, err := tracker.translate(result.frame, sink, session.client.write)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

// exited reports omp's end before the prompt finished, with its diagnostics.
func (d *ProcessDriver) exited(session *processSession) error {
	select {
	case waitErr := <-session.wait:
		if waitErr != nil {
			return fmt.Errorf("omp exited: %w: %s", waitErr, strings.TrimSpace(session.stderr.String()))
		}
	case <-time.After(d.config.InterruptGrace):
	}
	return fmt.Errorf("omp exited before the turn ended: %s", strings.TrimSpace(session.stderr.String()))
}

// stop closes stdin, which ends omp after it has persisted the session, and
// signals the process group only when it does not exit in time.
func (d *ProcessDriver) stop(session *processSession) {
	_ = session.stdin.Close()
	select {
	case <-session.wait:
		return
	case <-time.After(d.config.InterruptGrace):
	}
	_ = utils.TerminateProcessGroup(session.command.Process)
	select {
	case <-session.wait:
	case <-time.After(d.config.InterruptGrace):
		_ = utils.KillProcessGroup(session.command.Process)
		<-session.wait
	}
}

func cancelDialog(client *rpcClient, f frame) error {
	if f.Type != frameUIRequest || !dialogMethods[f.Method] {
		return nil
	}
	return client.write(uiCancel{Type: "extension_ui_response", ID: f.ID, Cancelled: true})
}

func (d *ProcessDriver) protocolError(err error, stderr *utils.BoundedBuffer) error {
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}
