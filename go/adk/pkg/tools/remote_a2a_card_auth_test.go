package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

// cardServer imitates kagent-controller: the agent card at
// GET /agents/kagent/peer/.well-known/agent-card.json (the tool's base URL — a2a-go fetches a
// base URL that has a path verbatim) only for Authorization "Bearer <want>", recording the
// Authorization header of every card request; JSON-RPC at POST /agents/kagent/peer, recording
// each request's tenant and rejecting a tenant other than the Agent's, as the controller does.
const peerCardPath = "/agents/kagent/peer/.well-known/agent-card.json"

func cardServer(t *testing.T, want string) (*httptest.Server, func() []string) {
	srv, seen, _ := peerServer(t, want)
	return srv, seen
}

func peerServer(t *testing.T, want string) (*httptest.Server, func() []string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen, tenants []string
	srv := httptest.NewServer(nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/agents/kagent/peer" {
			var rpc struct {
				ID     any `json:"id"`
				Params struct {
					Tenant string `json:"tenant"`
				} `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&rpc)
			mu.Lock()
			tenants = append(tenants, rpc.Params.Tenant)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if rpc.Params.Tenant != "" && rpc.Params.Tenant != "kagent/peer" {
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID,
					"error": map[string]any{"code": -32600, "message": "tenant does not match the Agent URL"}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID,
				"result": map[string]any{"message": map[string]any{"messageId": "m1", "role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "hi"}}}}})
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != peerCardPath {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		card := a2atype.AgentCard{
			Name:                "peer",
			Description:         "a peer agent",
			Version:             "1.0.0",
			SupportedInterfaces: []*a2atype.AgentInterface{a2atype.NewAgentInterface(srv.URL+"/agents/kagent/peer", a2atype.TransportProtocolJSONRPC)},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})
	t.Cleanup(srv.Close)
	get := func(v *[]string) func() []string {
		return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), *v...) }
	}
	return srv, get(&seen), get(&tenants)
}

func TestEnsureClientAgentCardAuthorization(t *testing.T) {
	inbound := func() context.Context {
		return withCallContext(context.Background(), map[string][]string{"authorization": {"Bearer caller-token"}})
	}

	t.Run("propagated caller token is sent on the agent card request", func(t *testing.T) {
		srv, seen := cardServer(t, "caller-token")
		s := &remoteA2AState{name: "peer", baseURL: srv.URL + peerCardPath, httpClient: srv.Client(), propagateToken: true}
		if _, err := s.ensureClient(inbound()); err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if got := seen(); len(got) != 1 || got[0] != "Bearer caller-token" {
			t.Fatalf("card request Authorization = %q, want [Bearer caller-token]", got)
		}
	})

	t.Run("without propagation the card request carries no token", func(t *testing.T) {
		srv, seen := cardServer(t, "caller-token")
		s := &remoteA2AState{name: "peer", baseURL: srv.URL + peerCardPath, httpClient: srv.Client()}
		_, err := s.ensureClient(inbound())
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("ensureClient error = %v, want a 401 from the card request", err)
		}
		if got := seen(); len(got) != 1 || got[0] != "" {
			t.Fatalf("card request Authorization = %q, want none", got)
		}
	})

	t.Run("a static Authorization header wins over the propagated token", func(t *testing.T) {
		srv, seen := cardServer(t, "static-token")
		s := &remoteA2AState{
			name: "peer", baseURL: srv.URL + peerCardPath, httpClient: srv.Client(), propagateToken: true,
			extraHeaders: map[string]string{"Authorization": "Bearer static-token"},
		}
		if _, err := s.ensureClient(inbound()); err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if got := seen(); len(got) != 1 || got[0] != "Bearer static-token" {
			t.Fatalf("card request Authorization = %q, want [Bearer static-token]", got)
		}
	})
}

func TestRemoteA2ADoesNotForwardTheCallersTenant(t *testing.T) {
	srv, _, tenants := peerServer(t, "caller-token")
	s := &remoteA2AState{name: "peer", baseURL: srv.URL + peerCardPath, httpClient: srv.Client(), propagateToken: true}
	// The request this agent is serving belongs to ANOTHER tenant (its own Agent).
	ctx := a2atype.AttachTenant(withCallContext(context.Background(), map[string][]string{"authorization": {"Bearer caller-token"}}), "kagent/caller")
	client, err := s.ensureClient(ctx)
	if err != nil {
		t.Fatalf("ensureClient: %v", err)
	}
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hello"))
	if _, err := client.SendMessage(ctx, &a2atype.SendMessageRequest{Message: msg}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got := tenants(); len(got) != 1 || got[0] != "" {
		t.Fatalf("request tenant = %q, want none (the caller's tenant must not be forwarded)", got)
	}
}
