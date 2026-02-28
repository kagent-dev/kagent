package auth_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// mockAuthorizer is a test double that returns configurable decisions and errors.
type mockAuthorizer struct {
	decision *auth.AuthzDecision
	err      error
}

func (m *mockAuthorizer) Check(_ context.Context, _ auth.AuthzRequest) (*auth.AuthzDecision, error) {
	return m.decision, m.err
}

var _ auth.Authorizer = (*mockAuthorizer)(nil)

func TestAuthorizerCheck(t *testing.T) {
	tests := []struct {
		name        string
		authorizer  auth.Authorizer
		req         auth.AuthzRequest
		wantAllowed bool
		wantReason  string
		wantErr     bool
	}{
		{
			name: "allow with claims",
			authorizer: &mockAuthorizer{
				decision: &auth.AuthzDecision{Allowed: true},
			},
			req: auth.AuthzRequest{
				Claims:   map[string]any{"sub": "user-1", "groups": []string{"admin"}},
				Resource: auth.Resource{Type: "agent", Name: "my-agent"},
				Action:   auth.VerbGet,
			},
			wantAllowed: true,
			wantErr:     false,
		},
		{
			name: "deny with reason",
			authorizer: &mockAuthorizer{
				decision: &auth.AuthzDecision{Allowed: false, Reason: "insufficient permissions"},
			},
			req: auth.AuthzRequest{
				Claims:   map[string]any{"sub": "user-2"},
				Resource: auth.Resource{Type: "agent", Name: "restricted-agent"},
				Action:   auth.VerbDelete,
			},
			wantAllowed: false,
			wantReason:  "insufficient permissions",
			wantErr:     false,
		},
		{
			name: "system error",
			authorizer: &mockAuthorizer{
				err: fmt.Errorf("policy engine unreachable"),
			},
			req: auth.AuthzRequest{
				Claims:   map[string]any{"sub": "user-3"},
				Resource: auth.Resource{Type: "agent", Name: "any-agent"},
				Action:   auth.VerbCreate,
			},
			wantErr: true,
		},
		{
			name: "nil claims",
			authorizer: &mockAuthorizer{
				decision: &auth.AuthzDecision{Allowed: true},
			},
			req: auth.AuthzRequest{
				Claims:   nil,
				Resource: auth.Resource{Type: "session", Name: "session-1"},
				Action:   auth.VerbGet,
			},
			wantAllowed: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := tt.authorizer.Check(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if decision == nil {
				t.Fatal("Check() returned nil decision without error")
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Check() Allowed = %v, want %v", decision.Allowed, tt.wantAllowed)
			}
			if decision.Reason != tt.wantReason {
				t.Errorf("Check() Reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}
