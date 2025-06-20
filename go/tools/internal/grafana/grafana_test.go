package grafana

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewGrafanaClient(t *testing.T) {
	client := newGrafanaClient("http://localhost:3000", "admin", "admin", "")

	if client == nil {
		t.Fatal("Expected non-nil Grafana client")
	}

	if client.Timeout != 30*time.Second {
		t.Errorf("Expected 30s timeout, got %v", client.Timeout)
	}
}

func TestMakeGrafanaRequest(t *testing.T) {
	// Create a mock Grafana server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Return different responses based on endpoint
		switch r.URL.Path {
		case "/api/dashboards/home":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"meta":{"isHome":true},"dashboard":{"title":"Home"}}`))
		case "/api/datasources":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"id":1,"name":"prometheus","type":"prometheus"}]`))
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Test successful request
	respBody, err := makeGrafanaRequest("GET", mockServer.URL, "/api/dashboards/home", "admin", "admin", "", nil)
	if err != nil {
		t.Fatalf("makeGrafanaRequest failed: %v", err)
	}

	if len(respBody) == 0 {
		t.Fatal("Expected non-empty response body")
	}

	// Test request with API key
	respBody, err = makeGrafanaRequest("GET", mockServer.URL, "/api/datasources", "", "", "test-api-key", nil)
	if err != nil {
		t.Fatalf("makeGrafanaRequest with API key failed: %v", err)
	}

	if len(respBody) == 0 {
		t.Fatal("Expected non-empty response body")
	}
}

func TestGrafanaURLConstruction(t *testing.T) {
	testCases := []struct {
		baseURL  string
		endpoint string
		expected string
	}{
		{
			baseURL:  "http://localhost:3000",
			endpoint: "/api/dashboards",
			expected: "http://localhost:3000/api/dashboards",
		},
		{
			baseURL:  "http://localhost:3000/",
			endpoint: "/api/dashboards",
			expected: "http://localhost:3000/api/dashboards",
		},
		{
			baseURL:  "http://localhost:3000",
			endpoint: "api/dashboards",
			expected: "http://localhost:3000/api/dashboards",
		},
		{
			baseURL:  "http://localhost:3000/",
			endpoint: "api/dashboards",
			expected: "http://localhost:3000/api/dashboards",
		},
	}

	for i, tc := range testCases {
		// Simulate URL construction logic from makeGrafanaRequest
		baseURL := tc.baseURL
		endpoint := tc.endpoint

		// Remove trailing slash from baseURL
		if strings.HasSuffix(baseURL, "/") {
			baseURL = baseURL[:len(baseURL)-1]
		}

		// Remove leading slash from endpoint
		if strings.HasPrefix(endpoint, "/") {
			endpoint = endpoint[1:]
		}

		fullURL := baseURL + "/" + endpoint

		if fullURL != tc.expected {
			t.Errorf("Test case %d: expected '%s', got '%s'", i, tc.expected, fullURL)
		}
	}
}

func TestGrafanaHTTPMethods(t *testing.T) {
	// Create a mock server that checks HTTP methods
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method":"GET"}`))
		case "POST":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"method":"POST"}`))
		case "PUT":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method":"PUT"}`))
		case "DELETE":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"method":"DELETE"}`))
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer mockServer.Close()

	methods := []string{"GET", "POST", "PUT", "DELETE"}

	for _, method := range methods {
		respBody, err := makeGrafanaRequest(method, mockServer.URL, "/test", "admin", "admin", "", nil)
		if err != nil {
			t.Fatalf("makeGrafanaRequest with %s failed: %v", method, err)
		}

		if len(respBody) == 0 {
			t.Fatalf("Expected non-empty response body for %s", method)
		}
	}
}
