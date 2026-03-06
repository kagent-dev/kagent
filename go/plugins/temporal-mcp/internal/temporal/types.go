package temporal

import "time"

// WorkflowFilter specifies criteria for listing workflows.
type WorkflowFilter struct {
	Status    string // "running", "completed", "failed", "" (all)
	AgentName string // parsed from workflow ID pattern "agent-{name}-{session}"
	PageSize  int
	NextToken []byte
}

// WorkflowSummary is a lightweight representation of a workflow execution.
type WorkflowSummary struct {
	WorkflowID string     `json:"WorkflowID"`
	RunID      string     `json:"RunID"`
	AgentName  string     `json:"AgentName"`
	SessionID  string     `json:"SessionID"`
	Status     string     `json:"Status"`
	StartTime  time.Time  `json:"StartTime"`
	CloseTime  *time.Time `json:"CloseTime,omitempty"`
	TaskQueue  string     `json:"TaskQueue"`
}

// WorkflowDetail includes the full activity history for a workflow.
type WorkflowDetail struct {
	WorkflowSummary
	Activities []ActivityInfo `json:"Activities"`
}

// ActivityInfo describes a single activity execution within a workflow.
type ActivityInfo struct {
	Name      string    `json:"Name"`
	Status    string    `json:"Status"`
	StartTime time.Time `json:"StartTime"`
	Duration  string    `json:"Duration"`
	Attempt   int       `json:"Attempt"`
	Error     string    `json:"Error,omitempty"`
}
