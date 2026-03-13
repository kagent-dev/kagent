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
//
// The provider is the source of truth for sandbox state. Implementations
// must handle idempotency — calling GetOrCreate multiple times with the same
// sessionID should return the same sandbox.
type SandboxProvider interface {
	// GetOrCreate provisions a new sandbox or returns the existing one for a
	// session. Implementations must be idempotent for the same session ID.
	// The call blocks until the sandbox is ready or the context is cancelled.
	// Terminal failures (e.g. template not found) return an error immediately.
	GetOrCreate(ctx context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error)

	// Get returns the current sandbox endpoint for a session, or nil if none exists.
	Get(ctx context.Context, sessionID string) (*SandboxEndpoint, error)

	// Destroy tears down the sandbox for the given session ID.
	Destroy(ctx context.Context, sessionID string) error
}
