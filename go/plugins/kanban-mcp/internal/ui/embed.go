// Package ui serves the embedded single-page Kanban board. The SPA is a single
// vanilla HTML+JS+CSS file (no build step) compiled into the binary via go:embed,
// so the server ships as one self-contained artifact. The page fetches the board
// from the REST API and subscribes to /events (SSE) for live updates.
package ui

import (
	"bytes"
	_ "embed"
	"net/http"
)

// readonlySentinel is the default read-only declaration in index.html. Handler
// rewrites it to "true" when the server runs in read-only mode so the SPA hides
// the "New Task" button. Keeping the default in the source file means the
// embedded page is valid as-is (read-only off) without any substitution.
const (
	readonlySentinel = "const READONLY = false;"
	readonlyEnabled  = "const READONLY = true;"
)

// indexHTML is the full single-page application, embedded at build time. A build
// failure here (e.g. a missing index.html) is caught at compile time.
//
//go:embed index.html
var indexHTML []byte

// taskProgressHTML is the self-contained MCP App View served as the
// ui://kanban/task-progress resource. It is a single vanilla HTML+JS+CSS file
// (no external assets) so it can be embedded directly in the MCP resource body.
//
//go:embed task_progress.html
var taskProgressHTML []byte

// TaskProgressHTML returns the MCP App View HTML for the task-progress widget.
// The mcp package serves these bytes as the ui://kanban/task-progress resource.
func TaskProgressHTML() []byte {
	return taskProgressHTML
}

// Handler returns an http.Handler that serves the embedded SPA as text/html for
// every request. It is mounted at "/" by the server; client-side routing (if any)
// and the REST/SSE surfaces live under their own prefixes. When readonly is true
// the served page hides the "New Task" button.
func Handler(readonly bool) http.Handler {
	page := indexHTML
	if readonly {
		page = bytes.Replace(indexHTML, []byte(readonlySentinel), []byte(readonlyEnabled), 1)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
}
