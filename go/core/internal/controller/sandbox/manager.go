package sandbox

import (
	"context"
	"fmt"
	"sync"
)

// SandboxManager manages the lifecycle mapping from session IDs to sandboxes.
// It is goroutine-safe and delegates actual provisioning to a SandboxProvider.
type SandboxManager struct {
	provider  SandboxProvider
	sandboxes map[string]*SandboxEndpoint // keyed by session ID
	mu        sync.RWMutex
}

// NewSandboxManager creates a new SandboxManager with the given provider.
func NewSandboxManager(provider SandboxProvider) *SandboxManager {
	return &SandboxManager{
		provider:  provider,
		sandboxes: make(map[string]*SandboxEndpoint),
	}
}

// GetOrCreateSandbox returns an existing sandbox for the session or creates
// a new one. This method is idempotent for the same session ID.
func (m *SandboxManager) GetOrCreateSandbox(ctx context.Context, sessionID string, opts CreateSandboxOptions) (*SandboxEndpoint, error) {
	m.mu.RLock()
	if ep, ok := m.sandboxes[sessionID]; ok {
		m.mu.RUnlock()
		return ep, nil
	}
	m.mu.RUnlock()

	// Upgrade to write lock
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if ep, ok := m.sandboxes[sessionID]; ok {
		return ep, nil
	}

	opts.SessionID = sessionID
	ep, err := m.provider.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox for session %s: %w", sessionID, err)
	}

	m.sandboxes[sessionID] = ep
	return ep, nil
}

// GetSandbox returns the sandbox endpoint for a session, or nil if none exists.
func (m *SandboxManager) GetSandbox(sessionID string) *SandboxEndpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sandboxes[sessionID]
}

// DestroySandbox tears down the sandbox for a session and removes it from
// the manager's tracking map.
func (m *SandboxManager) DestroySandbox(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	ep, ok := m.sandboxes[sessionID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.sandboxes, sessionID)
	m.mu.Unlock()

	return m.provider.Destroy(ctx, ep.ID)
}
