package sandbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sandboxv1alpha1 "sigs.k8s.io/agent-sandbox/api/v1alpha1"
	extv1alpha1 "sigs.k8s.io/agent-sandbox/extensions/api/v1alpha1"
)

func newTestProvider() *AgentSandboxProvider {
	scheme := runtime.NewScheme()
	_ = extv1alpha1.AddToScheme(scheme)
	_ = sandboxv1alpha1.AddToScheme(scheme)

	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(
		&extv1alpha1.SandboxClaim{},
		&sandboxv1alpha1.Sandbox{},
	).Build()
	return NewAgentSandboxProvider(c)
}

func defaultOpts(sessionID string) CreateSandboxOptions {
	return CreateSandboxOptions{
		SessionID: sessionID,
		AgentName: "my-agent",
		Namespace: "default",
		WorkspaceRef: WorkspaceRef{
			APIGroup: "extensions.agents.x-k8s.io",
			Kind:     "SandboxTemplate",
			Name:     "python-dev",
		},
	}
}

// simulateReady runs in a goroutine and simulates the agent-sandbox controller
// making a SandboxClaim ready: creates a Sandbox with ServiceFQDN and sets
// the claim's Ready condition to True.
func simulateReady(ctx context.Context, t *testing.T, p *AgentSandboxProvider, sessionID string) {
	t.Helper()
	name := claimName(sessionID)
	key := types.NamespacedName{Name: name, Namespace: "default"}

	// Wait for the claim to exist.
	for {
		claim := &extv1alpha1.SandboxClaim{}
		if err := p.client.Get(ctx, key, claim); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Create the Sandbox with ServiceFQDN.
	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: sandboxv1alpha1.PodTemplate{},
		},
	}
	if err := p.client.Create(ctx, sb); err != nil {
		t.Errorf("simulateReady: failed to create sandbox: %v", err)
		return
	}
	sb.Status.ServiceFQDN = name + ".default.svc.cluster.local"
	if err := p.client.Status().Update(ctx, sb); err != nil {
		t.Errorf("simulateReady: failed to update sandbox status: %v", err)
		return
	}

	// Mark the claim as Ready.
	claim := &extv1alpha1.SandboxClaim{}
	if err := p.client.Get(ctx, key, claim); err != nil {
		t.Errorf("simulateReady: failed to get claim: %v", err)
		return
	}
	claim.Status.Conditions = []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.SandboxConditionReady),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "DependenciesReady",
		},
	}
	if err := p.client.Status().Update(ctx, claim); err != nil {
		t.Errorf("simulateReady: failed to update claim status: %v", err)
	}
}

// simulateFailure sets a terminal failure condition on a SandboxClaim.
func simulateFailure(ctx context.Context, t *testing.T, p *AgentSandboxProvider, sessionID, reason, message string) {
	t.Helper()
	name := claimName(sessionID)
	key := types.NamespacedName{Name: name, Namespace: "default"}

	// Wait for the claim to exist.
	for {
		claim := &extv1alpha1.SandboxClaim{}
		if err := p.client.Get(ctx, key, claim); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}

	claim := &extv1alpha1.SandboxClaim{}
	if err := p.client.Get(ctx, key, claim); err != nil {
		t.Errorf("simulateFailure: failed to get claim: %v", err)
		return
	}
	claim.Status.Conditions = []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.SandboxConditionReady),
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
	if err := p.client.Status().Update(ctx, claim); err != nil {
		t.Errorf("simulateFailure: failed to update claim status: %v", err)
	}
}

func TestAgentSandboxProvider(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, p *AgentSandboxProvider)
	}{
		{
			name: "blocks until sandbox is ready",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				go simulateReady(ctx, t, p, "sess-1")

				ep, err := p.GetOrCreate(ctx, defaultOpts("sess-1"))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !ep.Ready {
					t.Error("expected sandbox to be ready")
				}
				name := claimName("sess-1")
				expectedURL := fmt.Sprintf("http://%s.default.svc.cluster.local:%d/mcp", name, defaultMCPPort)
				if ep.MCPUrl != expectedURL {
					t.Errorf("expected URL %s, got %s", expectedURL, ep.MCPUrl)
				}
			},
		},
		{
			name: "returns error on terminal failure",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				go simulateFailure(ctx, t, p, "sess-fail", "TemplateNotFound", "template python-dev not found")

				_, err := p.GetOrCreate(ctx, defaultOpts("sess-fail"))
				if err == nil {
					t.Fatal("expected error for terminal failure")
				}
				t.Logf("got expected error: %v", err)
			},
		},
		{
			name: "times out when sandbox never becomes ready",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()

				// Don't simulate readiness — let it time out.
				_, err := p.GetOrCreate(ctx, defaultOpts("sess-timeout"))
				if err == nil {
					t.Fatal("expected timeout error")
				}
				t.Logf("got expected error: %v", err)
			},
		},
		{
			name: "idempotent create returns same endpoint",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				go simulateReady(ctx, t, p, "sess-2")

				ep1, err := p.GetOrCreate(ctx, defaultOpts("sess-2"))
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				// Second call should find the existing ready claim immediately.
				ep2, err := p.GetOrCreate(ctx, defaultOpts("sess-2"))
				if err != nil {
					t.Fatalf("unexpected error on second call: %v", err)
				}

				if ep1.ID != ep2.ID {
					t.Errorf("expected same ID, got %s and %s", ep1.ID, ep2.ID)
				}
			},
		},
		{
			name: "get returns nil when no claim exists",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ep, err := p.Get(context.Background(), "nonexistent")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ep != nil {
					t.Error("expected nil for nonexistent session")
				}
			},
		},
		{
			name: "get returns not-ready endpoint for pending claim",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx := context.Background()
				name := claimName("sess-pending")

				// Create the claim directly (bypassing GetOrCreate which would block).
				claim := p.buildClaim(name, "default", defaultOpts("sess-pending"))
				if err := p.client.Create(ctx, claim); err != nil {
					t.Fatalf("failed to create claim: %v", err)
				}

				ep, err := p.Get(ctx, "sess-pending")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ep == nil {
					t.Fatal("expected endpoint, got nil")
				}
				if ep.Ready {
					t.Error("expected sandbox to not be ready")
				}
			},
		},
		{
			name: "get returns ready endpoint",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx := context.Background()
				name := claimName("sess-ready")

				// Create claim directly.
				claim := p.buildClaim(name, "default", defaultOpts("sess-ready"))
				if err := p.client.Create(ctx, claim); err != nil {
					t.Fatalf("failed to create claim: %v", err)
				}

				// Simulate readiness synchronously.
				simulateReady(ctx, t, p, "sess-ready")

				ep, err := p.Get(ctx, "sess-ready")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ep == nil {
					t.Fatal("expected endpoint, got nil")
				}
				if !ep.Ready {
					t.Error("expected sandbox to be ready")
				}
				expectedURL := fmt.Sprintf("http://%s.default.svc.cluster.local:%d/mcp", name, defaultMCPPort)
				if ep.MCPUrl != expectedURL {
					t.Errorf("expected URL %s, got %s", expectedURL, ep.MCPUrl)
				}
			},
		},
		{
			name: "destroy deletes the claim",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				ctx := context.Background()
				name := claimName("sess-4")

				// Create claim directly.
				claim := p.buildClaim(name, "default", defaultOpts("sess-4"))
				if err := p.client.Create(ctx, claim); err != nil {
					t.Fatalf("failed to create claim: %v", err)
				}

				if err := p.Destroy(ctx, "sess-4"); err != nil {
					t.Fatalf("unexpected error on destroy: %v", err)
				}

				ep, err := p.Get(ctx, "sess-4")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ep != nil {
					t.Error("expected nil after destroy")
				}
			},
		},
		{
			name: "destroy nonexistent is no-op",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				err := p.Destroy(context.Background(), "nonexistent")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			},
		},
		{
			name: "requires namespace",
			fn: func(t *testing.T, p *AgentSandboxProvider) {
				opts := CreateSandboxOptions{
					SessionID: "sess-7",
					AgentName: "my-agent",
					WorkspaceRef: WorkspaceRef{
						APIGroup: "extensions.agents.x-k8s.io",
						Kind:     "SandboxTemplate",
						Name:     "python-dev",
					},
				}

				_, err := p.GetOrCreate(context.Background(), opts)
				if err == nil {
					t.Fatal("expected error for missing namespace")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProvider()
			tt.fn(t, p)
		})
	}
}
