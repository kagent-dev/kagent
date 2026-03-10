package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_ServesHTML(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("embedded index.html is empty")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty")
	}
}
