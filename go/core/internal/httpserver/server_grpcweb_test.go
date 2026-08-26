package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The rule about *which* requests are gRPC-Web, and about the /api prefix, belongs
// to the gRPC server — `grpcserver.Server.WebHandlerOr` — because more than one
// binary serves HTTP beside that server and a second copy of the rule would drift.
// What is this server's business, and all these tests pin, is that a supplied
// router is applied around its own handler, and that leaving it unset changes
// nothing.

func TestWithGrpcWebAppliesTheSuppliedRouter(t *testing.T) {
	routerCalls := 0
	innerCalls := 0

	// Stands in for WebHandlerOr: records that it was given the router as `next`,
	// and answers without delegating, the way a real gRPC-Web call would.
	router := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			routerCalls++
			if next == nil {
				t.Error("the router was not given the server's own handler as next")
			}
			w.WriteHeader(http.StatusOK)
		})
	}

	s := &HTTPServer{config: ServerConfig{GrpcWebRouter: router}}
	handler := s.withGrpcWeb(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { innerCalls++ }))

	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/kagent.api.v1alpha1.AgentService/ListAgents", nil),
	)

	if routerCalls != 1 {
		t.Errorf("router calls = %d, want 1", routerCalls)
	}
	// The router decides whether the inner handler runs. This one does not delegate,
	// so the router genuinely sits in front rather than beside.
	if innerCalls != 0 {
		t.Errorf("inner handler calls = %d, want 0 — the router should be outermost", innerCalls)
	}
}

// A router that delegates must reach the server's own handler, or ordinary API
// traffic would never be served.
func TestWithGrpcWebRouterCanDelegate(t *testing.T) {
	innerCalls := 0
	passThrough := func(next http.Handler) http.Handler { return next }

	s := &HTTPServer{config: ServerConfig{GrpcWebRouter: passThrough}}
	s.withGrpcWeb(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { innerCalls++ })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/a2a/default/x", nil))

	if innerCalls != 1 {
		t.Errorf("inner handler calls = %d, want 1", innerCalls)
	}
}

// A server built without a router must behave exactly as it did before one existed.
func TestWithGrpcWebIsInertWhenUnset(t *testing.T) {
	called := 0
	s := &HTTPServer{config: ServerConfig{}}

	s.withGrpcWeb(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called++ })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/anything", nil))

	if called != 1 {
		t.Fatalf("handler calls = %d, want 1", called)
	}
}
