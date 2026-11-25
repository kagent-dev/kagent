package anthropicselfhostedsandboxworker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

const (
	outputLimit      = 64 << 10
	lifecycleTimeout = 2 * time.Minute
)

type bashInput struct {
	Command   string `json:"command"`
	Restart   bool   `json:"restart"`
	TimeoutMS int64  `json:"timeout_ms"`
}

func (b bashInput) timeout() time.Duration {
	const maxTimeout = 5 * time.Minute
	if b.TimeoutMS <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(min(b.TimeoutMS, maxTimeout.Milliseconds())) * time.Millisecond
}

func (r *Runner) result(ctx context.Context, w assignment, c command, text string, isError bool) error {
	call, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	return r.store.transition(call, w, c, "result", nil, &text, isError)
}

func (r *Runner) killExpiredProcess(ctx context.Context, w assignment, processID string) {
	call, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	sandboxID, err := r.store.sandbox(call, w)
	if err != nil {
		r.warn(ctx, "find sandbox for expired command", err)
		return
	}
	if _, err := r.sandboxes.SignalProcess(call, sandboxID, &guestpb.SignalProcessRequest{
		ProcessId: processID, Signal: guestpb.Signal_SIGNAL_KILL,
	}); err != nil {
		r.warn(ctx, "kill process after heartbeat expiry", fmt.Errorf("sandbox API status %s", status.Code(err)))
	}
}

func (r *Runner) startCommand(ctx context.Context, w *assignment, c command, template *apiv1.ResourceReference) error {
	if c.name != "bash" {
		return r.result(ctx, *w, c, "This sandbox adapter supports the bash tool only.", true)
	}
	var input bashInput
	if err := json.Unmarshal(c.input, &input); err != nil || (input.Command == "" && !input.Restart) {
		return r.result(ctx, *w, c, "Invalid bash tool input.", true)
	}
	if input.Command == "" {
		return r.result(ctx, *w, c, "Shell reset.", false)
	}
	if err := r.ensureSandbox(ctx, w, template); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.warn(ctx, "prepare command sandbox", fmt.Errorf("sandbox API status %s", status.Code(err)))
		return r.result(ctx, *w, c, "Could not prepare a command sandbox.", true)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Once we start the RPC, let it return a handle and commit that handle
	// before releasing the lease, even if the pod is shutting down.
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), apiTimeout+2*dbTimeout)
	defer cancel()
	call, cancelCall := context.WithTimeout(startCtx, dbTimeout)
	err := r.store.transition(call, *w, c, "starting", nil, nil, false)
	cancelCall()
	if err != nil {
		return err
	}
	c.phase = "starting"
	call, cancelCall = context.WithTimeout(startCtx, apiTimeout)
	process, err := r.sandboxes.StartProcess(call, w.sandboxID, &guestpb.StartProcessRequest{
		Command: []string{"/bin/bash", "-c", input.Command}, Timeout: durationpb.New(input.timeout()),
	})
	cancelCall()
	if err != nil || process.GetProcessId() == "" {
		return r.result(startCtx, *w, c, "Could not start or confirm the command.", true)
	}
	call, cancelCall = context.WithTimeout(startCtx, dbTimeout)
	defer cancelCall()
	return r.store.transition(call, *w, c, "running", new(process.GetProcessId()), nil, false)
}

func (r *Runner) collectCommand(ctx context.Context, w assignment, c command) error {
	call, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	process, err := r.sandboxes.GetProcess(call, w.sandboxID, &guestpb.GetProcessRequest{ProcessId: c.processID})
	if err == nil && process.GetState() != guestpb.ProcessState_PROCESS_STATE_EXITED {
		if !commandOverdue(c) {
			return nil
		}
		return r.result(ctx, w, c, "Command did not finish in time.", true)
	}
	if err != nil {
		return r.collectFailure(ctx, w, c, err)
	}
	var output strings.Builder
	var overflow, complete bool
	appendOutput := func(p []byte) {
		n := min(len(p), outputLimit-output.Len())
		output.Write(p[:n])
		overflow = overflow || n < len(p)
	}
	err = r.sandboxes.ReadProcessOutput(call, w.sandboxID, &guestpb.StreamProcessOutputRequest{
		ProcessId: c.processID, Follow: true,
	}, func(chunk *guestpb.ProcessOutput) error {
		appendOutput(chunk.GetStdout())
		appendOutput(chunk.GetStderr())
		complete = complete || chunk.GetExit() != nil
		return nil
	})
	if err != nil {
		return r.collectFailure(ctx, w, c, err)
	}
	if !complete {
		return r.collectFailure(ctx, w, c, status.Error(codes.Unavailable, "incomplete process output"))
	}
	if overflow {
		output.WriteString("\n[output truncated]")
	}
	if process.GetExitCode() != 0 {
		fmt.Fprintf(&output, "\nCommand exited with code %d.", process.GetExitCode())
	} else if output.Len() == 0 {
		output.WriteString("(no output)")
	}
	text := strings.ReplaceAll(strings.ToValidUTF8(output.String(), "\uFFFD"), "\x00", "\uFFFD")
	return r.result(ctx, w, c, text, process.GetExitCode() != 0)
}

func commandOverdue(c command) bool {
	var input bashInput
	// Running rows only originate from a successfully decoded Bash input.
	if err := json.Unmarshal(c.input, &input); err != nil {
		return true
	}
	return time.Since(c.startedAt) > input.timeout()+3*apiTimeout
}

func (r *Runner) collectFailure(ctx context.Context, w assignment, c command, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch status.Code(err) {
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument, codes.PermissionDenied, codes.Unauthenticated:
		return r.result(ctx, w, c, "The command's result is no longer available.", true)
	default:
		if commandOverdue(c) {
			return r.result(ctx, w, c, "Could not retrieve the command's result in time.", true)
		}
		return nil
	}
}

func (r *Runner) ensureSandbox(ctx context.Context, w *assignment, template *apiv1.ResourceReference) error {
	ctx, cancel := context.WithTimeout(ctx, lifecycleTimeout)
	defer cancel()
	for {
		err := r.prepareSandbox(ctx, w, template)
		if err == nil {
			return nil
		}
		switch status.Code(err) {
		case codes.Aborted, codes.Unavailable, codes.DeadlineExceeded, codes.FailedPrecondition:
			if wait(ctx, tickInterval) {
				continue
			}
		}
		return err
	}
}

func (r *Runner) prepareSandbox(ctx context.Context, w *assignment, template *apiv1.ResourceReference) error {
	call, cancel := context.WithTimeout(ctx, lifecycleTimeout/2)
	defer cancel()
	if w.sandboxID != "" {
		response, err := r.sandboxes.GetSandbox(call, &apiv1.GetSandboxRequest{SandboxId: w.sandboxID})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		instance := response.GetSandbox()
		if instance != nil && !sandboxGone(instance) {
			if instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_READY &&
				instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
				return nil
			}
			resumed, err := r.sandboxes.ResumeSandbox(call, &apiv1.ResumeSandboxRequest{SandboxId: w.sandboxID})
			if err != nil {
				return err
			}
			return requireReady(resumed.GetSandbox())
		}
		if err := r.store.saveSandbox(call, w, ""); err != nil {
			return err
		}
	}
	requestID := fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "%s:%d", w.id, w.generation)))
	created, err := r.sandboxes.CreateSandbox(call, &apiv1.CreateSandboxRequest{
		SandboxTemplate: template, RequestId: requestID, Name: "anthropic-" + requestID[:16],
	})
	if err != nil {
		return err
	}
	if err := requireReady(created.GetSandbox()); err != nil {
		return err
	}
	return r.store.saveSandbox(call, w, created.GetSandbox().GetId())
}

func requireReady(instance *apiv1.Sandbox) error {
	if instance.GetId() == "" || instance.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY ||
		instance.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE || sandboxGone(instance) {
		return status.Error(codes.FailedPrecondition, "sandbox is not ready")
	}
	return nil
}

func sandboxGone(instance *apiv1.Sandbox) bool {
	switch instance.GetState() {
	case apiv1.RuntimeState_RUNTIME_STATE_DELETING, apiv1.RuntimeState_RUNTIME_STATE_DELETED:
		return true
	}
	return !time.Now().Before(instance.GetExpiresAt().AsTime())
}

func (r *Runner) suspend(ctx context.Context, w assignment) bool {
	call, cancel := context.WithTimeout(ctx, lifecycleTimeout/2)
	defer cancel()
	response, err := r.sandboxes.SuspendSandbox(call, &apiv1.SuspendSandboxRequest{SandboxId: w.sandboxID})
	if err != nil && ctx.Err() == nil && status.Code(err) != codes.NotFound &&
		status.Code(err) != codes.FailedPrecondition {
		r.warn(ctx, "suspend idle sandbox", fmt.Errorf("sandbox API status %s", status.Code(err)))
	}
	return status.Code(err) == codes.NotFound || (err == nil &&
		response.GetSandbox().GetState() == apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED &&
		response.GetSandbox().GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE)
}
