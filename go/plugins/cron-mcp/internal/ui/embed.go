package ui

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

// Handler returns an http.Handler that serves the embedded SPA.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML) //nolint:errcheck
	})
}
