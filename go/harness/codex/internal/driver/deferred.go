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
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/internal/utils"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type pendingPhase string

const (
	phaseWaiting    pendingPhase = "waiting"
	phaseExecuting  pendingPhase = "executing"
	phaseResult     pendingPhase = "result"
	phaseContinuing pendingPhase = "continuing"
	phaseComplete   pendingPhase = "complete"
	phaseCanceled   pendingPhase = "canceled"
	phaseAbandoned  pendingPhase = "abandoned"
)

// pendingRecord contains no credentials or process identities. A call whose
// outcome is uncertain is never automatically re-executed after a restart.
type pendingRecord struct {
	Version   int                       `json:"version"`
	TaskID    string                    `json:"task_id"`
	ContextID string                    `json:"context_id"`
	Origin    runtime.TraceOrigin       `json:"origin,omitzero"`
	ThreadID  string                    `json:"thread_id"`
	Phase     pendingPhase              `json:"phase"`
	Server    string                    `json:"server,omitempty"`
	Tool      string                    `json:"tool,omitempty"`
	Approval  *runtime.ApprovalRequest  `json:"approval,omitempty"`
	Question  *runtime.AskUserRequest   `json:"question,omitempty"`
	Decision  *runtime.ApprovalDecision `json:"decision,omitempty"`
	Answer    *runtime.AskUserResponse  `json:"answer,omitempty"`
	Result    *mcp.CallToolResult       `json:"result,omitempty"`
	// Elicitation parks the call that asked for it, with CallID and Arguments,
	// so that Action accept retries exactly that call.
	Elicitation *runtime.URLElicitationRequest  `json:"elicitation,omitempty"`
	CallID      string                          `json:"call_id,omitempty"`
	Arguments   map[string]any                  `json:"arguments,omitempty"`
	Action      *runtime.URLElicitationResponse `json:"action,omitempty"`
}

func (r *pendingRecord) request() runtime.InputRequest {
	switch {
	case r.Approval != nil:
		return r.Approval
	case r.Elicitation != nil:
		return r.Elicitation
	default:
		return r.Question
	}
}

// call is the MCP call that Resume executes or answers for the model.
func (r *pendingRecord) call() (id, name string, arguments map[string]any) {
	if r.Approval != nil {
		return r.Approval.CallID, r.Approval.Name, r.Approval.Args
	}
	return r.CallID, r.Server + "." + r.Tool, r.Arguments
}

// executes reports whether the saved response runs the call.
func (r *pendingRecord) executes() bool {
	return r.Decision != nil && r.Decision.Approved || r.Action != nil && r.Action.Action == apia2a.URLElicitationAccept
}

func (r *pendingRecord) validate() error {
	requests := 0
	for _, present := range []bool{r.Approval != nil, r.Question != nil, r.Elicitation != nil} {
		if present {
			requests++
		}
	}
	if r.Version != 1 || r.TaskID == "" || r.ContextID == "" || r.ThreadID == "" || requests != 1 {
		return fmt.Errorf("invalid Codex pending continuation identity")
	}
	switch {
	case r.Approval != nil:
		if r.Approval.ID == "" || r.Approval.CallID == "" || r.Server == "" || r.Tool == "" || r.Approval.Name != r.Server+"."+r.Tool || r.Answer != nil || r.Action != nil || r.CallID != "" || r.Arguments != nil {
			return fmt.Errorf("invalid Codex pending approval")
		}
	case r.Elicitation != nil:
		if r.Elicitation.ID == "" || apia2a.ValidateElicitationURL(r.Elicitation.URL) != nil || r.Elicitation.Server != r.Server || r.CallID == "" || r.Server == "" || r.Tool == "" || r.Decision != nil || r.Answer != nil {
			return fmt.Errorf("invalid Codex pending URL elicitation")
		}
	case r.Question.ID == "" || len(r.Question.Questions) == 0 || r.Server != "" || r.Tool != "" || r.Decision != nil || r.Action != nil || r.CallID != "" || r.Arguments != nil:
		return fmt.Errorf("invalid Codex pending question")
	}
	responded := r.Decision != nil || r.Answer != nil || r.Action != nil
	switch r.Phase {
	case phaseWaiting:
		if responded || r.Result != nil {
			return fmt.Errorf("waiting Codex continuation already has a response")
		}
	case phaseExecuting:
		if r.Question != nil || !r.executes() || r.Result != nil {
			return fmt.Errorf("invalid Codex executing continuation")
		}
	case phaseResult, phaseContinuing, phaseComplete:
		if !responded || r.Question == nil && r.Result == nil {
			return fmt.Errorf("resolved Codex continuation has no response")
		}
	case phaseCanceled, phaseAbandoned:
	default:
		return fmt.Errorf("unknown Codex pending continuation phase %q", r.Phase)
	}
	if r.Decision != nil {
		if err := validateInputResponse(r.request(), r.Decision); err != nil {
			return err
		}
	}
	if r.Answer != nil {
		if err := validateInputResponse(r.request(), r.Answer); err != nil {
			return err
		}
	}
	if r.Action != nil {
		if err := validateInputResponse(r.request(), r.Action); err != nil {
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

func (p *deferredTurn) TraceOrigin() runtime.TraceOrigin { return p.record.Origin }

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
	if r.Phase == phaseAbandoned || r.Phase == phaseExecuting || r.Phase == phaseContinuing {
		return "", "", nil, runtime.ErrSessionAbandoned
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
	if d.config.PendingDir == "" {
		return runtime.Outcome{}, fmt.Errorf("Codex pending input requires a durable directory")
	}
	r := pendingRecord{Version: 1, TaskID: session.turn.TaskID, ContextID: session.turn.ContextID,
		ThreadID: session.translator.threadID, Origin: session.turn.Origin, Phase: phaseWaiting}
	callID := ""
	switch request := request.(type) {
	case *runtime.ApprovalRequest:
		tool := session.translator.tools[request.CallID]
		if tool.server == "" || tool.tool == "" || d.config.Tools == nil {
			return runtime.Outcome{}, fmt.Errorf("Codex approval has no configured tool execution boundary")
		}
		r.Approval, r.Server, r.Tool = request, tool.server, tool.tool
		callID = request.CallID
	case *runtime.AskUserRequest:
		r.Question = request
	case *runtime.URLElicitationRequest:
		id, ok := session.translator.elicitedTool(session.translator.threadID, request.Server)
		tool := session.translator.tools[id]
		arguments, isMap := tool.arguments.(map[string]any)
		if !ok || tool.tool == "" || tool.arguments != nil && !isMap || d.config.Tools == nil {
			return runtime.Outcome{}, fmt.Errorf("Codex URL elicitation has no configured tool execution boundary")
		}
		r.Elicitation, r.Server, r.Tool, r.CallID, r.Arguments = request, tool.server, tool.tool, id, arguments
		callID = id
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

// interruptForCheckpoint waits for both the RPC response and terminal notification.
// An interrupt acknowledgement alone does not establish a checkpoint boundary.
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
		if err := pending.Cancel(context.WithoutCancel(ctx), sink); err != nil {
			outcome = runtime.Outcome{}
			returnErr = &runtime.CancellationError{Err: p.abandon(errors.Join(returnErr, err))}
			return
		}
		outcome, returnErr = runtime.Outcome{}, context.Canceled
	}()
	defer func() {
		if returnErr != nil && ctx.Err() == nil {
			returnErr = p.abandon(returnErr)
		}
	}()
	r, err := p.current()
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err := validateInputResponse(r.request(), response); err != nil {
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
		if action, ok := response.(*runtime.URLElicitationResponse); ok && !reflect.DeepEqual(r.Action, action) {
			return runtime.Outcome{}, fmt.Errorf("Codex URL elicitation answer changed after execution")
		}
	}
	if r.Phase != phaseResult {
		switch response := response.(type) {
		case *runtime.ApprovalDecision:
			r.Decision = response
			if !response.Approved {
				r.Result = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Tool call denied: " + response.RejectionReason}}}
			}
		case *runtime.AskUserResponse:
			r.Answer = response
		case *runtime.URLElicitationResponse:
			r.Action = response
			if text := declinedElicitation(r.Server, response.Action); text != "" {
				r.Result = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
			}
		}
		if r.executes() {
			if err := p.execute(ctx, r, sink); err != nil {
				var notExecuted *toolNotExecutedError
				if errors.As(err, &notExecuted) {
					if notExecuted.elicitation != nil {
						return p.reparkElicitation(ctx, r, notExecuted.elicitation, err)
					}
					return p.repark(ctx, r, phaseWaiting, err)
				}
				return runtime.Outcome{}, fmt.Errorf("execute deferred Codex tool (outcome may be uncertain): %w", err)
			}
		}
		r.Phase = phaseResult
		if err := p.driver.savePending(r); err != nil {
			return runtime.Outcome{}, err
		}
	}
	turn := runtime.Turn{TaskID: r.TaskID, ContextID: r.ContextID, ContinuationID: r.ThreadID, Origin: r.Origin}
	var tool *toolContinuation
	if r.Question == nil {
		data, err := json.Marshal(r.Result)
		if err != nil {
			return runtime.Outcome{}, err
		}
		namespace := "mcp__" + r.Server
		callID, name, _ := r.call()
		tool = &toolContinuation{
			output: turnToolOutput{Name: r.Tool, Namespace: &namespace, Output: string(data)},
			result: runtime.ToolResult{ID: callID, Name: name, Result: r.Result, IsError: r.Result.IsError},
		}
		if !r.executes() {
			tool.result.Status = runtime.ToolDeclined
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

// execute runs the saved MCP call once the person allowed it. Its error is a
// *toolNotExecutedError when no tools/call request was sent.
func (p *deferredTurn) execute(ctx context.Context, r *pendingRecord, sink runtime.EventSink) error {
	if p.driver.config.Tools == nil {
		return &toolNotExecutedError{err: fmt.Errorf("Codex has no configured tool execution boundary")}
	}
	r.Phase = phaseExecuting
	if err := p.driver.savePending(r); err != nil {
		return err
	}
	callID, name, arguments := r.call()
	telemetry, observed := sink.(runtime.ToolExecutionSink)
	if observed {
		telemetry.ToolExecutionStarted(runtime.ToolCall{ID: callID, Name: name, Arguments: arguments, MCPServer: r.Server, MCPTool: r.Tool})
	}
	var err error
	r.Result, err = p.driver.config.Tools.Call(ctx, r.Server, r.Tool, arguments)
	if observed {
		status := runtime.ToolRan
		var notExecuted *toolNotExecutedError
		if errors.As(err, &notExecuted) {
			status = runtime.ToolDeclined
		}
		telemetry.ToolExecutionFinished(runtime.ToolResult{ID: callID, Name: name, Result: r.Result, IsError: err != nil || r.Result != nil && r.Result.IsError, Status: status})
	}
	return err
}

// declinedElicitation is the tool result the model sees when the person did
// not complete a URL elicitation, or empty after accept.
func declinedElicitation(server, action string) string {
	switch action {
	case apia2a.URLElicitationDecline:
		return fmt.Sprintf("The person declined the interaction that the MCP server %q asked for at a URL, so the tool call could not complete.", server)
	case apia2a.URLElicitationCancel:
		return fmt.Sprintf("The person canceled the interaction that the MCP server %q asked for at a URL, so the tool call could not complete.", server)
	default:
		return ""
	}
}

// repark preserves the same public request after proven non-execution. Saved
// results require the same response on retry and never invoke MCP again.
func (p *deferredTurn) repark(ctx context.Context, r *pendingRecord, phase pendingPhase, cause error) (runtime.Outcome, error) {
	if phase != phaseWaiting && phase != phaseResult {
		return runtime.Outcome{}, fmt.Errorf("invalid Codex retry phase %q", phase)
	}
	r.Phase = phase
	if phase == phaseWaiting {
		r.Decision, r.Answer, r.Action, r.Result = nil, nil, nil, nil
	}
	if err := p.driver.savePending(r); err != nil {
		return runtime.Outcome{}, errors.Join(cause, err)
	}
	logging.FromContext(ctx).ErrorContext(ctx, "codex continuation remains waiting after non-execution", "phase", phase, "error", cause)
	return runtime.Outcome{Pending: &deferredTurn{driver: p.driver, record: *r}}, nil
}

// reparkElicitation parks the same call on the URL elicitation the server
// sent instead of running it, so the person gets the server's current link. A
// granted approval stays granted: accept runs the call without asking again.
func (p *deferredTurn) reparkElicitation(ctx context.Context, r *pendingRecord, elicitation *mcp.ElicitParams, cause error) (runtime.Outcome, error) {
	callID, _, arguments := r.call()
	*r = pendingRecord{Version: r.Version, TaskID: r.TaskID, ContextID: r.ContextID, Origin: r.Origin, ThreadID: r.ThreadID, Phase: phaseWaiting,
		Server: r.Server, Tool: r.Tool, CallID: callID, Arguments: arguments,
		Elicitation: &runtime.URLElicitationRequest{ID: uuid.NewString(), Server: r.Server, Message: elicitation.Message, URL: elicitation.URL, ElicitationID: elicitation.ElicitationID}}
	if err := p.driver.savePending(r); err != nil {
		return runtime.Outcome{}, errors.Join(cause, err)
	}
	logging.FromContext(ctx).InfoContext(ctx, "codex call waits on the MCP server's URL elicitation", "server", r.Server, "tool", r.Tool)
	return runtime.Outcome{Pending: &deferredTurn{driver: p.driver, record: *r}}, nil
}

// abandon prevents a failed task from being restored as pending input.
func (p *deferredTurn) abandon(cause error) error {
	r, err := p.current()
	if err != nil {
		return errors.Join(cause, err)
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled {
		return cause
	}
	r.Phase = phaseAbandoned
	if err := p.driver.savePending(r); err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(runtime.ErrSessionAbandoned, cause)
}

func (p *deferredTurn) Cancel(_ context.Context, _ runtime.ModelCallSink) error {
	r, err := p.current()
	if err != nil {
		return err
	}
	if r.Phase == phaseAbandoned {
		return runtime.ErrSessionAbandoned
	}
	r.Phase = phaseCanceled
	return p.driver.savePending(r)
}

var _ runtime.PendingTurn = (*deferredTurn)(nil)

func validateInputResponse(request runtime.InputRequest, response runtime.InputResponse) error {
	switch request := request.(type) {
	case *runtime.ApprovalRequest:
		decision, ok := response.(*runtime.ApprovalDecision)
		if !ok {
			return fmt.Errorf("Codex is waiting for a structured tool approval response")
		}
		if decision.ID != request.ID {
			return fmt.Errorf("tool approval response ID %q does not match pending ID %q", decision.ID, request.ID)
		}
	case *runtime.AskUserRequest:
		answer, ok := response.(*runtime.AskUserResponse)
		if !ok {
			return fmt.Errorf("Codex is waiting for a structured ask-user response")
		}
		if answer.ID != request.ID {
			return fmt.Errorf("ask-user response ID %q does not match pending ID %q", answer.ID, request.ID)
		}
		if len(answer.Answers) != len(request.Questions) {
			return fmt.Errorf("ask-user response must answer every pending question")
		}
	case *runtime.URLElicitationRequest:
		action, ok := response.(*runtime.URLElicitationResponse)
		if !ok {
			return fmt.Errorf("Codex is waiting for a URL elicitation response")
		}
		if action.ID != request.ID {
			return fmt.Errorf("URL elicitation response ID %q does not match pending ID %q", action.ID, request.ID)
		}
		switch action.Action {
		case apia2a.URLElicitationAccept, apia2a.URLElicitationDecline, apia2a.URLElicitationCancel:
		default:
			return fmt.Errorf("URL elicitation action %q is not accept, decline or cancel", action.Action)
		}
	default:
		return fmt.Errorf("Codex has an unsupported pending input request %T", request)
	}
	return nil
}
