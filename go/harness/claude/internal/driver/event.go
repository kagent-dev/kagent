package driver

type EventKind string

const (
	EventSessionStarted EventKind = "session_started"
	EventTextDelta      EventKind = "text_delta"
	EventToolActivity   EventKind = "tool_activity"
	EventCompleted      EventKind = "completed"
	EventFailed         EventKind = "failed"
)

// Event is the Claude stream vocabulary consumed by ProcessDriver. Vendor
// parsing details stay here and are normalized before reaching shared runtime
// code.
type Event struct {
	Kind        EventKind
	SessionID   string
	Text        string
	ToolID      string
	ToolName    string
	ToolPhase   string
	ToolResult  any
	ToolError   bool
	Metadata    map[string]any
	Category    string
	SafeMessage string
	Result      string
}
