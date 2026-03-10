package ui

import (
	"bytes"
	_ "embed"
	"html"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

// Config holds UI-specific configuration injected into the HTML.
type Config struct {
	WebUIURL  string // URL of the official Temporal Web UI (empty = disabled)
	Namespace string // Temporal namespace
}

// Handler returns an http.Handler that serves the embedded SPA with injected config.
func Handler(cfg Config) http.Handler {
	// Inject server-side config as a global JS variable before </head>
	script := []byte(`<script>window.__TEMPORAL_CONFIG__={` +
		`"webuiURL":"` + html.EscapeString(cfg.WebUIURL) + `",` +
		`"namespace":"` + html.EscapeString(cfg.Namespace) + `"` +
		`};</script></head>`)

	rendered := bytes.Replace(indexHTML, []byte("</head>"), script, 1)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(rendered) //nolint:errcheck
	})
}
