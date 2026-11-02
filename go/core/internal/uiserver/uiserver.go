// Package uiserver serves the web UI bundle on its own listener and hands the API
// paths to the controller's handler in-process.
package uiserver

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Keep in sync with the ui.basePath check in helm/kagent/templates/ui-configmap.yaml.
var (
	basePathRe     = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*$`)
	dotSegmentRe   = regexp.MustCompile(`(^|/)\.\.?(/|$)`)
	reservedPathRe = regexp.MustCompile(`^/(api|a2a|assets|health|env-config\.js|index\.html|mockServiceWorker\.js)(/|$)`)
	digitsRe       = regexp.MustCompile(`^[0-9]+$`)
)

// apiPaths reach the controller's handler; everything else is the bundle.
var apiPaths = []string{"/api/", "/.well-known/jwks.json"}

// maxBodyBytes sits just above the gateway's 16 MiB gRPC cap, which chat attachments hit first.
const maxBodyBytes = 20 << 20

// identityHeaders are set by an auth proxy in front of the UI, and the API may trust them.
var identityHeaders = []string{
	"x-auth-request-user",
	"x-auth-request-email",
	"x-auth-request-groups",
	"x-auth-request-preferred-username",
	"x-auth-request-access-token",
	"x-auth-request-redirect",
	"x-forwarded-user",
	"x-forwarded-email",
	"x-forwarded-groups",
	"x-forwarded-preferred-username",
}

type Config struct {
	// BindAddress is where the UI listens, such as ":8080".
	BindAddress string
	// Root holds the built bundle. Without an index.html there, the listener stays off.
	Root string
	// Environ supplies the KAGENT_UI_* browser settings, as NAME=value pairs.
	Environ []string
	// API is the controller's handler for /api/, /mcp and /.well-known/jwks.json.
	API http.Handler
	// StrippedHeaders are removed from every request before routing.
	StrippedHeaders []string
}

// StrippedHeaders returns the identity headers a browser must not set, less those the
// operator forwards. In trusted-proxy mode the authenticator's agent headers are always stripped.
func StrippedHeaders(forwarded []string, trustedProxy bool) []string {
	strip := slices.DeleteFunc(slices.Clone(identityHeaders), func(name string) bool {
		return slices.ContainsFunc(forwarded, func(f string) bool { return strings.EqualFold(strings.TrimSpace(f), name) })
	})
	if trustedProxy {
		strip = append(strip, "x-agent-name", "x-user-id")
	}
	return strip
}

// Run serves the UI until ctx ends. It returns at once when Root has no bundle.
func Run(ctx context.Context, cfg Config) error {
	logger := logging.FromContext(ctx).With("component", "ui_server")
	if cfg.Root == "" {
		return nil
	}
	handler, err := NewHandler(ctx, cfg)
	if errors.Is(err, os.ErrNotExist) {
		logger.WarnContext(ctx, "no UI bundle found; UI listener is off", "root", cfg.Root)
		return nil
	}
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.BindAddress)
	if err != nil {
		return fmt.Errorf("failed to listen for the UI on %s: %w", cfg.BindAddress, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Minute, IdleTimeout: 75 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.InfoContext(ctx, "starting UI server", "address", listener.Addr().String())
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to serve the UI: %w", err)
	}
	return nil
}

// NewHandler renders the browser settings and index.html, then routes the bundle and API.
func NewHandler(ctx context.Context, cfg Config) (http.Handler, error) {
	html, err := os.ReadFile(filepath.Join(cfg.Root, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to read the UI bundle: %w", err)
	}
	logger := logging.FromContext(ctx)
	envConfig, basePath := render(logger, cfg.Environ)
	html = setBaseHref(logger, html, basePath)
	started := time.Now()

	// ServeMux cleans every path but a CONNECT target's, which stripBasePath refuses.
	file := func(r *http.Request) string { return filepath.Join(cfg.Root, path.Clean("/"+r.URL.Path)) }
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "OK") })
	mux.HandleFunc("/env-config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeContent(w, r, "env-config.js", started, strings.NewReader(envConfig))
	})
	// The bundle entrypoint is never cached, or a client keeps loading the previous release's assets.
	index := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", started, bytes.NewReader(html))
	}
	mux.HandleFunc("/index.html", index)
	// The image ships no mock service worker; the SPA fallback would answer it with a misleading 200.
	mux.Handle("/mockServiceWorker.js", http.NotFoundHandler())
	// Vite fingerprints these filenames, so a given URL's bytes never change.
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if !serveFile(w, r, file(r)) {
			http.NotFound(w, r)
		}
	})
	// SPA fallback: a deep link such as /agents is a client-side route, not a file.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !serveFile(w, r, file(r)) {
			index(w, r)
		}
	})
	api := http.MaxBytesHandler(cfg.API, maxBodyBytes)
	for _, p := range apiPaths {
		mux.Handle(p, api)
	}
	// The UI's MCP servers page is also /mcp, so a browser loading it gets the app.
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
			index(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
	return stripHeaders(stripBasePath(mux, basePath), cfg.StrippedHeaders), nil
}

// render returns env-config.js for the given environment, and the validated base path.
func render(logger *slog.Logger, environ []string) (string, string) {
	vars := map[string]string{}
	var extensions []string
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		vars[name] = value
		if strings.HasPrefix(kv, "KAGENT_UI_EXTENSION_") {
			// Sorted as whole NAME=value lines, first line of the value only.
			first, _, _ := strings.Cut(kv, "\n")
			extensions = append(extensions, first)
		}
	}
	get := func(v env.StringVar) string { return cmp.Or(vars[v.Name()], v.DefaultValue()) }

	basePath := strings.TrimSuffix(get(env.UIBasePath), "/")
	if !basePathRe.MatchString(basePath) || dotSegmentRe.MatchString(basePath) || reservedPathRe.MatchString(basePath) {
		logger.Warn("KAGENT_UI_BASE_PATH is not a path like /ui, or starts with a path the UI serves; serving at the root", "value", basePath)
		basePath = ""
	}
	// A non-numeric timeout would abort every chat stream at once, so fall back instead.
	timeout := get(env.UIStreamTimeoutMS)
	if !digitsRe.MatchString(timeout) {
		logger.Warn("KAGENT_UI_STREAM_TIMEOUT_MS is not a number; using the default", "value", timeout)
		timeout = env.UIStreamTimeoutMS.DefaultValue()
	}

	// Extension settings pass through by prefix, so core never lists another product's keys.
	slices.Sort(extensions)
	fields := [][2]string{
		{"API_BASE_URL", get(env.UIAPIBaseURL)},
		{"SSO_REDIRECT_PATH", get(env.UISSORedirectPath)},
		{"STREAM_TIMEOUT_MS", timeout},
	}
	for _, kv := range extensions {
		name, value, _ := strings.Cut(kv, "=")
		fields = append(fields, [2]string{name, value})
	}
	fields = append(fields,
		[2]string{"BASE_PATH", basePath},
		[2]string{"ENABLE_MOCK_UI", cmp.Or(vars[env.UIEnableMock.Name()], "false")},
	)
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = "  " + jsString(f[0]) + ": " + jsString(f[1])
	}
	return "window.environmentVariables = {\n" + strings.Join(lines, ",\n") + "\n};\n", basePath
}

// jsString quotes s for a <script> element; encoding/json escapes `<` so markup cannot close it.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// setBaseHref points <base href> at the base path, so deep links load assets under the sub-path too.
func setBaseHref(logger *slog.Logger, html []byte, basePath string) []byte {
	out := strings.Replace(string(html), `<base href="/"`, `<base href="`+basePath+`/"`, 1)
	if !strings.Contains(out, `<base href="`+basePath+`/"`) {
		logger.Warn("could not set <base href> in index.html", "base_path", basePath+"/")
	}
	return []byte(out)
}

// stripHeaders removes headers a browser must not set before any route sees the request.
func stripHeaders(next http.Handler, names []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range names {
			r.Header.Del(name)
		}
		next.ServeHTTP(w, r)
	})
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

// Flush is asserted directly by the gRPC-Web wrapper, so Unwrap alone does not cover streams.
func (w *baseWriter) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }

func (w *baseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// serveFile serves a regular file, unlike http.ServeFile, which redirects /index.html.
// It writes nothing and returns false when name is not one.
func serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	http.ServeContent(w, r, name, fi.ModTime(), f)
	return true
}
