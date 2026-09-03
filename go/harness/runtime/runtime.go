// Package runtime defines the minimal turn and event vocabulary shared by
// Harness runtime adapters.
package runtime

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

// Outcome is the terminal result of one runtime turn. A nil Failure is success.
type Outcome struct {
	Failure       *Failure
	InputRequired InputRequest
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
	Header   string
	Question string
	Options  []AskUserOption
	IsOther  bool
	IsSecret bool
}

type AskUserOption struct {
	Label       string
	Description string
}

// Failure contains only runtime-vetted information safe to expose publicly.
type Failure struct {
	Message string
}
