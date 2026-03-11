package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexHTMLEmbedded(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("indexHTML is empty")
	}
	if !strings.Contains(string(indexHTML), "<!DOCTYPE html>") {
		t.Fatal("indexHTML does not look like HTML")
	}
}

func TestHandlerServesHTML(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %s", ct)
	}
	if !strings.Contains(w.Body.String(), "Git Repos") {
		t.Error("response does not contain expected title")
	}
}
