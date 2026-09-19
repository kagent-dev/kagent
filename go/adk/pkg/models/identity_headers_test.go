// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// headersSeenBy sends one request under ctx through a BuildHTTPClient client
// and returns the headers the server received.
func headersSeenBy(t *testing.T, ctx context.Context, tc TransportConfig) http.Header {
	t.Helper()
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client, err := BuildHTTPClient(tc)
	if err != nil {
		t.Fatalf("BuildHTTPClient: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	return got
}

// unsetenv removes name for the test and restores it afterwards; t.Setenv can
// only set a value, and an empty value is not the same as an absent variable.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// Every model call names the agent the runtime executes and the authenticated
// user of the turn, whatever the model's own default headers say.
func TestBuildHTTPClient_IdentifiesTheAgentAndTheUser(t *testing.T) {
	t.Setenv(env.KagentAgentTemplate.Name(), "assistant")
	t.Setenv(env.KagentNamespace.Name(), "team-a")
	ctx := WithUser(t.Context(), "alice@example.com")
	got := headersSeenBy(t, ctx, TransportConfig{Headers: map[string]string{
		"X-Custom":      "1",
		adk.AgentHeader: "impostor", // a model configuration cannot name another agent
	}})
	want := map[string]string{
		"X-Custom":               "1",
		adk.AgentHeader:          "assistant",
		adk.AgentNamespaceHeader: "team-a",
		adk.UserHeader:           "alice@example.com",
	}
	for name, value := range want {
		if got.Get(name) != value {
			t.Errorf("header %s = %q, want %q", name, got.Get(name), value)
		}
	}
}

// The user of the turn and the caller's bearer token live in the same context;
// the user's slot must never read the token (a pointer to a zero-size struct
// shared an address with BearerTokenKey once, and the raw id token of the turn
// went out as x-kagent-user).
func TestWithUserDoesNotShareTheBearerTokenSlot(t *testing.T) {
	ctx := WithUser(t.Context(), "alice@example.com")
	ctx = context.WithValue(ctx, BearerTokenKey, "eyJ.the-callers-token")
	if got := UserFromContext(ctx); got != "alice@example.com" {
		t.Fatalf("UserFromContext = %q, want the user, not the bearer", got)
	}
	if token, ok := PassthroughToken(ctx, true); !ok || token != "eyJ.the-callers-token" {
		t.Fatalf("PassthroughToken = %q, %v; want the bearer", token, ok)
	}
	if got := UserFromContext(context.WithValue(t.Context(), BearerTokenKey, "eyJ.only-a-token")); got != "" {
		t.Fatalf("UserFromContext with only a bearer = %q, want none", got)
	}
}

// A header the runtime cannot fill is absent, never a stand-in: no identity
// without the controller's variables (both of them), no user outside an
// authenticated turn.
func TestBuildHTTPClient_SendsNoIdentityItDoesNotHave(t *testing.T) {
	identity := []string{adk.AgentHeader, adk.AgentNamespaceHeader}
	tests := []struct {
		name          string
		agent, ns     string
		user          string
		wantAgent     bool
		wantUserValue string
	}{
		{name: "runtime without a controller", wantAgent: false},
		{name: "template without a namespace", agent: "assistant", wantAgent: false},
		{name: "namespace without a template", ns: "team-a", wantAgent: false},
		{name: "turn without an authenticated user", agent: "assistant", ns: "team-a", wantAgent: true},
		{name: "authenticated turn", agent: "assistant", ns: "team-a", user: "alice@example.com", wantAgent: true, wantUserValue: "alice@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range map[string]string{env.KagentAgentTemplate.Name(): tt.agent, env.KagentNamespace.Name(): tt.ns} {
				if value == "" {
					unsetenv(t, name)
				} else {
					t.Setenv(name, value)
				}
			}
			ctx := t.Context()
			if tt.user != "" {
				ctx = WithUser(ctx, tt.user)
			}
			got := headersSeenBy(t, ctx, TransportConfig{})
			for _, name := range identity {
				if _, present := got[http.CanonicalHeaderKey(name)]; present != tt.wantAgent {
					t.Errorf("header %s present = %v, want %v (value %q)", name, present, tt.wantAgent, got.Get(name))
				}
			}
			if got.Get(adk.UserHeader) != tt.wantUserValue {
				t.Errorf("header %s = %q, want %q", adk.UserHeader, got.Get(adk.UserHeader), tt.wantUserValue)
			}
		})
	}
}
