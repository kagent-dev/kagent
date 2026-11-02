// Package runtime defines the minimal turn and event vocabulary shared by
// Harness runtime adapters.
package runtime

import (
	"context"
	"errors"
	"time"
)

// ErrSessionAbandoned means a deferred continuation cannot safely be resumed.
var ErrSessionAbandoned = errors.New("deferred continuation cannot be resumed; start a new session")

// CancellationError reports that a runtime could not reach a safe cancellation
// boundary. The executor must not acknowledge cancellation as successful.
type CancellationError struct{ Err error }

func (e *CancellationError) Error() string {
	return "runtime cancellation cleanup failed: " + e.Err.Error()
}
func (e *CancellationError) Unwrap() error { return e.Err }

// Turn is one invocation of an Actor's root conversation.
type Turn struct {
	TaskID         string
	ContextID      string
	Origin         TraceOrigin
	Prompt         string
	ContinuationID string
	InputResponse  InputResponse
}

// InputResponse is one structured response to a parked native turn.
type InputResponse interface {
	isInputResponse()
}

type ApprovalDecision struct {
	ID              string
	Approved        bool
	RejectionReason string
}

func (*ApprovalDecision) isInputResponse() {}

// AskUserResponse contains one answer per question, in request order.
type AskUserResponse struct {
	ID      string
	Answers [][]string
}

func (*AskUserResponse) isInputResponse() {}

// EventSink receives ordered incremental runtime activity. Terminal state is
// returned as an Outcome from the runner rather than mixed into this stream.
type EventSink interface {
	SessionStarted(SessionStarted) error
	TextDelta(TextDelta) error
	ToolCall(ToolCall) error
	ToolResult(ToolResult) error
	ModelCallSink
}

// ModelCallSink records model usage, including usage reported while a turn is canceled.
type ModelCallSink interface {
	ModelCall(ModelCall)
}

// SessionStarted reports the stable private continuation selected by a runtime.
type SessionStarted struct {
	ContinuationID string
}

// TextDelta is one ordered fragment of assistant text.
type TextDelta struct {
	Text string
}

// ToolCall reports a runtime tool invocation after its arguments are complete.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	// MCPServer and MCPTool are set for an MCP tool, such as kagent and k8s_get.
	// Name keeps the harness's full name, such as mcp__kagent__k8s_get for Claude.
	MCPServer, MCPTool string
	// ParentToolID is the ID of the tool call that started this call's subagent, such as an Agent call.
	ParentToolID string
}

// ToolResult reports the result paired with one ToolCall ID.
type ToolResult struct {
	ID      string
	Name    string
	Result  any
	IsError bool
	Status  ToolStatus
}

// ToolStatus says whether a tool call ran. The zero value means it ran and returned a result.
type ToolStatus string

const (
	ToolRan ToolStatus = ""
	// ToolDeclined means the runtime refused the call, so it never ran.
	ToolDeclined ToolStatus = "declined"
	// ToolUnfinished means the result never arrived, so Result is a placeholder.
	// Producers also set IsError, which the A2A UI reads.
	ToolUnfinished ToolStatus = "unfinished"
)

// ToolExecutionSink observes deferred execution without publishing another A2A tool artifact.
// Drivers bracket deferred execution attempts; a proven non-execution suppresses the span.
// Supplying a cached result is not another execution attempt.
type ToolExecutionSink interface {
	ToolExecutionStarted(ToolCall)
	ToolExecutionFinished(ToolResult)
}

// ModelCall reports one model request. InputTokens includes cached tokens. UsagePartial
// means the harness never reported final usage, so OutputTokens is a lower bound.
type ModelCall struct {
	Provider              string
	RequestModel          string
	ResponseModel         string
	ResponseID            string
	RequestID             string
	InputTokens           int64
	CacheReadTokens       int64
	CacheWriteTokens      int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	StopReason            string
	// ErrorType is the harness's own error category for a failed call, such as
	// Claude Code's rate_limit. Empty means the harness gave none.
	ErrorType string
	// Retries is how many times the harness retried the call after a transient error.
	Retries int
	// ParentToolID is the ID of the tool call that started this call's subagent, such as an Agent call.
	ParentToolID string
	Start, End   time.Time
	UsagePartial bool
	// Canceled marks a partial call that cancellation cut short; it is not a failure.
	Canceled bool
	// UsageOnly marks leftover tokens of a turn, such as the missing output of partial calls, and not
	// one model request. It has no times or finish reason, and only the token counters record it.
	UsageOnly bool
}

// PendingTurn identifies the durable continuation behind an input request.
// The executor retains this handle while the A2A task is waiting for input and
// must call either Resume or Cancel. Resume continues the task and may return a
// new PendingTurn when the runtime asks for more input.
type PendingTurn interface {
	Request() InputRequest
	Resume(context.Context, InputResponse, EventSink) (Outcome, error)
	// Cancel reaps the native process and emits every ModelCall the harness
	// reported before it returns, even after ctx is done.
	Cancel(context.Context, ModelCallSink) error
}

// Outcome describes why a runtime operation stopped producing events. Failure
// ends the turn, Pending returns a durable continuation awaiting input, and an
// empty Outcome is successful completion. Failure and Pending are exclusive.
type Outcome struct {
	Failure *Failure
	Pending PendingTurn
}

// InputRequest is one structured request that parks a native turn.
type InputRequest interface {
	isInputRequest()
}

type ApprovalRequest struct {
	ID     string
	CallID string
	Name   string
	Args   map[string]any
	Hint   string
}

func (*ApprovalRequest) isInputRequest() {}

// AskUserRequest asks one or more questions before the task can continue.
type AskUserRequest struct {
	ID        string
	Questions []AskUserQuestion
	Hint      string
}

func (*AskUserRequest) isInputRequest() {}

type AskUserQuestion struct {
	ID       string
	Question string
	Choices  []string
	Multiple bool
}

// Failure contains only runtime-vetted information safe to expose publicly.
type Failure struct {
	Message string
}
