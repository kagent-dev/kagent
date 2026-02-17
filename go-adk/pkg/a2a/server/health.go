package server

import (
	"net/http"
)

// RegisterHealthEndpoints registers health check endpoints on the given mux.
// These endpoints are used by Kubernetes for readiness/liveness probes.
func RegisterHealthEndpoints(mux *http.ServeMux) {
	// Health endpoint for Kubernetes readiness probe
	// Returns 200 OK when the service is ready to accept traffic
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Healthz endpoint (alternative common path for Kubernetes)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}
