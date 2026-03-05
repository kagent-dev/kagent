package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUI_Embedded verifies that indexHTML is non-empty at init time (catches missing embed file).
func TestUI_Embedded(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("indexHTML is empty — embed directive likely failed")
	}
	if !strings.Contains(string(indexHTML), "Kanban") {
		t.Error("indexHTML does not contain 'Kanban'")
	}
}

// TestUI_Handler verifies that GET / returns 200 with text/html content-type and non-empty body.
func TestUI_Handler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	Handler().ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty body")
	}
	if !strings.Contains(body, "Kanban") {
		t.Errorf("expected body to contain 'Kanban', got: %q", body[:min(200, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
