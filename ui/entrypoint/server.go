package main

import (
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// newHandler serves the static bundle from root, and the rendered index.html and env-config.js from generated.
func newHandler(root, generated, basePath string) http.Handler {
	// ServeMux cleans every path but a CONNECT target's, which stripBasePath refuses.
	file := func(r *http.Request) string { return root + path.Clean("/"+r.URL.Path) }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "OK") })
	mux.HandleFunc("/env-config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript")
		serveFile(w, r, generated+"/env-config.js")
	})
	// The bundle entrypoint is never cached, or a client keeps loading the previous release's assets.
	index := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		serveFile(w, r, generated+"/index.html")
	}
	mux.HandleFunc("/index.html", index)
	// The image ships no mock service worker; the SPA fallback would answer it with a misleading 200.
	mux.Handle("/mockServiceWorker.js", http.NotFoundHandler())
	// Vite fingerprints these filenames, so a given URL's bytes never change.
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		serveFile(w, r, file(r))
	})
	// SPA fallback: a deep link such as /agents is a client-side route, not a file.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if fi, err := os.Stat(file(r)); err == nil && fi.Mode().IsRegular() {
			serveFile(w, r, file(r))
			return
		}
		index(w, r)
	})
	return stripBasePath(mux, basePath)
}

// stripBasePath drops ui.basePath when the proxy in front forwards it unstripped, and
// puts it back on redirects. It refuses CONNECT, whose path ServeMux leaves uncleaned.
func stripBasePath(next http.Handler, basePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if rest, ok := strings.CutPrefix(r.URL.Path, basePath); ok && basePath != "" && (rest == "" || rest[0] == '/') {
			r.URL.Path, r.URL.RawPath = "/"+strings.TrimPrefix(rest, "/"), ""
			w = &baseWriter{ResponseWriter: w, basePath: basePath}
		}
		next.ServeHTTP(w, r)
	})
}

type baseWriter struct {
	http.ResponseWriter
	basePath string
}

func (w *baseWriter) WriteHeader(code int) {
	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "/") {
		w.Header().Set("Location", w.basePath+loc)
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *baseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// serveFile serves a regular file, unlike http.ServeFile, which redirects /index.html.
func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := os.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, fi.ModTime(), f)
}
