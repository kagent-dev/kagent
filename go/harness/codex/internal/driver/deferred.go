package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pendingPhase string

const (
	phaseWaiting    pendingPhase = "waiting"
	phaseExecuting  pendingPhase = "executing"
	phaseResult     pendingPhase = "result"
	phaseContinuing pendingPhase = "continuing"
	phaseComplete   pendingPhase = "complete"
	phaseCanceled   pendingPhase = "canceled"
)

// pendingRecord contains no credentials or process identities. A call whose
// outcome is uncertain is never automatically re-executed after a restart.
type pendingRecord struct {
	Version   int                       `json:"version"`
	TaskID    string                    `json:"task_id"`
	ContextID string                    `json:"context_id"`
	ThreadID  string                    `json:"thread_id"`
	Phase     pendingPhase              `json:"phase"`
	Server    string                    `json:"server,omitempty"`
	Tool      string                    `json:"tool,omitempty"`
	Approval  *runtime.ApprovalRequest  `json:"approval,omitempty"`
	Question  *runtime.AskUserRequest   `json:"question,omitempty"`
	Decision  *runtime.ApprovalDecision `json:"decision,omitempty"`
	Answer    *runtime.AskUserResponse  `json:"answer,omitempty"`
	Result    *mcp.CallToolResult       `json:"result,omitempty"`
}

func (r *pendingRecord) request() runtime.InputRequest {
	if r.Approval != nil {
		return r.Approval
	}
	return r.Question
}

func (r *pendingRecord) validate() error {
	if r.Version != 1 || r.TaskID == "" || r.ContextID == "" || r.ThreadID == "" || (r.Approval == nil) == (r.Question == nil) {
		return fmt.Errorf("invalid Codex pending continuation identity")
	}
	if r.Approval != nil {
		if r.Approval.ID == "" || r.Approval.CallID == "" || r.Server == "" || r.Tool == "" || r.Approval.Name != r.Server+"."+r.Tool || r.Question != nil || r.Answer != nil {
			return fmt.Errorf("invalid Codex pending approval")
		}
	} else if r.Question.ID == "" || len(r.Question.Questions) == 0 || r.Server != "" || r.Tool != "" || r.Decision != nil {
		return fmt.Errorf("invalid Codex pending question")
	}
	switch r.Phase {
	case phaseWaiting:
		if r.Decision != nil || r.Answer != nil || r.Result != nil {
			return fmt.Errorf("waiting Codex continuation already has a response")
		}
	case phaseExecuting:
		if r.Approval == nil || r.Decision == nil || !r.Decision.Approved || r.Result != nil {
			return fmt.Errorf("invalid Codex executing continuation")
		}
	case phaseResult, phaseContinuing, phaseComplete:
		if r.Approval != nil && (r.Decision == nil || r.Result == nil) || r.Question != nil && r.Answer == nil {
			return fmt.Errorf("resolved Codex continuation has no response")
		}
	case phaseCanceled:
	default:
		return fmt.Errorf("unknown Codex pending continuation phase %q", r.Phase)
	}
	if r.Decision != nil {
		if _, err := codexInputResponse(r.request(), r.Decision); err != nil {
			return err
		}
	}
	if r.Answer != nil {
		if _, err := codexInputResponse(r.request(), r.Answer); err != nil {
			return err
		}
	}
	return nil
}

type deferredTurn struct {
	driver *ProcessDriver
	record pendingRecord
}

func (p *deferredTurn) Request() runtime.InputRequest { return p.record.request() }

// RestorePending is a local read, including after data is restored into an
// already-constructed golden process. The A2A executor owns task admission.
func (d *ProcessDriver) RestorePending() (string, string, runtime.PendingTurn, error) {
	r, err := d.loadPending()
	if err != nil || r == nil {
		return "", "", nil, err
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled {
		return "", "", nil, nil
	}
	return r.TaskID, r.ContextID, &deferredTurn{driver: d, record: *r}, nil
}

func (d *ProcessDriver) loadPending() (*pendingRecord, error) {
	if d.config.PendingDir == "" {
		return nil, nil
	}
	path := filepath.Join(d.config.PendingDir, "codex-pending.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Codex pending continuation: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > int64(d.config.MaxFrameBytes)*4 {
		return nil, fmt.Errorf("invalid Codex pending continuation file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Codex pending continuation: %w", err)
	}
	var r pendingRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return nil, fmt.Errorf("decode Codex pending continuation: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("Codex pending continuation contains trailing data")
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func (d *ProcessDriver) savePending(r *pendingRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode Codex pending continuation: %w", err)
	}
	if len(data) > d.config.MaxFrameBytes*4 {
		return fmt.Errorf("Codex pending continuation exceeds size limit")
	}
	if err := utils.ReplacePrivateFile(filepath.Join(d.config.PendingDir, "codex-pending.json"), data); err != nil {
		return fmt.Errorf("persist Codex pending continuation: %w", err)
	}
	return nil
}

func (d *ProcessDriver) deferInput(ctx context.Context, session *processSession, request runtime.InputRequest, sink runtime.EventSink) (runtime.Outcome, error) {
	r := pendingRecord{Version: 1, TaskID: session.turn.TaskID, ContextID: session.turn.ContextID,
		ThreadID: session.translator.threadID, Phase: phaseWaiting}
	callID := ""
	switch request := request.(type) {
	case *runtime.ApprovalRequest:
		tool := session.translator.tools[request.CallID]
		if tool.server == "" || tool.tool == "" || d.config.Tools == nil {
			return runtime.Outcome{}, fmt.Errorf("Codex approval has no configured tool execution boundary")
		}
		request.ID = uuid.NewString()
		r.Approval, r.Server, r.Tool = request, tool.server, tool.tool
		callID = request.CallID
	case *runtime.AskUserRequest:
		request.ID = uuid.NewString()
		r.Question = request
	default:
		return runtime.Outcome{}, fmt.Errorf("unsupported durable Codex input %T", request)
	}
	if err := r.validate(); err != nil {
		return runtime.Outcome{}, err
	}
	// Never release the approval RPC. Interrupting while it is unanswered
	// prevents Codex from executing the protected call before we take ownership.
	if err := d.interruptForCheckpoint(ctx, session, callID, sink); err != nil {
		return runtime.Outcome{}, err
	}
	// Run owns process cleanup. It closes stdin and waits for exit before the
	// caller can publish input-required and snapshot the durable directory.
	if err := d.savePending(&r); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Outcome{Pending: &deferredTurn{driver: d, record: r}}, nil
}

// interruptForCheckpoint observes both the response and terminal notification;
// rpcClient.call intentionally discards notifications and cannot do this job.
func (d *ProcessDriver) interruptForCheckpoint(ctx context.Context, session *processSession, callID string, sink runtime.EventSink) error {
	ctx, cancel := context.WithTimeout(ctx, d.config.InterruptGrace)
	defer cancel()
	if err := session.client.write(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage("4"), Method: "turn/interrupt"}, struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
	}{session.translator.threadID, session.translator.turnID}); err != nil {
		return err
	}
	ack, terminal := false, false
	filtered := checkpointSink{EventSink: sink, callID: callID}
	for !ack || !terminal {
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupt Codex before checkpoint: %w", ctx.Err())
		case frame, ok := <-session.client.frames:
			if !ok {
				return fmt.Errorf("Codex closed before confirming interruption")
			}
			if frame.err != nil {
				return frame.err
			}
			message := frame.message
			if message.Method == "" {
				if string(message.ID) != "4" || message.Error != nil {
					return fmt.Errorf("Codex failed to acknowledge interruption")
				}
				ack = true
				continue
			}
			if len(message.ID) != 0 {
				// Other callbacks are canceled with the turn. This checkpoint
				// preserves only the input request that triggered interruption.
				continue
			}
			outcome, done, err := session.translator.translate(message, filtered)
			if err != nil {
				return err
			}
			if done {
				if outcome.Failure == nil || outcome.Failure.Message != "Codex execution was interrupted" {
					return fmt.Errorf("Codex did not stop at the input boundary")
				}
				terminal = true
			}
		}
	}
	return nil
}

type checkpointSink struct {
	runtime.EventSink
	callID string
}

func (s checkpointSink) ToolResult(result runtime.ToolResult) error {
	if result.ID != "" && result.ID == s.callID {
		return nil // Keep the public call unresolved until the human decides.
	}
	return s.EventSink.ToolResult(result)
}

func (p *deferredTurn) current() (*pendingRecord, error) {
	r, err := p.driver.loadPending()
	if err != nil {
		return nil, err
	}
	if r == nil || r.TaskID != p.record.TaskID || r.ContextID != p.record.ContextID || r.ThreadID != p.record.ThreadID || !reflect.DeepEqual(r.request(), p.record.request()) {
		return nil, fmt.Errorf("Codex pending continuation no longer matches")
	}
	return r, nil
}

func (p *deferredTurn) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (outcome runtime.Outcome, returnErr error) {
	defer func() {
		if ctx.Err() == nil {
			return
		}
		// The original native turn was already interrupted at the waiting
		// boundary. Once this attempt has released its MCP/native resources,
		// cancellation needs a tombstone, not another native replay. Cancel a
		// replacement request if this attempt reached another input boundary.
		pending := outcome.Pending
		if pending == nil {
			pending = p
		}
		if err := pending.Cancel(context.WithoutCancel(ctx)); err != nil {
			outcome = runtime.Outcome{}
			returnErr = &runtime.CancellationError{Err: errors.Join(returnErr, err)}
			return
		}
		outcome, returnErr = runtime.Outcome{}, context.Canceled
	}()
	r, err := p.current()
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err := codexInputResponse(r.request(), response); err != nil {
		return runtime.Outcome{}, err
	}
	if r.Phase == phaseExecuting || r.Phase == phaseContinuing {
		return runtime.Outcome{}, fmt.Errorf("Codex continuation outcome is uncertain; automatic replay is disabled")
	}
	if r.Phase != phaseWaiting && r.Phase != phaseResult {
		return runtime.Outcome{}, fmt.Errorf("Codex continuation is already resolved")
	}
	if r.Phase == phaseResult {
		if decision, ok := response.(*runtime.ApprovalDecision); ok && !reflect.DeepEqual(r.Decision, decision) {
			return runtime.Outcome{}, fmt.Errorf("Codex approval decision changed after execution")
		}
		if answer, ok := response.(*runtime.AskUserResponse); ok && !reflect.DeepEqual(r.Answer, answer) {
			return runtime.Outcome{}, fmt.Errorf("Codex answer changed after resolution")
		}
	} else {
		switch response := response.(type) {
		case *runtime.ApprovalDecision:
			r.Decision = response
			if response.Approved {
				if p.driver.config.Tools == nil {
					return p.repark(ctx, r, phaseWaiting, fmt.Errorf("Codex approval has no configured tool execution boundary"))
				}
				r.Phase = phaseExecuting
				if err := p.driver.savePending(r); err != nil {
					return runtime.Outcome{}, err
				}
				r.Result, err = p.driver.config.Tools.Call(ctx, r.Server, r.Tool, r.Approval.Args)
				if err != nil {
					var notExecuted *toolNotExecutedError
					if errors.As(err, &notExecuted) {
						return p.repark(ctx, r, phaseWaiting, err)
					}
					return runtime.Outcome{}, fmt.Errorf("execute deferred Codex tool (outcome may be uncertain): %w", err)
				}
			} else {
				r.Result = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Tool call denied: " + response.RejectionReason}}}
			}
		case *runtime.AskUserResponse:
			r.Answer = response
		}
		r.Phase = phaseResult
		if err := p.driver.savePending(r); err != nil {
			return runtime.Outcome{}, err
		}
	}
	turn := runtime.Turn{TaskID: r.TaskID, ContextID: r.ContextID, ContinuationID: r.ThreadID}
	var tool *toolContinuation
	if r.Approval != nil {
		data, err := json.Marshal(r.Result)
		if err != nil {
			return runtime.Outcome{}, err
		}
		namespace := "mcp__" + r.Server
		tool = &toolContinuation{
			output: turnToolOutput{Name: r.Tool, Namespace: &namespace, Output: string(data)},
			result: runtime.ToolResult{ID: r.Approval.CallID, Name: r.Approval.Name, Result: r.Result, IsError: r.Result.IsError},
		}
	} else {
		data, err := json.Marshal(struct {
			Questions []runtime.AskUserQuestion `json:"questions"`
			Answers   [][]string                `json:"answers"`
		}{r.Question.Questions, r.Answer.Answers})
		if err != nil {
			return runtime.Outcome{}, err
		}
		turn.Prompt = "Continue the interrupted task using these answers to your pending questions: " + string(data)
	}
	r.Phase = phaseContinuing
	if err := p.driver.savePending(r); err != nil {
		return runtime.Outcome{}, err
	}
	outcome, err = p.driver.run(ctx, turn, tool, sink)
	var notStarted *turnNotStartedError
	if errors.As(err, &notStarted) {
		return p.repark(ctx, r, phaseResult, err)
	}
	if err != nil || outcome.Pending != nil {
		return outcome, err // Another input request has already replaced the record.
	}
	r.Phase = phaseComplete
	if err := p.driver.savePending(r); err != nil {
		return runtime.Outcome{}, err
	}
	return outcome, nil
}

// repark preserves the same public request after proven non-execution. Saved
// results require the same response on retry and never invoke MCP again.
func (p *deferredTurn) repark(ctx context.Context, r *pendingRecord, phase pendingPhase, cause error) (runtime.Outcome, error) {
	if phase != phaseWaiting && phase != phaseResult {
		return runtime.Outcome{}, fmt.Errorf("invalid Codex retry phase %q", phase)
	}
	r.Phase = phase
	if phase == phaseWaiting {
		r.Decision, r.Answer, r.Result = nil, nil, nil
	}
	if err := p.driver.savePending(r); err != nil {
		return runtime.Outcome{}, errors.Join(cause, err)
	}
	logging.FromContext(ctx).ErrorContext(ctx, "codex continuation remains waiting after non-execution", "phase", phase, "error", cause)
	return runtime.Outcome{Pending: &deferredTurn{driver: p.driver, record: *r}}, nil
}

func (p *deferredTurn) Cancel(_ context.Context) error {
	r, err := p.current()
	if err != nil {
		return err
	}
	r.Phase = phaseCanceled
	return p.driver.savePending(r)
}

var _ runtime.PendingTurn = (*deferredTurn)(nil)
