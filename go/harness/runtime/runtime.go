// Package runtime defines the minimal turn and event vocabulary shared by
// Harness runtime adapters.
package runtime

import "context"

// Turn is one invocation of an Actor's root conversation.
type Turn struct {
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
}

// ToolResult reports the result paired with one ToolCall ID.
type ToolResult struct {
	ID      string
	Name    string
	Result  any
	IsError bool
}

// PendingTurn owns the live native process or session behind an input request.
// The executor retains this handle while the A2A task is waiting for input and
// must call either Resume or Cancel. Resume continues the same native turn and
// may return a new PendingTurn when the runtime asks for more input.
type PendingTurn interface {
	Request() InputRequest
	Resume(context.Context, InputResponse, EventSink) (Outcome, error)
	Cancel(context.Context) error
}

// Outcome describes why a runtime operation stopped producing events. Failure
// ends the turn, Pending transfers its live resources to the caller, and an
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

// AskUserRequest asks one or more questions while retaining the native turn.
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
