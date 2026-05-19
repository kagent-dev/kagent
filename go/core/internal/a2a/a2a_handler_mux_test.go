package a2a

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type testSession struct {
	principal auth.Principal
}

func (s *testSession) Principal() auth.Principal {
	return s.principal
}

type testAuthProvider struct{}

func (p *testAuthProvider) Authenticate(context.Context, http.Header, url.Values) (auth.Session, error) {
	return nil, nil
}

func (p *testAuthProvider) UpstreamAuth(*http.Request, auth.Session, auth.Principal) error {
	return nil
}

type testA2AAccessProvider struct {
	testAuthProvider
	err      error
	calls    int
	sessions []auth.Session
	targets  []auth.A2ATarget
}

func (p *testA2AAccessProvider) CheckA2AAccess(_ context.Context, session auth.Session, target auth.A2ATarget) error {
	p.calls++
	p.sessions = append(p.sessions, session)
	p.targets = append(p.targets, target)
	return p.err
}

func TestHandlerMuxDispatchesWhenProviderDoesNotImplementA2AAccessProvider(t *testing.T) {
	m := newTestHandlerMux(&testAuthProvider{})
	called := false
	m.handlers[routeKey(false, "default", "agent-one")] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	rr := serveTestA2A(t, m, "/api/a2a/default/agent-one", nil)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: want %d, got %d", http.StatusNoContent, rr.Code)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestHandlerMuxA2AAccessProviderAllowsAndReceivesAgentTarget(t *testing.T) {
	provider := &testA2AAccessProvider{}
	m := newTestHandlerMux(provider)
	called := false
	m.handlers[routeKey(false, "default", "agent-one")] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})
	session := &testSession{principal: auth.Principal{User: auth.User{ID: "user@example.com"}}}

	rr := serveTestA2A(t, m, "/api/a2a/default/agent-one", session)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status: want %d, got %d", http.StatusAccepted, rr.Code)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
	if provider.calls != 1 {
		t.Fatalf("CheckA2AAccess calls: want 1, got %d", provider.calls)
	}
	assertTarget(t, provider.targets[0], auth.A2ATarget{
		Namespace:    "default",
		Name:         "agent-one",
		WorkloadType: auth.A2AWorkloadAgent,
	})
	if provider.sessions[0] != session {
		t.Fatalf("session: want %p, got %p", session, provider.sessions[0])
	}
}

func TestHandlerMuxA2AAccessProviderDeniesWithForbidden(t *testing.T) {
	provider := &testA2AAccessProvider{err: errors.New("denied")}
	m := newTestHandlerMux(provider)
	called := false
	m.handlers[routeKey(false, "default", "agent-one")] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	rr := serveTestA2A(t, m, "/api/a2a/default/agent-one", &testSession{})

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want %d, got %d", http.StatusForbidden, rr.Code)
	}
	if called {
		t.Fatal("handler should not be called after access denial")
	}
	if provider.calls != 1 {
		t.Fatalf("CheckA2AAccess calls: want 1, got %d", provider.calls)
	}
}

func TestHandlerMuxA2AAccessProviderReceivesSandboxTarget(t *testing.T) {
	provider := &testA2AAccessProvider{}
	m := newTestHandlerMux(provider)
	m.handlers[routeKey(true, "observability", "sandbox-one")] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := serveTestA2A(t, m, "/api/a2a-sandboxes/observability/sandbox-one", &testSession{})

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rr.Code)
	}
	if provider.calls != 1 {
		t.Fatalf("CheckA2AAccess calls: want 1, got %d", provider.calls)
	}
	assertTarget(t, provider.targets[0], auth.A2ATarget{
		Namespace:    "observability",
		Name:         "sandbox-one",
		WorkloadType: auth.A2AWorkloadSandbox,
	})
}

func TestHandlerMuxMissingAuthSessionPassesNilSession(t *testing.T) {
	provider := &testA2AAccessProvider{}
	m := newTestHandlerMux(provider)
	m.handlers[routeKey(false, "default", "agent-one")] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := serveTestA2A(t, m, "/api/a2a/default/agent-one", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want %d, got %d", http.StatusOK, rr.Code)
	}
	if provider.calls != 1 {
		t.Fatalf("CheckA2AAccess calls: want 1, got %d", provider.calls)
	}
	if provider.sessions[0] != nil {
		t.Fatalf("session: want nil, got %#v", provider.sessions[0])
	}
}

func TestHandlerMuxExistingRouteExtractionBehavior(t *testing.T) {
	provider := &testA2AAccessProvider{}
	m := newTestHandlerMux(provider)
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "non matching route remains router 404",
			path:       "/api/not-a2a/default/agent-one",
			wantStatus: http.StatusNotFound,
			wantCalls:  0,
		},
		{
			name:       "matching route missing handler checks access before mux 404",
			path:       "/api/a2a/default/missing-agent",
			wantStatus: http.StatusNotFound,
			wantCalls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider.calls = 0
			rr := serveTestA2A(t, m, tt.path, &testSession{})
			if rr.Code != tt.wantStatus {
				t.Fatalf("status: want %d, got %d", tt.wantStatus, rr.Code)
			}
			if provider.calls != tt.wantCalls {
				t.Fatalf("CheckA2AAccess calls: want %d, got %d", tt.wantCalls, provider.calls)
			}
		})
	}
}

func TestHandlerMuxA2AOnlyPathPredicate(t *testing.T) {
	m := newTestHandlerMux(&testAuthProvider{})
	tests := []struct {
		name        string
		escapedPath string
		want        bool
	}{
		{name: "agent route", escapedPath: "/api/a2a/default/agent-one", want: true},
		{name: "agent subroute", escapedPath: "/api/a2a/default/agent-one/tasks", want: true},
		{name: "sandbox route", escapedPath: "/api/a2a-sandboxes/default/sandbox-one", want: true},
		{name: "non a2a api", escapedPath: "/api/me", want: false},
		{name: "evil agent prefix", escapedPath: "/api/a2aevil/default/agent-one", want: false},
		{name: "evil sandbox prefix", escapedPath: "/api/a2a-sandboxesevil/default/sandbox-one", want: false},
		{name: "missing name segment", escapedPath: "/api/a2a/default", want: false},
		{name: "encoded slash", escapedPath: "/api/a2a/default%2Fagent-one/tasks", want: false},
		{name: "encoded backslash", escapedPath: "/api/a2a/default%5Cagent-one/tasks", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.isA2ARequestPath(tt.escapedPath); got != tt.want {
				t.Fatalf("isA2ARequestPath(%q) = %v, want %v", tt.escapedPath, got, tt.want)
			}
		})
	}
}

func TestHandlerMuxDeniesBeforeMissingHandlerLookup(t *testing.T) {
	provider := &testA2AAccessProvider{err: errors.New("denied")}
	m := newTestHandlerMux(provider)

	rr := serveTestA2A(t, m, "/api/a2a/default/missing-agent", &testSession{})

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: want %d, got %d", http.StatusForbidden, rr.Code)
	}
	if provider.calls != 1 {
		t.Fatalf("CheckA2AAccess calls: want 1, got %d", provider.calls)
	}
	assertTarget(t, provider.targets[0], auth.A2ATarget{
		Namespace:    "default",
		Name:         "missing-agent",
		WorkloadType: auth.A2AWorkloadAgent,
	})
}

func newTestHandlerMux(provider auth.AuthProvider) *handlerMux {
	return &handlerMux{
		handlers:          make(map[string]http.Handler),
		agentPathPrefix:   "/api/a2a",
		sandboxPathPrefix: "/api/a2a-sandboxes",
		authenticator:     provider,
	}
}

func serveTestA2A(t *testing.T, h http.Handler, path string, session auth.Session) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.PathPrefix("/api/a2a/{namespace}/{name}").Handler(h)
	router.PathPrefix("/api/a2a-sandboxes/{namespace}/{name}").Handler(h)

	req := httptest.NewRequest(http.MethodPost, path, nil)
	if session != nil {
		req = req.WithContext(auth.AuthSessionTo(req.Context(), session))
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func assertTarget(t *testing.T, got, want auth.A2ATarget) {
	t.Helper()
	if got != want {
		t.Fatalf("target: want %#v, got %#v", want, got)
	}
}
