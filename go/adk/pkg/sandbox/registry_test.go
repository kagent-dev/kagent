package sandbox

import (
	"sync"
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
)

// fakeToolset is a minimal toolset for testing (avoids real MCP connections).
type fakeToolset struct {
	name string
}

func (f *fakeToolset) Name() string                                       { return f.name }
func (f *fakeToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) { return nil, nil }

func TestRegistryGetNotFound(t *testing.T) {
	r := NewSandboxRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent session")
	}
}

func TestRegistryRemove(t *testing.T) {
	r := NewSandboxRegistry()
	// Manually insert an entry for testing.
	r.mu.Lock()
	r.entries["sess-1"] = &sandboxEntry{
		mcpURL:  "http://test:8080/mcp",
		toolset: &fakeToolset{name: "sandbox"},
	}
	r.mu.Unlock()

	ts, ok := r.Get("sess-1")
	if !ok || ts == nil {
		t.Fatal("expected to find entry for sess-1")
	}

	r.Remove("sess-1")
	_, ok = r.Get("sess-1")
	if ok {
		t.Error("expected not found after Remove")
	}
}

func TestRegistryIdempotentInsert(t *testing.T) {
	r := NewSandboxRegistry()
	// Insert the same session twice.
	r.mu.Lock()
	r.entries["sess-1"] = &sandboxEntry{
		mcpURL:  "http://test:8080/mcp",
		toolset: &fakeToolset{name: "first"},
	}
	r.mu.Unlock()

	ts, ok := r.Get("sess-1")
	if !ok {
		t.Fatal("expected to find sess-1")
	}
	if ts.Name() != "first" {
		t.Errorf("expected name=first, got %s", ts.Name())
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewSandboxRegistry()

	// Pre-populate some entries.
	for i := range 100 {
		id := "sess-" + string(rune('A'+i%26))
		r.mu.Lock()
		r.entries[id] = &sandboxEntry{
			mcpURL:  "http://test:8080/mcp",
			toolset: &fakeToolset{name: "sandbox"},
		}
		r.mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "sess-" + string(rune('A'+i%26))
			r.Get(id)
		}(i)
	}

	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "sess-remove-" + string(rune('A'+i%26))
			r.Remove(id)
		}(i)
	}

	wg.Wait()
}
