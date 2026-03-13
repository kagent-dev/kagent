package sandbox

import (
	"context"
	"fmt"
	"sync"
)

// StubProvider is a sandbox provider for testing that returns fake endpoints.
// It stores sandboxes in memory to support Get/Destroy operations.
type StubProvider struct {
	mu        sync.RWMutex
	sandboxes map[string]*SandboxEndpoint
}

var _ SandboxProvider = (*StubProvider)(nil)

func NewStubProvider() *StubProvider {
	return &StubProvider{
		sandboxes: make(map[string]*SandboxEndpoint),
	}
}

func (s *StubProvider) GetOrCreate(_ context.Context, opts CreateSandboxOptions) (*SandboxEndpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ep, ok := s.sandboxes[opts.SessionID]; ok {
		return ep, nil
	}

	ep := &SandboxEndpoint{
		ID:       fmt.Sprintf("stub-%s", opts.SessionID),
		MCPUrl:   "http://localhost:9999/mcp",
		Protocol: "streamable-http",
		Ready:    true,
	}
	s.sandboxes[opts.SessionID] = ep
	return ep, nil
}

func (s *StubProvider) Get(_ context.Context, sessionID string) (*SandboxEndpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sandboxes[sessionID], nil
}

func (s *StubProvider) Destroy(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sandboxes, sessionID)
	return nil
}
