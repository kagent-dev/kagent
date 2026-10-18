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

// cardServer serves an agent card at /agents/kagent/peer — where a2a-go's resolver fetches it
// for a base URL with a path, as kagent-controller serves it — only to requests carrying
// Authorization "Bearer <want>", and records the Authorization header of every card request.
func cardServer(t *testing.T, want string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/agents/kagent/peer" {
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
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

func TestEnsureClientAgentCardAuthorization(t *testing.T) {
	inbound := func() context.Context {
		return withCallContext(context.Background(), map[string][]string{"authorization": {"Bearer caller-token"}})
	}

	t.Run("propagated caller token is sent on the agent card request", func(t *testing.T) {
		srv, seen := cardServer(t, "caller-token")
		s := &remoteA2AState{name: "peer", baseURL: srv.URL + "/agents/kagent/peer", httpClient: srv.Client(), propagateToken: true}
		if _, err := s.ensureClient(inbound()); err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if got := seen(); len(got) != 1 || got[0] != "Bearer caller-token" {
			t.Fatalf("card request Authorization = %q, want [Bearer caller-token]", got)
		}
	})

	t.Run("without propagation the card request carries no token", func(t *testing.T) {
		srv, seen := cardServer(t, "caller-token")
		s := &remoteA2AState{name: "peer", baseURL: srv.URL + "/agents/kagent/peer", httpClient: srv.Client()}
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
			name: "peer", baseURL: srv.URL + "/agents/kagent/peer", httpClient: srv.Client(), propagateToken: true,
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
