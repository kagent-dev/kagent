package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Identity headers an auth proxy in front of the UI sets and the controller may trust.
// They are removed from every proxied request unless ui.additionalForwardedHeaders lists them.
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

// forwardedHeaders are copied through unchanged where the route does not set them.
var forwardedHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"}

type config struct {
	upstream     string
	readTimeout  time.Duration
	sendTimeout  time.Duration
	maxBodyBytes int64
	stripped     []string
}

type route int

const (
	routeJWKS route = iota
	routeA2A
	routeMCP
	routeAPI
)

type cancelKey struct{}

type server struct {
	config
	root      string
	envConfig string
	index     []byte
	basePath  string
	started   time.Time
	proxies   map[route]http.Handler
}

// loadConfig reads the proxy settings the chart renders from ui.nginx.*, ui.additionalForwardedHeaders and the auth mode.
// A malformed value falls back to the chart default, as the browser settings do.
func loadConfig(environ []string) config {
	vars := map[string]string{}
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		vars[name] = value
	}
	get := func(name, fallback string) string {
		if v := vars[name]; v != "" {
			return v
		}
		return fallback
	}
	duration := func(name string) time.Duration {
		v := get(name, "1800s")
		d, ok := parseDuration(v)
		if !ok {
			fmt.Fprintf(os.Stderr, "entrypoint: %s='%s' is not a duration like 1800s; using 1800s\n", name, v)
			d = 30 * time.Minute
		}
		return d
	}
	size := get("KAGENT_UI_MAX_BODY_SIZE", "20m")
	maxBody, ok := parseSize(size)
	if !ok {
		fmt.Fprintf(os.Stderr, "entrypoint: KAGENT_UI_MAX_BODY_SIZE='%s' is not a size like 20m; using 20m\n", size)
		maxBody = 20 << 20
	}
	return config{
		upstream:     get("KAGENT_UI_UPSTREAM", "localhost:8083"),
		readTimeout:  duration("KAGENT_UI_PROXY_READ_TIMEOUT"),
		sendTimeout:  duration("KAGENT_UI_PROXY_SEND_TIMEOUT"),
		maxBodyBytes: maxBody,
		stripped:     strippedHeaders(vars["KAGENT_UI_FORWARDED_HEADERS"], vars["KAGENT_UI_TRUSTED_PROXY"] == "true"),
	}
}

// strippedHeaders returns the identity headers to clear: all of them less those the operator forwards.
// In trusted-proxy mode the authenticator's agent headers are always cleared, even when listed.
func strippedHeaders(forwarded string, trustedProxy bool) []string {
	opted := map[string]bool{}
	for _, name := range strings.Split(forwarded, ",") {
		opted[strings.ToLower(strings.TrimSpace(name))] = true
	}
	strip := slices.Clone(identityHeaders)
	if trustedProxy {
		strip = append(strip, "x-agent-name", "x-user-id")
		delete(opted, "x-agent-name")
		delete(opted, "x-user-id")
	}
	return slices.DeleteFunc(strip, func(name string) bool { return opted[name] })
}

var (
	durationRe = regexp.MustCompile(`^(\d+)(ms|s|m|h|d|w)?$`)
	sizeRe     = regexp.MustCompile(`^(\d+)([kKmMgG])?$`)
)

// parseDuration reads the time syntax the chart values use: 1800, 1800s, 30m or 1h, and "1h 30m" as a sum.
func parseDuration(v string) (time.Duration, bool) {
	units := map[string]time.Duration{"": time.Second, "ms": time.Millisecond, "s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}
	parts := strings.Fields(v)
	var total time.Duration
	for _, part := range parts {
		m := durationRe.FindStringSubmatch(part)
		if m == nil {
			return 0, false
		}
		n, err := strconv.ParseInt(m[1], 10, 32)
		if err != nil {
			return 0, false
		}
		total += time.Duration(n) * units[m[2]]
	}
	return total, len(parts) > 0
}

// parseSize reads 20m, 512k, 1g or a byte count. 0 means no limit.
func parseSize(v string) (int64, bool) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 32)
	if err != nil {
		return 0, false
	}
	return n << map[string]int{"": 0, "k": 10, "m": 20, "g": 30}[strings.ToLower(m[2])], true
}

func newServer(cfg config, root, envConfig string, index []byte, basePath string) *server {
	// The runtime image has no mime database, so the types the bundle uses are registered here.
	for ext, typ := range map[string]string{".woff": "font/woff", ".woff2": "font/woff2", ".ico": "image/x-icon", ".map": "application/json", ".txt": "text/plain; charset=utf-8", ".webmanifest": "application/manifest+json"} {
		_ = mime.AddExtensionType(ext, typ)
	}
	s := &server{config: cfg, root: root, envConfig: envConfig, index: index, basePath: basePath, started: time.Now()}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := (&net.Dialer{Timeout: 60 * time.Second}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &sendConn{Conn: c, timeout: cfg.sendTimeout}, nil
		},
		ResponseHeaderTimeout: cfg.readTimeout,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConnsPerHost:   32,
		DisableCompression:    true,
	}
	s.proxies = map[route]http.Handler{}
	for _, r := range []route{routeJWKS, routeA2A, routeMCP, routeAPI} {
		s.proxies[r] = s.newProxy(r, transport)
	}
	return s
}

// sendConn bounds each write to the upstream, like proxy_send_timeout.
type sendConn struct {
	net.Conn
	timeout time.Duration
}

func (c *sendConn) Write(p []byte) (int, error) {
	if c.timeout > 0 {
		_ = c.SetWriteDeadline(time.Now().Add(c.timeout))
	}
	return c.Conn.Write(p)
}

// idleBody ends a response that goes quiet for longer than the read timeout, however long the stream runs.
type idleBody struct {
	io.ReadCloser
	timer *time.Timer
	idle  time.Duration
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.timer.Reset(b.idle)
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.ReadCloser.Close()
}

func (s *server) newProxy(kind route, transport http.RoundTripper) http.Handler {
	rp := &httputil.ReverseProxy{
		Transport: transport,
		// Chat is server-sent events; buffering would hold tokens back.
		FlushInterval: -1,
		Rewrite:       func(pr *httputil.ProxyRequest) { s.rewrite(kind, pr) },
		ModifyResponse: func(resp *http.Response) error {
			if cancel, ok := resp.Request.Context().Value(cancelKey{}).(context.CancelFunc); ok && s.readTimeout > 0 {
				resp.Body = &idleBody{ReadCloser: resp.Body, timer: time.AfterFunc(s.readTimeout, cancel), idle: s.readTimeout}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var tooLarge *http.MaxBytesError
			var netErr net.Error
			switch {
			case errors.As(err, &tooLarge):
				w.WriteHeader(http.StatusRequestEntityTooLarge)
			case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
				w.WriteHeader(http.StatusGatewayTimeout)
			default:
				w.WriteHeader(http.StatusBadGateway)
			}
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if kind != routeAPI {
			// Only /api/ passes WebSocket upgrades through.
			r.Header.Del("Upgrade")
			r.Header.Del("Connection")
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		rp.ServeHTTP(w, r.WithContext(context.WithValue(ctx, cancelKey{}, cancel)))
	})
}

// rewrite sets the upstream request headers each nginx location set.
func (s *server) rewrite(kind route, pr *httputil.ProxyRequest) {
	in, out := pr.In, pr.Out
	out.URL.Scheme, out.URL.Host = "http", s.upstream
	out.URL.Path, out.URL.RawPath = in.URL.Path, ""

	host, _, err := net.SplitHostPort(in.Host)
	if err != nil {
		host = in.Host
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		host = "localhost"
	}
	out.Host = host
	ip, _, _ := net.SplitHostPort(in.RemoteAddr)

	// Headers a location does not set reach the controller as the client sent them.
	for _, name := range forwardedHeaders {
		if v := in.Header.Values(name); len(v) > 0 {
			out.Header[name] = slices.Clone(v)
		}
	}
	appended := strings.Join(append(in.Header.Values("X-Forwarded-For"), ip), ", ")
	switch kind {
	case routeA2A:
		out.Header.Set("X-Forwarded-Host", host)
		out.Header.Set("X-Forwarded-Proto", "http")
		out.Header.Set("X-Forwarded-For", appended)
		out.Header.Set("Origin", "http://"+host)
	case routeMCP:
		out.Header.Set("X-Forwarded-Proto", "http")
	case routeAPI:
		out.Header.Set("X-Real-IP", ip)
		out.Header.Set("X-Forwarded-For", appended)
		out.Header.Set("X-Forwarded-Proto", "http")
		out.Header.Set("X-Forwarded-Host", "localhost")
	}
	// The JWKS location clears nothing, so the controller sees what the client sent.
	if kind != routeJWKS {
		for _, name := range s.stripped {
			out.Header.Del(name)
		}
	}
}

// cleanPath normalises a request path like nginx does, keeping a trailing slash.
func cleanPath(p string) string {
	c := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}
	return c
}

// stripBase drops ui.basePath when the proxy in front forwards it unstripped.
func (s *server) stripBase(p string) string {
	if rest, ok := strings.CutPrefix(p, s.basePath); ok && s.basePath != "" && (rest == "" || rest[0] == '/') {
		return "/" + strings.TrimPrefix(rest, "/")
	}
	return p
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.maxBodyBytes > 0 {
		if r.ContentLength > s.maxBodyBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	}
	p := s.stripBase(cleanPath(r.URL.Path))
	r.URL.Path, r.URL.RawPath = p, ""

	switch {
	case strings.HasPrefix(p, "/health"):
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "OK")
	case p == "/env-config.js":
		// A stale copy would pin the client to the previous release's settings.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeContent(w, r, "env-config.js", s.started, strings.NewReader(s.envConfig))
	case p == "/.well-known/jwks.json":
		s.proxies[routeJWKS].ServeHTTP(w, r)
	case strings.HasPrefix(p, "/a2a/"):
		s.proxies[routeA2A].ServeHTTP(w, r)
	case p == "/mcp":
		s.proxies[routeMCP].ServeHTTP(w, r)
	case strings.HasPrefix(p, "/api/"):
		s.proxies[routeAPI].ServeHTTP(w, r)
	case p == "/mockServiceWorker.js":
		// The image ships no worker; the SPA fallback would answer with a misleading 200.
		http.NotFound(w, r)
	case strings.HasPrefix(p, "/assets/"):
		// Vite fingerprints these filenames, so a given URL's bytes never change.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if !s.serveFile(w, r, p) {
			http.NotFound(w, r)
		}
	case p == "/index.html" || p == "/":
		s.serveIndex(w, r)
	default:
		// SPA fallback: a deep link such as /agents is a client-side route, not a file.
		if !s.serveFile(w, r, p) {
			s.serveIndex(w, r)
		}
	}
}

// serveIndex answers with the app shell, never cached, or a client keeps loading the previous release's assets.
func (s *server) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "index.html", s.started, bytes.NewReader(s.index))
}

// serveFile serves a regular file under the bundle root and writes nothing when p is not one.
func (s *server) serveFile(w http.ResponseWriter, r *http.Request, p string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	f, err := os.Open(filepath.Join(s.root, filepath.FromSlash(p)))
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	return true
}

// accessLog writes one line per request to stdout, in nginx's combined format.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		fmt.Printf("%s - - [%s] \"%s %s %s\" %d %d \"%s\" \"%s\"\n", r.RemoteAddr, time.Now().Format("02/Jan/2006:15:04:05 -0700"),
			r.Method, r.RequestURI, r.Proto, rec.status, rec.bytes, r.Referer(), r.UserAgent())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach Flush and Hijack, which streams and upgrades need.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
