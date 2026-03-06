package temporal

import "time"

// ExecutionRequest is the input to AgentExecutionWorkflow.
type ExecutionRequest struct {
	SessionID   string `json:"sessionID"`
	UserID      string `json:"userID"`
	AgentName   string `json:"agentName"`
	Message     []byte `json:"message"`     // serialized A2A message
	Config      []byte `json:"config"`      // serialized AgentConfig
	NATSSubject string `json:"natsSubject"` // e.g. "agent.myagent.sess123.stream"
}

// ExecutionResult is the output of AgentExecutionWorkflow.
type ExecutionResult struct {
	SessionID string `json:"sessionID"`
	Status    string `json:"status"` // "completed", "rejected", "failed"
	Response  []byte `json:"response,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// LLMRequest is the input to LLMInvokeActivity.
type LLMRequest struct {
	Config      []byte `json:"config"`      // serialized AgentConfig (model info)
	History     []byte `json:"history"`     // serialized conversation history
	NATSSubject string `json:"natsSubject"` // for token streaming
}

// LLMResponse is the output of LLMInvokeActivity.
type LLMResponse struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	// AgentCalls contains A2A agent invocations detected in tool calls.
	AgentCalls []AgentCall `json:"agentCalls,omitempty"`
	// NeedsApproval indicates HITL approval is required before continuing.
	NeedsApproval bool   `json:"needsApproval,omitempty"`
	ApprovalMsg   string `json:"approvalMsg,omitempty"`
	// Terminal indicates this is the final response (no more tool calls).
	Terminal bool `json:"terminal,omitempty"`
}

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args []byte `json:"args"` // JSON-encoded arguments
}

// AgentCall represents an A2A agent invocation (child workflow).
type AgentCall struct {
	TargetAgent string `json:"targetAgent"`
	Message     []byte `json:"message"`
}

// ToolRequest is the input to ToolExecuteActivity.
type ToolRequest struct {
	ToolName    string `json:"toolName"`
	ToolCallID  string `json:"toolCallID"`
	Args        []byte `json:"args"`
	NATSSubject string `json:"natsSubject"`
}

// ToolResponse is the output of ToolExecuteActivity.
type ToolResponse struct {
	ToolCallID string `json:"toolCallID"`
	Result     []byte `json:"result"`
	Error      string `json:"error,omitempty"`
}

// SessionRequest is the input to SessionActivity.
type SessionRequest struct {
	AppName   string `json:"appName"`
	UserID    string `json:"userID"`
	SessionID string `json:"sessionID"`
}

// SessionResponse is the output of SessionActivity.
type SessionResponse struct {
	SessionID string `json:"sessionID"`
	Created   bool   `json:"created"`
}

// TaskSaveRequest is the input to SaveTaskActivity.
type TaskSaveRequest struct {
	SessionID string `json:"sessionID"`
	TaskData  []byte `json:"taskData"`
}

// AppendEventRequest is the input to AppendEventActivity.
type AppendEventRequest struct {
	SessionID string `json:"sessionID"`
	Event     []byte `json:"event"`
}

// ApprovalDecision is the payload for HITL approval signals.
type ApprovalDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// WorkerConfig holds configuration for a Temporal worker.
type WorkerConfig struct {
	TemporalAddr string `json:"temporalAddr"` // e.g. "temporal-server:7233"
	Namespace    string `json:"namespace"`     // Temporal namespace
	TaskQueue    string `json:"taskQueue"`     // per-agent: "agent-{agentName}"
	NATSAddr     string `json:"natsAddr"`      // e.g. "nats://nats:4222"
}

// ClientConfig holds configuration for a Temporal client.
type ClientConfig struct {
	TemporalAddr string `json:"temporalAddr"`
	Namespace    string `json:"namespace"`
}

// TemporalConfig is the runtime configuration for Temporal, derived from
// the Agent CRD spec and passed to the agent pod via config.json.
type TemporalConfig struct {
	Enabled         bool          `json:"enabled"`
	HostAddr        string        `json:"hostAddr"`
	Namespace       string        `json:"namespace"`
	TaskQueue       string        `json:"taskQueue"`       // "agent-{agentName}"
	NATSAddr        string        `json:"natsAddr"`
	WorkflowTimeout time.Duration `json:"workflowTimeout"` // default 48h
	LLMMaxAttempts  int           `json:"llmMaxAttempts"`  // default 5
	ToolMaxAttempts int           `json:"toolMaxAttempts"` // default 3
}

// DefaultTemporalConfig returns a TemporalConfig with default values.
func DefaultTemporalConfig() TemporalConfig {
	return TemporalConfig{
		Namespace:       "default",
		WorkflowTimeout: 48 * time.Hour,
		LLMMaxAttempts:  5,
		ToolMaxAttempts: 3,
	}
}

// TaskQueueForAgent returns the Temporal task queue name for an agent.
func TaskQueueForAgent(agentName string) string {
	return "agent-" + agentName
}

// ApprovalSignalName is the Temporal signal channel name for HITL approvals.
const ApprovalSignalName = "approval"

// WorkflowIDForSession returns a deterministic workflow ID for a session.
func WorkflowIDForSession(agentName, sessionID string) string {
	return "agent-" + agentName + "-" + sessionID
}

// ChildWorkflowID returns the workflow ID for a child workflow.
func ChildWorkflowID(parentSessionID, targetAgentName string) string {
	return parentSessionID + "-child-" + targetAgentName
}
