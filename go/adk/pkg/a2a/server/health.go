package server

import (
	"net/http"
)

func defaultHealthPaths() []string { return []string{"/health", "/healthz"} }

var defaultHealthHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
})

func registerHealthEndpoints(mux *http.ServeMux, paths []string, handler http.Handler) {
	for _, path := range paths {
		mux.Handle(path, handler)
	}
}
