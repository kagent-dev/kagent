package sandbox

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// sandboxEntry holds the MCP toolset for a provisioned sandbox.
type sandboxEntry struct {
	mcpURL  string
	toolset tool.Toolset
}

// SandboxRegistry maps session IDs to their sandbox MCP toolsets.
// It is safe for concurrent use.
type SandboxRegistry struct {
	mu      sync.RWMutex
	entries map[string]*sandboxEntry
}

// NewSandboxRegistry creates an empty registry.
func NewSandboxRegistry() *SandboxRegistry {
	return &SandboxRegistry{
		entries: make(map[string]*sandboxEntry),
	}
}

// GetOrCreate returns the existing toolset for a session or creates a new one
// by connecting to the given MCP URL. Creation is idempotent — if two
// goroutines race, the first one wins.
func (r *SandboxRegistry) GetOrCreate(ctx context.Context, sessionID, mcpURL string) (tool.Toolset, error) {
	log := logr.FromContextOrDiscard(ctx)

	// Fast path: read lock.
	r.mu.RLock()
	if e, ok := r.entries[sessionID]; ok {
		r.mu.RUnlock()
		return e.toolset, nil
	}
	r.mu.RUnlock()

	// Slow path: create under write lock.
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if e, ok := r.entries[sessionID]; ok {
		return e.toolset, nil
	}

	log.Info("Creating sandbox MCP toolset", "sessionID", sessionID, "mcpURL", mcpURL)

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: mcpURL,
	}

	ts, err := mcptoolset.New(mcptoolset.Config{
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox MCP toolset for %s: %w", mcpURL, err)
	}

	r.entries[sessionID] = &sandboxEntry{
		mcpURL:  mcpURL,
		toolset: ts,
	}
	return ts, nil
}

// Get returns the toolset for a session, or nil if none exists.
func (r *SandboxRegistry) Get(sessionID string) (tool.Toolset, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[sessionID]
	if !ok {
		return nil, false
	}
	return e.toolset, true
}

// Remove deletes the entry for a session.
func (r *SandboxRegistry) Remove(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, sessionID)
}
