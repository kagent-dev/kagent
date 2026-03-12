package sandbox

import (
	"context"
	"sync"
	"testing"
)

func TestSandboxManager(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m *SandboxManager)
	}{
		{
			name: "create and retrieve sandbox",
			fn: func(t *testing.T, m *SandboxManager) {
				ctx := context.Background()
				opts := CreateSandboxOptions{
					AgentName: "test-agent",
					Namespace: "default",
					WorkspaceRef: WorkspaceRef{
						APIGroup: "sandbox.kagent.dev",
						Kind:     "SandboxTemplate",
						Name:     "my-template",
					},
				}

				ep, err := m.GetOrCreateSandbox(ctx, "session-1", opts)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ep.ID != "stub-session-1" {
					t.Errorf("expected ID stub-session-1, got %s", ep.ID)
				}
				if !ep.Ready {
					t.Error("expected sandbox to be ready")
				}

				// Verify GetSandbox returns the same endpoint
				got := m.GetSandbox("session-1")
				if got == nil {
					t.Fatal("expected sandbox to exist")
				}
				if got.ID != ep.ID {
					t.Errorf("expected same sandbox ID, got %s", got.ID)
				}
			},
		},
		{
			name: "idempotent create returns same sandbox",
			fn: func(t *testing.T, m *SandboxManager) {
				ctx := context.Background()
				opts := CreateSandboxOptions{
					AgentName: "test-agent",
					Namespace: "default",
				}

				ep1, err := m.GetOrCreateSandbox(ctx, "session-2", opts)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				ep2, err := m.GetOrCreateSandbox(ctx, "session-2", opts)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if ep1.ID != ep2.ID {
					t.Errorf("expected same sandbox ID on second call, got %s and %s", ep1.ID, ep2.ID)
				}
			},
		},
		{
			name: "destroy removes sandbox",
			fn: func(t *testing.T, m *SandboxManager) {
				ctx := context.Background()
				opts := CreateSandboxOptions{
					AgentName: "test-agent",
					Namespace: "default",
				}

				_, err := m.GetOrCreateSandbox(ctx, "session-3", opts)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				err = m.DestroySandbox(ctx, "session-3")
				if err != nil {
					t.Fatalf("unexpected error on destroy: %v", err)
				}

				got := m.GetSandbox("session-3")
				if got != nil {
					t.Error("expected sandbox to be removed after destroy")
				}
			},
		},
		{
			name: "destroy nonexistent session is no-op",
			fn: func(t *testing.T, m *SandboxManager) {
				err := m.DestroySandbox(context.Background(), "nonexistent")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			},
		},
		{
			name: "get nonexistent returns nil",
			fn: func(t *testing.T, m *SandboxManager) {
				got := m.GetSandbox("nonexistent")
				if got != nil {
					t.Error("expected nil for nonexistent sandbox")
				}
			},
		},
		{
			name: "concurrent access",
			fn: func(t *testing.T, m *SandboxManager) {
				ctx := context.Background()
				var wg sync.WaitGroup

				for i := 0; i < 50; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						opts := CreateSandboxOptions{
							AgentName: "test-agent",
							Namespace: "default",
						}
						_, err := m.GetOrCreateSandbox(ctx, "concurrent-session", opts)
						if err != nil {
							t.Errorf("unexpected error: %v", err)
						}
					}()
				}

				wg.Wait()

				got := m.GetSandbox("concurrent-session")
				if got == nil {
					t.Fatal("expected sandbox to exist after concurrent creates")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewStubProvider()
			manager := NewSandboxManager(provider)
			tt.fn(t, manager)
		})
	}
}
