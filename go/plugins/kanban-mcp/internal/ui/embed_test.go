package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUI_Embedded verifies the index.html is embedded and non-empty at init time.
// A failed or missing embed surfaces here rather than as a blank page in a browser.
func TestUI_Embedded(t *testing.T) {
	if len(indexHTML) == 0 {
		t.Fatal("indexHTML is empty: go:embed of index.html failed")
	}
	if !strings.Contains(string(indexHTML), "Kanban") {
		t.Fatal("embedded UI does not contain expected marker \"Kanban\"")
	}
}

// TestTaskProgressHTML_Embedded verifies the MCP App View is embedded and looks
// like a valid MCP App (contains the handshake the host expects).
func TestTaskProgressHTML_Embedded(t *testing.T) {
	html := string(TaskProgressHTML())
	if html == "" {
		t.Fatal("TaskProgressHTML is empty: go:embed of task_progress.html failed")
	}
	for _, marker := range []string{"ui/initialize", "ui/notifications/tool-result", "refresh_task_progress"} {
		if !strings.Contains(html, marker) {
			t.Errorf("embedded View missing expected marker %q", marker)
		}
	}
}

// TestUI_Handler verifies GET / returns 200 with an HTML content type and a body
// containing the "Kanban" marker. It uses httptest.NewRecorder so no network
// listener is required.
func TestUI_Handler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	Handler(false).ServeHTTP(rec, req)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html prefix", ct)
	}
	if !strings.Contains(rec.Body.String(), "Kanban") {
		t.Fatal("response body does not contain expected marker \"Kanban\"")
	}
}

// TestUI_Handler_Readonly verifies the served page reflects the read-only flag:
// the default page declares READONLY false, and Handler(true) rewrites it so the
// SPA hides the "New Task" button.
func TestUI_Handler_Readonly(t *testing.T) {
	if !strings.Contains(string(indexHTML), readonlySentinel) {
		t.Fatalf("index.html missing read-only sentinel %q", readonlySentinel)
	}

	tests := []struct {
		name     string
		readonly bool
		want     string
	}{
		{name: "default is read-write", readonly: false, want: readonlySentinel},
		{name: "readonly rewrites flag", readonly: true, want: readonlyEnabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			Handler(tt.readonly).ServeHTTP(rec, req)

			body := rec.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body does not contain %q", tt.want)
			}
			if tt.readonly && strings.Contains(body, readonlySentinel) {
				t.Fatal("readonly page still contains read-write sentinel")
			}
		})
	}
}
