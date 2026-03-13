package sandbox

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
)

// SandboxToolset implements tool.Toolset by delegating to the per-session
// sandbox MCP toolset stored in the SandboxRegistry. Since the Google ADK
// calls Tools() per-invocation with the session context, we can look up the
// correct session's sandbox tools dynamically.
//
// If no sandbox has been provisioned for the current session (e.g. beforeExecute
// hasn't run yet), Tools() returns an empty slice.
type SandboxToolset struct {
	registry *SandboxRegistry
}

// NewSandboxToolset creates a toolset backed by the given registry.
func NewSandboxToolset(registry *SandboxRegistry) *SandboxToolset {
	return &SandboxToolset{registry: registry}
}

var _ tool.Toolset = (*SandboxToolset)(nil)

// Name returns the toolset name.
func (s *SandboxToolset) Name() string {
	return "sandbox"
}

// Tools returns the sandbox MCP tools for the current session. If no sandbox
// is provisioned yet, returns an empty slice (not an error).
func (s *SandboxToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	sessionID := ctx.SessionID()
	if sessionID == "" {
		return nil, nil
	}

	inner, ok := s.registry.Get(sessionID)
	if !ok {
		return nil, nil
	}

	return inner.Tools(ctx)
}
