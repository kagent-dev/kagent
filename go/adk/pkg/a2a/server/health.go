package server

import (
	"net/http"
)

func defaultHealthPaths() []string { return []string{"/health", "/healthz"} }

var defaultHealthHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
})

// RegisterHealthEndpoints registers the default health check endpoints on the given mux.
// These endpoints are used by Kubernetes for readiness/liveness probes.
func RegisterHealthEndpoints(mux *http.ServeMux) {
	registerHealthEndpoints(mux, defaultHealthPaths(), defaultHealthHandler)
}

func registerHealthEndpoints(mux *http.ServeMux, paths []string, handler http.Handler) {
	for _, path := range paths {
		mux.Handle(path, handler)
	}
}
