package sandbox

import "context"

// SandboxPhase represents the lifecycle phase of a sandbox.
type SandboxPhase string

const (
	SandboxPhasePending SandboxPhase = "Pending"
	SandboxPhaseReady   SandboxPhase = "Ready"
	SandboxPhaseFailed  SandboxPhase = "Failed"
)

// CreateSandboxOptions contains parameters for creating a sandbox.
type CreateSandboxOptions struct {
	SessionID    string
	AgentName    string
	Namespace    string
	WorkspaceRef WorkspaceRef
}

// WorkspaceRef is the workspace reference from the Agent CRD.
type WorkspaceRef struct {
	APIGroup  string
	Kind      string
	Name      string
	Namespace string
}

// SandboxEndpoint describes a running sandbox and how to reach it.
type SandboxEndpoint struct {
	ID       string            `json:"sandbox_id"`
	MCPUrl   string            `json:"mcp_url"`
	Protocol string            `json:"protocol"`
	Headers  map[string]string `json:"headers,omitempty"`
	Ready    bool              `json:"ready"`
}

// SandboxStatus reports the current state of a sandbox.
type SandboxStatus struct {
	Phase   SandboxPhase `json:"phase"`
	Message string       `json:"message,omitempty"`
}

// SandboxProvider is the controller-internal interface that sandbox backends
// must implement. Each provider maps workspace references to concrete
// sandbox environments (e.g. agent-sandbox pods, Moat processes).
type SandboxProvider interface {
	// Create provisions a new sandbox. Implementations should be idempotent
	// for the same session ID.
	Create(ctx context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error)

	// Destroy tears down the sandbox for the given sandbox ID.
	Destroy(ctx context.Context, sandboxID string) error

	// Status returns the current status of a sandbox.
	Status(ctx context.Context, sandboxID string) (*SandboxStatus, error)
}
