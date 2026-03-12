package sandbox

import (
	"context"
	"fmt"
)

// StubProvider is a sandbox provider for testing that returns fake endpoints.
type StubProvider struct{}

var _ SandboxProvider = (*StubProvider)(nil)

func NewStubProvider() *StubProvider {
	return &StubProvider{}
}

func (s *StubProvider) Create(_ context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error) {
	return &SandboxEndpoint{
		ID:       fmt.Sprintf("stub-%s", opts.SessionID),
		MCPUrl:   "http://localhost:9999/mcp",
		Protocol: "streamable-http",
		Ready:    true,
	}, nil
}

func (s *StubProvider) Destroy(_ context.Context, _ string) error {
	return nil
}

func (s *StubProvider) Status(_ context.Context, sandboxID string) (*SandboxStatus, error) {
	return &SandboxStatus{
		Phase:   SandboxPhaseReady,
		Message: fmt.Sprintf("stub sandbox %s is ready", sandboxID),
	}, nil
}
