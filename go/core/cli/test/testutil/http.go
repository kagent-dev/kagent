package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
)

// NewMockHTTPServer creates a test HTTP server for mocking API responses.
func NewMockHTTPServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}

// MockAgentResponse returns a mock AgentResponse handler.
func MockAgentResponse(agents []api.AgentResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agents)
	}
}

// MockSessionResponse returns a mock SessionResponse handler.
func MockSessionResponse(sessions []*api.Session) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	}
}

// MockVersionResponse returns a mock version response handler.
func MockVersionResponse(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version": version,
		})
	}
}

// MockErrorResponse returns a mock error response handler.
func MockErrorResponse(statusCode int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{
			"error": message,
		})
	}
}
