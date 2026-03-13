package sandbox

import (
	"testing"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
)

func TestSandboxToolsetName(t *testing.T) {
	ts := NewSandboxToolset(NewSandboxRegistry())
	if ts.Name() != "sandbox" {
		t.Errorf("expected name=sandbox, got %s", ts.Name())
	}
}

// mockReadonlyContext implements agent.ReadonlyContext for testing.
type mockReadonlyContext struct {
	agent.ReadonlyContext
	sessionID string
}

func (m *mockReadonlyContext) SessionID() string { return m.sessionID }

func TestSandboxToolsetNoSessionReturnsEmpty(t *testing.T) {
	ts := NewSandboxToolset(NewSandboxRegistry())
	tools, err := ts.Tools(&mockReadonlyContext{sessionID: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(tools))
	}
}

func TestSandboxToolsetNoRegistryEntryReturnsEmpty(t *testing.T) {
	ts := NewSandboxToolset(NewSandboxRegistry())
	tools, err := ts.Tools(&mockReadonlyContext{sessionID: "sess-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty tools, got %d", len(tools))
	}
}

func TestSandboxToolsetDelegatesToRegistry(t *testing.T) {
	registry := NewSandboxRegistry()
	// Inject a fake toolset.
	registry.mu.Lock()
	registry.entries["sess-1"] = &sandboxEntry{
		mcpURL:  "http://test:8080/mcp",
		toolset: &fakeToolsetWithTools{},
	}
	registry.mu.Unlock()

	ts := NewSandboxToolset(registry)
	tools, err := ts.Tools(&mockReadonlyContext{sessionID: "sess-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

// fakeToolsetWithTools returns one fake tool.
type fakeToolsetWithTools struct{}

func (f *fakeToolsetWithTools) Name() string { return "sandbox" }
func (f *fakeToolsetWithTools) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	return []tool.Tool{&fakeTool{}}, nil
}

type fakeTool struct{ tool.Tool }

func (f *fakeTool) Name() string { return "exec" }
