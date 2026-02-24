package auth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authimpl "github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/pkg/auth"
)

// directProvider is a pass-through provider that marshals AuthzRequest directly
// (no wrapping) and expects AuthzDecision directly (no unwrapping).
// Used to test ExternalAuthorizer transport logic independently of OPA formatting.
type directProvider struct{}

func (p *directProvider) Name() string { return "direct" }

func (p *directProvider) MarshalRequest(req auth.AuthzRequest) ([]byte, error) {
	return json.Marshal(req)
}

func (p *directProvider) UnmarshalDecision(data []byte) (*auth.AuthzDecision, error) {
	var d auth.AuthzDecision
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func TestExternalAuthorizer(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		timeout     time.Duration
		cancelCtx   bool
		claims      map[string]any
		resource    auth.Resource
		action      auth.Verb
		wantAllowed bool
		wantReason  string
		wantErr     bool
	}{
		{
			name: "allowed response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{Allowed: true}) //nolint:errcheck
			},
			claims:      map[string]any{"sub": "user-1", "groups": []string{"admin"}},
			resource:    auth.Resource{Type: "Agent", Name: "default/my-agent"},
			action:      auth.VerbGet,
			wantAllowed: true,
		},
		{
			name: "denied with reason",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{ //nolint:errcheck
					Allowed: false,
					Reason:  "user not in admin group",
				})
			},
			claims:      map[string]any{"sub": "user-2"},
			resource:    auth.Resource{Type: "Agent", Name: "default/restricted-agent"},
			action:      auth.VerbDelete,
			wantAllowed: false,
			wantReason:  "user not in admin group",
		},
		{
			name: "non-200 status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			claims:   map[string]any{"sub": "user-3"},
			resource: auth.Resource{Type: "Agent", Name: "default/any-agent"},
			action:   auth.VerbCreate,
			wantErr:  true,
		},
		{
			name: "malformed JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("{invalid")) //nolint:errcheck
			},
			claims:   map[string]any{"sub": "user-4"},
			resource: auth.Resource{Type: "Agent", Name: "default/any-agent"},
			action:   auth.VerbGet,
			wantErr:  true,
		},
		{
			name: "timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{Allowed: true}) //nolint:errcheck
			},
			timeout:  50 * time.Millisecond,
			claims:   map[string]any{"sub": "user-5"},
			resource: auth.Resource{Type: "Agent", Name: "default/any-agent"},
			action:   auth.VerbGet,
			wantErr:  true,
		},
		{
			name: "nil claims",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{Allowed: true}) //nolint:errcheck
			},
			claims:      nil,
			resource:    auth.Resource{Type: "Session", Name: "session-1"},
			action:      auth.VerbGet,
			wantAllowed: true,
		},
		{
			name: "request body correctness",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "expected POST", http.StatusMethodNotAllowed)
					return
				}
				if r.Header.Get("Content-Type") != "application/json" {
					http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
					return
				}

				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read body failed", http.StatusInternalServerError)
					return
				}

				// directProvider sends AuthzRequest without wrapping
				var req auth.AuthzRequest
				if err := json.Unmarshal(body, &req); err != nil {
					http.Error(w, "invalid JSON", http.StatusBadRequest)
					return
				}

				if req.Resource.Type != "Agent" || req.Resource.Name != "default/test-agent" {
					http.Error(w, "unexpected resource", http.StatusBadRequest)
					return
				}
				if req.Action != auth.VerbUpdate {
					http.Error(w, "unexpected action", http.StatusBadRequest)
					return
				}
				if req.Claims["sub"] != "validator" {
					http.Error(w, "unexpected claims", http.StatusBadRequest)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{Allowed: true}) //nolint:errcheck
			},
			claims:      map[string]any{"sub": "validator"},
			resource:    auth.Resource{Type: "Agent", Name: "default/test-agent"},
			action:      auth.VerbUpdate,
			wantAllowed: true,
		},
		{
			name: "context cancellation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(5 * time.Second)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(auth.AuthzDecision{Allowed: true}) //nolint:errcheck
			},
			cancelCtx: true,
			claims:    map[string]any{"sub": "user-6"},
			resource:  auth.Resource{Type: "Agent", Name: "default/any-agent"},
			action:    auth.VerbGet,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			timeout := 5 * time.Second
			if tt.timeout > 0 {
				timeout = tt.timeout
			}

			authorizer := &authimpl.ExternalAuthorizer{
				Endpoint: server.URL,
				Provider: &directProvider{},
				Client:   &http.Client{Timeout: timeout},
			}

			ctx := context.Background()
			if tt.cancelCtx {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}

			decision, err := authorizer.Check(ctx, auth.AuthzRequest{
				Claims:   tt.claims,
				Resource: tt.resource,
				Action:   tt.action,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if decision != nil {
					t.Errorf("Check() returned non-nil decision on error: %+v", decision)
				}
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

// TestExternalAuthorizerWithOPAProvider tests the full integration with OPAProvider
// using an httptest server that speaks OPA's wire format.
func TestExternalAuthorizerWithOPAProvider(t *testing.T) {
	opaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusInternalServerError)
			return
		}

		// Verify OPA-formatted request: {"input": {...}}
		var envelope struct {
			Input auth.AuthzRequest `json:"input"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(w, "invalid OPA request format", http.StatusBadRequest)
			return
		}

		// Make a decision based on the input
		allowed := false
		reason := "denied by default"
		if envelope.Input.Claims != nil {
			if sub, ok := envelope.Input.Claims["sub"].(string); ok && sub == "admin" {
				allowed = true
				reason = ""
			}
		}

		// Respond in OPA format: {"result": {...}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"result": map[string]any{
				"allowed": allowed,
				"reason":  reason,
			},
		})
	}))
	defer opaServer.Close()

	authorizer := &authimpl.ExternalAuthorizer{
		Endpoint: opaServer.URL,
		Provider: &authimpl.OPAProvider{},
		Client:   &http.Client{Timeout: 5 * time.Second},
	}

	// Test allowed request
	decision, err := authorizer.Check(context.Background(), auth.AuthzRequest{
		Claims:   map[string]any{"sub": "admin"},
		Resource: auth.Resource{Type: "Agent", Name: "default/my-agent"},
		Action:   auth.VerbGet,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected allowed, got denied: %s", decision.Reason)
	}

	// Test denied request
	decision, err = authorizer.Check(context.Background(), auth.AuthzRequest{
		Claims:   map[string]any{"sub": "user-1"},
		Resource: auth.Resource{Type: "Agent", Name: "default/my-agent"},
		Action:   auth.VerbDelete,
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if decision.Allowed {
		t.Error("expected denied, got allowed")
	}
	if decision.Reason != "denied by default" {
		t.Errorf("Reason = %q, want %q", decision.Reason, "denied by default")
	}
}
