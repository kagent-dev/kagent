package auth_test

import (
	"encoding/json"
	"testing"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

func TestOPAProviderName(t *testing.T) {
	p := &authimpl.OPAProvider{}
	if got := p.Name(); got != "opa" {
		t.Errorf("Name() = %q, want %q", got, "opa")
	}
}

func TestOPAProviderMarshalRequest(t *testing.T) {
	tests := []struct {
		name       string
		req        auth.AuthzRequest
		wantClaims bool
	}{
		{
			name: "full request",
			req: auth.AuthzRequest{
				Claims:   map[string]any{"sub": "user-1", "groups": []string{"admin"}},
				Resource: auth.Resource{Type: "Agent", Name: "default/my-agent"},
				Action:   auth.VerbGet,
			},
			wantClaims: true,
		},
		{
			name: "nil claims",
			req: auth.AuthzRequest{
				Claims:   nil,
				Resource: auth.Resource{Type: "Session", Name: "session-1"},
				Action:   auth.VerbCreate,
			},
			wantClaims: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &authimpl.OPAProvider{}
			data, err := p.MarshalRequest(tt.req)
			if err != nil {
				t.Fatalf("MarshalRequest() error = %v", err)
			}

			// Verify the JSON has {"input": {...}} wrapper
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}

			inputRaw, ok := envelope["input"]
			if !ok {
				t.Fatal("MarshalRequest() JSON missing 'input' key")
			}

			// Verify the inner request round-trips
			var inner auth.AuthzRequest
			if err := json.Unmarshal(inputRaw, &inner); err != nil {
				t.Fatalf("unmarshal inner request: %v", err)
			}

			if inner.Resource.Type != tt.req.Resource.Type {
				t.Errorf("Resource.Type = %q, want %q", inner.Resource.Type, tt.req.Resource.Type)
			}
			if inner.Resource.Name != tt.req.Resource.Name {
				t.Errorf("Resource.Name = %q, want %q", inner.Resource.Name, tt.req.Resource.Name)
			}
			if inner.Action != tt.req.Action {
				t.Errorf("Action = %q, want %q", inner.Action, tt.req.Action)
			}
			if tt.wantClaims && inner.Claims == nil {
				t.Error("expected non-nil Claims")
			}
			if !tt.wantClaims && inner.Claims != nil {
				t.Errorf("expected nil Claims, got %v", inner.Claims)
			}
		})
	}
}

func TestOPAProviderUnmarshalDecision(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantAllowed bool
		wantReason  string
		wantErr     bool
	}{
		{
			name:        "allowed",
			input:       `{"result": {"allowed": true}}`,
			wantAllowed: true,
		},
		{
			name:        "denied with reason",
			input:       `{"result": {"allowed": false, "reason": "not in admin group"}}`,
			wantAllowed: false,
			wantReason:  "not in admin group",
		},
		{
			name:        "empty result defaults to denied",
			input:       `{"result": {}}`,
			wantAllowed: false,
		},
		{
			name:    "malformed JSON",
			input:   `{invalid`,
			wantErr: true,
		},
		{
			name:        "no result key defaults to zero value",
			input:       `{}`,
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &authimpl.OPAProvider{}
			decision, err := p.UnmarshalDecision([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalDecision() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if decision == nil {
				t.Fatal("UnmarshalDecision() returned nil without error")
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v", decision.Allowed, tt.wantAllowed)
			}
			if decision.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}

func TestProviderByName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantErr  bool
	}{
		{name: "opa", input: "opa", wantName: "opa"},
		{name: "empty defaults to opa", input: "", wantName: "opa"},
		{name: "unknown provider", input: "foobar", wantErr: true},
		{name: "cerbos not yet supported", input: "cerbos", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := authimpl.ProviderByName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ProviderByName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				if p != nil {
					t.Errorf("ProviderByName(%q) returned non-nil provider on error", tt.input)
				}
				return
			}
			if p.Name() != tt.wantName {
				t.Errorf("ProviderByName(%q).Name() = %q, want %q", tt.input, p.Name(), tt.wantName)
			}
		})
	}
}
