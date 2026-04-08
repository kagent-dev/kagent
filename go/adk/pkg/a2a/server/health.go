package server

import (
	"net/http"
)

// RegisterHealthEndpoints registers health check endpoints on the given mux.
// These endpoints are used by Kubernetes for readiness/liveness probes.
func RegisterHealthEndpoints(mux *http.ServeMux) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.Handle("/health", handler)
	mux.Handle("/healthz", handler)
}
