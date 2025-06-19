package prometheus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrometheusQueryHTTP(t *testing.T) {
	// Create a mock Prometheus server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		query := r.URL.Query().Get("query")
		if query == "" {
			http.Error(w, "Missing query parameter", http.StatusBadRequest)
			return
		}

		// Return mock Prometheus response
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"__name__": "test_metric"},
						"value":  []interface{}{1234567890, "1.23"},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Test successful HTTP request
	resp, err := http.Get(mockServer.URL + "/api/v1/query?query=test_metric")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	status, ok := result["status"].(string)
	if !ok || status != "success" {
		t.Fatalf("Expected status 'success', got %v", result["status"])
	}
}

func TestPrometheusRangeQueryHTTP(t *testing.T) {
	// Create a mock Prometheus server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		query := r.URL.Query().Get("query")
		if query == "" {
			http.Error(w, "Missing query parameter", http.StatusBadRequest)
			return
		}

		// Return mock Prometheus range response
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"__name__": "test_metric"},
						"values": [][]interface{}{
							{1234567890, "1.23"},
							{1234567900, "1.45"},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Test successful HTTP request
	resp, err := http.Get(mockServer.URL + "/api/v1/query_range?query=test_metric&start=1234567800&end=1234567900&step=15s")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	status, ok := result["status"].(string)
	if !ok || status != "success" {
		t.Fatalf("Expected status 'success', got %v", result["status"])
	}
}

func TestPrometheusTargetsHTTP(t *testing.T) {
	// Create a mock Prometheus server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/targets" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		// Return mock targets response
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"activeTargets": []map[string]interface{}{
					{
						"discoveredLabels": map[string]string{
							"__address__":      "localhost:9090",
							"__metrics_path__": "/metrics",
							"__scheme__":       "http",
							"job":              "prometheus",
						},
						"labels": map[string]string{
							"instance": "localhost:9090",
							"job":      "prometheus",
						},
						"scrapePool":         "prometheus",
						"scrapeUrl":          "http://localhost:9090/metrics",
						"globalUrl":          "http://localhost:9090/metrics",
						"lastError":          "",
						"lastScrape":         "2023-01-01T00:00:00Z",
						"lastScrapeDuration": 0.001,
						"health":             "up",
					},
				},
				"droppedTargets": []interface{}{},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Test successful HTTP request
	resp, err := http.Get(mockServer.URL + "/api/v1/targets")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	status, ok := result["status"].(string)
	if !ok || status != "success" {
		t.Fatalf("Expected status 'success', got %v", result["status"])
	}
}

func TestPrometheusLabelsHTTP(t *testing.T) {
	// Create a mock Prometheus server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/labels" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		// Return mock labels response
		response := map[string]interface{}{
			"status": "success",
			"data": []string{
				"__name__",
				"instance",
				"job",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	// Test successful HTTP request
	resp, err := http.Get(mockServer.URL + "/api/v1/labels")
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	status, ok := result["status"].(string)
	if !ok || status != "success" {
		t.Fatalf("Expected status 'success', got %v", result["status"])
	}
}
