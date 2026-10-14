package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	phaseWaiting   = "waiting"
	phaseResuming  = "resuming"
	phaseExecuting = "executing"
	phaseReleased  = "released"
	phaseComplete  = "complete"
	phaseCanceled  = "canceled"
	phaseAbandoned = "abandoned"
)

// DeferredTool is Claude's native deferred_tool_use payload, before execution.
type DeferredTool struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type deferredRecord struct {
	Version   int                       `json:"version"`
	TaskID    string                    `json:"taskId"`
	ContextID string                    `json:"contextId"`
	Origin    runtime.TraceOrigin       `json:"origin,omitzero"`
	SessionID string                    `json:"sessionId"`
	RequestID string                    `json:"requestId"`
	Canceling bool                      `json:"canceling,omitempty"`
	Phase     string                    `json:"phase"`
	Tool      DeferredTool              `json:"tool"`
	Decision  *runtime.ApprovalDecision `json:"decision,omitempty"`
	Answer    *runtime.AskUserResponse  `json:"answer,omitempty"`
}

type nativeQuestion struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
	Multiple bool `json:"multiSelect"`
}

func (r *deferredRecord) request() (runtime.InputRequest, error) {
	if r.Tool.Name != "AskUserQuestion" {
		return &runtime.ApprovalRequest{ID: r.RequestID, CallID: r.Tool.ID, Name: r.Tool.Name, Args: r.Tool.Input, Hint: "Claude requires approval before calling " + r.Tool.Name + "."}, nil
	}
	data, err := json.Marshal(r.Tool.Input)
	if err != nil {
		return nil, err
	}
	var input struct {
		Questions []nativeQuestion `json:"questions"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}
	if len(input.Questions) == 0 {
		return nil, fmt.Errorf("Claude deferred question has no questions")
	}
	request := &runtime.AskUserRequest{ID: r.RequestID}
	seen := make(map[string]struct{}, len(input.Questions))
	for i, q := range input.Questions {
		if q.Question == "" {
			return nil, fmt.Errorf("Claude deferred question is empty")
		}
		if _, ok := seen[q.Question]; ok {
			return nil, fmt.Errorf("Claude deferred question text is duplicated")
		}
		seen[q.Question] = struct{}{}
		question := runtime.AskUserQuestion{ID: fmt.Sprintf("%s/%d", r.Tool.ID, i), Question: q.Question, Multiple: q.Multiple}
		for _, option := range q.Options {
			question.Choices = append(question.Choices, option.Label)
		}
		request.Questions = append(request.Questions, question)
	}
	return request, nil
}

func (r *deferredRecord) validate() error {
	if r.Version != 1 || r.TaskID == "" || r.ContextID == "" || r.SessionID == "" || r.RequestID == "" || r.Tool.ID == "" || r.Tool.Name == "" {
		return fmt.Errorf("invalid Claude deferred identity")
	}
	request, err := r.request()
	if err != nil {
		return err
	}
	if r.Decision != nil {
		if _, ok := request.(*runtime.ApprovalRequest); !ok || r.Decision.ID != r.RequestID || r.Answer != nil {
			return fmt.Errorf("invalid Claude deferred decision")
		}
	}
	if r.Answer != nil {
		q, ok := request.(*runtime.AskUserRequest)
		if !ok || r.Answer.ID != r.RequestID || len(r.Answer.Answers) != len(q.Questions) {
			return fmt.Errorf("invalid Claude deferred answer")
		}
		for i, answer := range r.Answer.Answers {
			if len(answer) == 0 || !q.Questions[i].Multiple && len(answer) != 1 {
				return fmt.Errorf("invalid Claude deferred answer selection")
			}
		}
	}
	switch r.Phase {
	case phaseWaiting:
		if r.Decision != nil || r.Answer != nil || r.Canceling {
			return fmt.Errorf("waiting Claude input already has a response")
		}
	case phaseResuming, phaseExecuting, phaseReleased, phaseComplete:
		if r.Decision == nil && r.Answer == nil && !r.Canceling {
			return fmt.Errorf("Claude continuation has no response")
		}
	case phaseCanceled, phaseAbandoned:
	default:
		return fmt.Errorf("unknown Claude deferred phase %q", r.Phase)
	}
	return nil
}

func loadDeferred(directory string, max int) (*deferredRecord, error) {
	path := filepath.Join(directory, "claude-pending.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Claude deferred input: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > int64(max) {
		return nil, fmt.Errorf("invalid Claude deferred file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Claude deferred input: %w", err)
	}
	var r deferredRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return nil, fmt.Errorf("decode Claude deferred input: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("Claude deferred file contains trailing data")
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func saveDeferred(directory string, max int, r *deferredRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode Claude deferred input: %w", err)
	}
	if len(data) > max {
		return fmt.Errorf("Claude deferred input exceeds size limit")
	}
	if err := utils.ReplacePrivateFile(filepath.Join(directory, "claude-pending.json"), data); err != nil {
		return fmt.Errorf("persist Claude deferred input: %w", err)
	}
	return nil
}

func equalArguments(a, b map[string]any) bool {
	// Compare wire values, including numbers decoded by separate JSON readers.
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

func (r *deferredRecord) hookDecision() (string, map[string]any, error) {
	if err := r.validate(); err != nil {
		return "", nil, err
	}
	if r.Canceling {
		return "deny", nil, nil
	}
	if r.Decision != nil {
		if r.Decision.Approved {
			return "allow", r.Tool.Input, nil
		}
		return "deny", nil, nil
	}
	request, err := r.request()
	if err != nil {
		return "", nil, err
	}
	questions := request.(*runtime.AskUserRequest)
	answers := make(map[string]string, len(questions.Questions))
	for i, q := range questions.Questions {
		answers[q.Question] = strings.Join(r.Answer.Answers[i], ", ")
	}
	updated := maps.Clone(r.Tool.Input)
	updated["answers"] = answers
	return "allow", updated, nil
}

// RestorePending reads DATA lazily, including after restoring into a golden
// harness whose executor and ephemeral hook listener already exist.
func (d *ProcessDriver) RestorePending() (string, string, runtime.PendingTurn, error) {
	if d.config.ApprovalBroker == nil {
		return "", "", nil, nil
	}
	b := d.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := loadDeferred(b.directory, d.config.MaxEventBytes)
	if err != nil || r == nil {
		return "", "", nil, err
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled {
		return "", "", nil, nil
	}
	if r.Phase == phaseAbandoned || r.Phase == phaseResuming || r.Phase == phaseExecuting || r.Phase == phaseReleased {
		return "", "", nil, runtime.ErrSessionAbandoned
	}
	if !b.protects(r.Tool.Name) {
		return "", "", nil, fmt.Errorf("Claude deferred tool is no longer protected")
	}
	request, err := r.request()
	if err != nil {
		return "", "", nil, err
	}
	return r.TaskID, r.ContextID, &deferredTurn{driver: d, record: *r, request: request}, nil
}

func (d *ProcessDriver) parkDeferred(turn runtime.Turn, session string, tool *DeferredTool) (runtime.Outcome, error) {
	b := d.config.ApprovalBroker
	if b == nil || !b.protects(tool.Name) {
		return runtime.Outcome{}, fmt.Errorf("unsupported Claude deferred tool")
	}
	r := deferredRecord{Version: 1, TaskID: turn.TaskID, ContextID: turn.ContextID, SessionID: session, RequestID: uuid.NewString(), Origin: turn.Origin, Phase: phaseWaiting, Tool: *tool}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := saveDeferred(b.directory, d.config.MaxEventBytes, &r); err != nil {
		return runtime.Outcome{}, err
	}
	request, err := r.request()
	if err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Outcome{Pending: &deferredTurn{driver: d, record: r, request: request}}, nil
}

type deferredTurn struct {
	driver  *ProcessDriver
	record  deferredRecord
	request runtime.InputRequest
}

func (p *deferredTurn) Request() runtime.InputRequest { return p.request }

func (p *deferredTurn) TraceOrigin() runtime.TraceOrigin { return p.record.Origin }

// current requires the broker mutex. Disk is authoritative after DATA restore.
func (p *deferredTurn) current() (*deferredRecord, error) {
	b := p.driver.config.ApprovalBroker
	r, err := loadDeferred(b.directory, p.driver.config.MaxEventBytes)
	if err != nil {
		return nil, err
	}
	if r == nil || r.RequestID != p.record.RequestID || r.TaskID != p.record.TaskID || r.ContextID != p.record.ContextID || r.SessionID != p.record.SessionID || r.Tool.ID != p.record.Tool.ID || r.Tool.Name != p.record.Tool.Name || !equalArguments(r.Tool.Input, p.record.Tool.Input) {
		return nil, fmt.Errorf("Claude deferred continuation no longer matches")
	}
	return r, nil
}

// cancelReplay leaves the release phase intact: a tool already in flight must
// finish, while the hooks deny any further tool and stop after its result.
func (d *ProcessDriver) cancelReplay(tool *DeferredTool) error {
	b := d.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := loadDeferred(b.directory, d.config.MaxEventBytes)
	if err != nil {
		return err
	}
	if r == nil || r.Tool.ID != tool.ID || r.Tool.Name != tool.Name || !equalArguments(r.Tool.Input, tool.Input) {
		return fmt.Errorf("Claude replay changed during cancellation")
	}
	r.Canceling = true
	return saveDeferred(b.directory, d.config.MaxEventBytes, r)
}

// beginResume commits the response before native replay. Its lock covers only
// the journal transition; process execution and failure cleanup happen outside it.
func (p *deferredTurn) beginResume(response runtime.InputResponse) (*deferredRecord, error) {
	b := p.driver.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := p.current()
	if err != nil {
		return nil, err
	}
	if r.Phase != phaseWaiting {
		return nil, fmt.Errorf("Claude continuation is resolved or uncertain; automatic replay is disabled")
	}
	switch value := response.(type) {
	case *runtime.ApprovalDecision:
		r.Decision = value
	case *runtime.AskUserResponse:
		r.Answer = value
	default:
		return nil, fmt.Errorf("unsupported Claude deferred response %T", response)
	}
	r.Phase = phaseResuming
	if err := saveDeferred(b.directory, p.driver.config.MaxEventBytes, r); err != nil {
		return nil, err
	}
	return r, nil
}

// beginCancel retains the previous state so a proven process-start failure can
// leave cancellation retryable without holding the journal lock during replay.
func (p *deferredTurn) beginCancel() (*deferredRecord, deferredRecord, error) {
	b := p.driver.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := p.current()
	if err != nil {
		return nil, deferredRecord{}, err
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled || r.Phase == phaseAbandoned {
		return nil, deferredRecord{}, fmt.Errorf("Claude continuation is already resolved")
	}
	previous := *r
	r.Phase, r.Canceling, r.Decision, r.Answer = phaseResuming, true, nil, nil
	if err := saveDeferred(b.directory, p.driver.config.MaxEventBytes, r); err != nil {
		return nil, deferredRecord{}, err
	}
	return r, previous, nil
}

func (p *deferredTurn) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (outcome runtime.Outcome, returnErr error) {
	defer func() {
		// Only the explicit successful tombstone path returns context.Canceled.
		// All other errors during cancellation leave cleanup unacknowledged.
		if ctx.Err() != nil && returnErr != nil && returnErr != context.Canceled {
			var cancellationErr *runtime.CancellationError
			if !errors.As(returnErr, &cancellationErr) {
				returnErr = &runtime.CancellationError{Err: returnErr}
			}
		}
	}()
	defer func() {
		if returnErr != nil && returnErr != context.Canceled {
			returnErr = p.abandon(returnErr)
		}
	}()
	r, err := p.beginResume(response)
	if err != nil {
		return runtime.Outcome{}, err
	}
	b := p.driver.config.ApprovalBroker
	telemetry, observed := sink.(runtime.ToolExecutionSink)
	executing := r.Decision == nil || r.Decision.Approved
	if observed && executing {
		server, tool := splitMCPName(r.Tool.Name)
		telemetry.ToolExecutionStarted(runtime.ToolCall{ID: r.Tool.ID, Name: r.Tool.Name, Arguments: r.Tool.Input, MCPServer: server, MCPTool: tool})
	}
	sink = deferredResultSink{EventSink: sink, callID: r.Tool.ID, declined: !executing}
	outcome, err = p.driver.run(ctx, runtime.Turn{TaskID: r.TaskID, ContextID: r.ContextID, ContinuationID: r.SessionID, Origin: r.Origin}, sink, &r.Tool)
	var startErr *processStartError
	if errors.As(err, &startErr) {
		if observed && executing {
			telemetry.ToolExecutionFinished(runtime.ToolResult{ID: r.Tool.ID, Status: runtime.ToolDeclined})
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		current, restoreErr := p.current()
		if restoreErr != nil {
			return runtime.Outcome{}, errors.Join(err, restoreErr)
		}
		if current.Phase != phaseResuming {
			return runtime.Outcome{}, fmt.Errorf("Claude input changed before process launch: %w", err)
		}
		current.Phase, current.Decision, current.Answer, current.Canceling = phaseWaiting, nil, nil, false
		if restoreErr := saveDeferred(b.directory, p.driver.config.MaxEventBytes, current); restoreErr != nil {
			return runtime.Outcome{}, errors.Join(err, restoreErr)
		}
		logging.FromContext(ctx).ErrorContext(ctx, "failed to start claude replay; input remains waiting", "error", err)
		return runtime.Outcome{Pending: p}, nil
	}
	if err != nil || outcome.Pending != nil {
		return outcome, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, err := p.current()
	if err != nil {
		return runtime.Outcome{}, err
	}
	// A completed process must have consumed the decision through its exact hook.
	if current.Phase != phaseExecuting && current.Phase != phaseReleased {
		if current.Canceling || ctx.Err() != nil {
			return runtime.Outcome{}, &runtime.CancellationError{Err: fmt.Errorf("Claude did not consume its cancellation")}
		}
		return runtime.Outcome{}, fmt.Errorf("Claude did not consume its deferred decision")
	}
	current.Phase = phaseComplete
	if current.Canceling || ctx.Err() != nil {
		current.Phase, current.Canceling = phaseCanceled, true
	}
	if err := saveDeferred(b.directory, p.driver.config.MaxEventBytes, current); err != nil {
		if current.Canceling {
			return runtime.Outcome{}, &runtime.CancellationError{Err: err}
		}
		return runtime.Outcome{}, err
	}
	if current.Canceling {
		return runtime.Outcome{}, context.Canceled
	}
	return outcome, nil
}

// Cancel resolves the native deferred call with a stop decision before writing
// the tombstone, so the next task cannot revive the canceled call on --resume.
func (p *deferredTurn) Cancel(ctx context.Context, sink runtime.ModelCallSink) (returnErr error) {
	r, previous, err := p.beginCancel()
	if err != nil {
		return err
	}
	b := p.driver.config.ApprovalBroker
	outcome, err := p.driver.run(ctx, runtime.Turn{TaskID: r.TaskID, ContextID: r.ContextID, ContinuationID: r.SessionID, Origin: r.Origin}, discardSink{ModelCallSink: sink}, &r.Tool)
	var startErr *processStartError
	if errors.As(err, &startErr) {
		// No cleanup process ran, so even cancellation remains retryable.
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, restoreErr := p.current(); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return errors.Join(err, saveDeferred(b.directory, p.driver.config.MaxEventBytes, &previous))
	}
	defer func() {
		if returnErr != nil {
			returnErr = p.abandon(returnErr)
		}
	}()
	if err != nil {
		return err
	}
	if outcome.Pending != nil {
		return fmt.Errorf("Claude did not resolve canceled input")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current, err := p.current()
	if err != nil {
		return err
	}
	if current.Phase != phaseExecuting && current.Phase != phaseReleased {
		return fmt.Errorf("Claude did not consume its cancellation")
	}
	current.Phase = phaseCanceled
	return saveDeferred(b.directory, p.driver.config.MaxEventBytes, current)
}

// abandon prevents a failed task from being restored as pending input.
func (p *deferredTurn) abandon(cause error) error {
	b := p.driver.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := p.current()
	if err != nil {
		return errors.Join(cause, err)
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled {
		return cause
	}
	r.Phase = phaseAbandoned
	if err := saveDeferred(b.directory, p.driver.config.MaxEventBytes, r); err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(runtime.ErrSessionAbandoned, cause)
}

type discardSink struct{ runtime.ModelCallSink }

func (discardSink) SessionStarted(runtime.SessionStarted) error { return nil }
func (discardSink) TextDelta(runtime.TextDelta) error           { return nil }
func (discardSink) ToolCall(runtime.ToolCall) error             { return nil }
func (discardSink) ToolResult(runtime.ToolResult) error         { return nil }

var _ runtime.EventSink = discardSink{}

var _ runtime.PendingTurn = (*deferredTurn)(nil)

type deferredResultSink struct {
	runtime.EventSink
	callID   string
	declined bool
}

func (s deferredResultSink) ToolResult(result runtime.ToolResult) error {
	if result.ID == s.callID && s.declined {
		result.Status = runtime.ToolDeclined
	}
	return s.EventSink.ToolResult(result)
}
