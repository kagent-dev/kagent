package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUI_Embedded(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("indexHTML is empty — embed directive likely failed")
	}
	if !strings.Contains(string(indexHTML), "Temporal Workflows") {
		t.Error("indexHTML does not contain 'Temporal Workflows'")
	}
}

func TestUI_Handler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	Handler(Config{WebUIURL: "http://temporal:8080", Namespace: "test"}).ServeHTTP(w, req)

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
	if !strings.Contains(body, "Temporal Workflows") {
		t.Errorf("expected body to contain 'Temporal Workflows'")
	}
}
