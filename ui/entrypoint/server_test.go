package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const indexHTML = `<html><head><base href="/"></head>app</html>`

// start runs the UI server against an upstream stub and returns the UI's URL.
func start(t *testing.T, upstream http.Handler, mutate func(*config), basePath string) string {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	root := t.TempDir()
	check(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	check(t, os.WriteFile(filepath.Join(root, "assets/app-abc.js"), []byte("js"), 0o644))
	check(t, os.WriteFile(filepath.Join(root, "favicon.ico"), []byte("ico"), 0o644))
	cfg := config{
		upstream:     strings.TrimPrefix(up.URL, "http://"),
		readTimeout:  30 * time.Second,
		sendTimeout:  30 * time.Second,
		maxBodyBytes: 1 << 10,
		stripped:     strippedHeaders("", false),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	ui := httptest.NewServer(newServer(cfg, root, "window.environmentVariables = {};\n", []byte(indexHTML), basePath))
	t.Cleanup(ui.Close)
	return ui.URL
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, url string, header ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	check(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

var notCalled = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unexpected upstream call", 500) })

func TestStatic(t *testing.T) {
	url := start(t, notCalled, nil, "/ui")
	for _, tc := range []struct {
		path, body, cache string
		status            int
	}{
		{"/", indexHTML, "no-store", 200},
		{"/index.html", indexHTML, "no-store", 200},
		{"/agents/foo", indexHTML, "no-store", 200},
		{"/env-config.js", "window.environmentVariables = {};\n", "no-store", 200},
		{"/assets/app-abc.js", "js", "public, max-age=31536000, immutable", 200},
		{"/assets/missing.js", "404 page not found\n", "public, max-age=31536000, immutable", 404},
		{"/favicon.ico", "ico", "", 200},
		{"/mockServiceWorker.js", "404 page not found\n", "", 404},
		{"/health", "OK", "", 200},
		{"/ui", indexHTML, "no-store", 200},
		{"/ui/assets/app-abc.js", "js", "public, max-age=31536000, immutable", 200},
		{"/uix", indexHTML, "no-store", 200},
		{"/../../etc/passwd", indexHTML, "no-store", 200},
	} {
		resp, body := get(t, url+tc.path)
		if resp.StatusCode != tc.status || body != tc.body || resp.Header.Get("Cache-Control") != tc.cache {
			t.Errorf("%s: got %d %q cache=%q", tc.path, resp.StatusCode, body, resp.Header.Get("Cache-Control"))
		}
	}
	if resp, _ := get(t, url+"/env-config.js"); resp.Header.Get("Content-Type") != "application/javascript" {
		t.Errorf("env-config.js content type %q", resp.Header.Get("Content-Type"))
	}
	if resp, _ := get(t, url+"/favicon.ico"); resp.Header.Get("Content-Type") != "image/x-icon" {
		t.Errorf("favicon content type %q", resp.Header.Get("Content-Type"))
	}
}

func echoHeaders(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"X-Auth-Request-User", "X-Forwarded-User", "X-Agent-Name", "X-User-Id", "Authorization", "X-Custom", "Upgrade", "Origin", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip"} {
		w.Header().Set("Got-"+name, r.Header.Get(name))
	}
	w.Header().Set("Got-Host", r.Host)
	w.Header().Set("Got-Path", r.URL.Path)
}

var spoofed = []string{"X-Auth-Request-User", "u", "X-Forwarded-User", "f", "X-Agent-Name", "a", "X-User-Id", "i", "Authorization", "tok", "X-Custom", "c"}

func TestStripsIdentityHeaders(t *testing.T) {
	for _, tc := range []struct {
		name      string
		forwarded string
		trusted   bool
		want      map[string]string
	}{
		{"default", "", false, map[string]string{"X-Auth-Request-User": "", "X-Forwarded-User": "", "X-Agent-Name": "a", "X-User-Id": "i", "Authorization": "tok", "X-Custom": "c"}},
		{"opt in, case-insensitive", " x-forwarded-user ", false, map[string]string{"X-Auth-Request-User": "", "X-Forwarded-User": "f"}},
		{"trusted proxy", "", true, map[string]string{"X-Agent-Name": "", "X-User-Id": "", "X-Auth-Request-User": ""}},
		{"trusted proxy ignores opt in", "X-Agent-Name,X-User-Id", true, map[string]string{"X-Agent-Name": "", "X-User-Id": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := start(t, http.HandlerFunc(echoHeaders), func(c *config) { c.stripped = strippedHeaders(tc.forwarded, tc.trusted) }, "")
			for _, p := range []string{"/api/x", "/a2a/x", "/mcp"} {
				resp, _ := get(t, url+p, spoofed...)
				for name, want := range tc.want {
					if got := resp.Header.Get("Got-" + name); got != want {
						t.Errorf("%s %s: got %q want %q", p, name, got, want)
					}
				}
			}
		})
	}
}

func TestProxyHeaders(t *testing.T) {
	url := start(t, http.HandlerFunc(echoHeaders), nil, "/ui")
	h := func(p string, extra ...string) *http.Response {
		resp, _ := get(t, url+p, extra...)
		return resp
	}
	a2a := h("/ui/a2a/ns/agent", "X-Forwarded-For", "9.9.9.9", "Origin", "https://evil.test")
	if a2a.Header.Get("Got-Path") != "/a2a/ns/agent" || a2a.Header.Get("Got-X-Forwarded-Proto") != "http" ||
		a2a.Header.Get("Got-Origin") != "http://127.0.0.1" || a2a.Header.Get("Got-X-Forwarded-Host") != "127.0.0.1" ||
		a2a.Header.Get("Got-X-Forwarded-For") != "9.9.9.9, 127.0.0.1" || a2a.Header.Get("Got-Host") != "127.0.0.1" {
		t.Errorf("a2a headers: %v", a2a.Header)
	}
	api := h("/api/x")
	if api.Header.Get("Got-X-Real-Ip") != "127.0.0.1" || api.Header.Get("Got-X-Forwarded-Host") != "localhost" || api.Header.Get("Got-X-Forwarded-For") != "127.0.0.1" {
		t.Errorf("api headers: %v", api.Header)
	}
	mcp := h("/mcp", "X-Forwarded-For", "9.9.9.9")
	if mcp.Header.Get("Got-X-Forwarded-For") != "9.9.9.9" || mcp.Header.Get("Got-X-Forwarded-Proto") != "http" {
		t.Errorf("mcp headers: %v", mcp.Header)
	}
	// Only /api/ passes an upgrade through.
	for p, want := range map[string]string{"/api/x": "websocket", "/a2a/x": "", "/mcp": ""} {
		req, _ := http.NewRequest(http.MethodGet, url+p, nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		resp, err := http.DefaultClient.Do(req)
		check(t, err)
		resp.Body.Close()
		if got := resp.Header.Get("Got-Upgrade"); got != want {
			t.Errorf("%s: upgrade %q want %q", p, got, want)
		}
	}
	jwks := h("/.well-known/jwks.json", "X-Forwarded-User", "f")
	if jwks.Header.Get("Got-X-Forwarded-User") != "f" || jwks.Header.Get("Got-Path") != "/.well-known/jwks.json" {
		t.Errorf("jwks headers: %v", jwks.Header)
	}
}

func TestBodyCap(t *testing.T) {
	url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		w.Header().Set("Got-Bytes", strings.Repeat("x", int(n)/100))
	}), nil, "")
	post := func(body io.Reader, length int64) int {
		req, _ := http.NewRequest(http.MethodPost, url+"/api/x", body)
		req.ContentLength = length
		resp, err := http.DefaultClient.Do(req)
		check(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(strings.NewReader(strings.Repeat("a", 1024)), 1024); got != 200 {
		t.Errorf("body at the cap: %d", got)
	}
	if got := post(strings.NewReader(strings.Repeat("a", 1025)), 1025); got != 413 {
		t.Errorf("body over the cap: %d", got)
	}
	// Chunked bodies carry no length up front, so the cap trips mid-stream.
	if got := post(io.NopCloser(strings.NewReader(strings.Repeat("a", 4096))), -1); got != 413 {
		t.Errorf("chunked body over the cap: %d", got)
	}
	unlimited := start(t, http.HandlerFunc(echoHeaders), func(c *config) { c.maxBodyBytes = 0 }, "")
	req, _ := http.NewRequest(http.MethodPost, unlimited+"/api/x", strings.NewReader(strings.Repeat("a", 1<<20)))
	resp, err := http.DefaultClient.Do(req)
	check(t, err)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("unlimited body: %d", resp.StatusCode)
	}
}

func TestStreamingFlushesImmediately(t *testing.T) {
	release := make(chan struct{})
	url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: two\n\n")
	}), nil, "")
	resp, err := http.Get(url + "/a2a/x")
	check(t, err)
	defer resp.Body.Close()
	buf := make([]byte, 11)
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "data: one\n\n" {
		t.Fatalf("first event before the stream ends: %q %v", buf, err)
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "data: two\n\n" {
		t.Errorf("second event: %q", rest)
	}
}

func TestTimeouts(t *testing.T) {
	t.Run("no response headers", func(t *testing.T) {
		url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) }), func(c *config) { c.readTimeout = 100 * time.Millisecond }, "")
		resp, _ := get(t, url+"/api/x")
		if resp.StatusCode != http.StatusGatewayTimeout {
			t.Errorf("status %d", resp.StatusCode)
		}
	})
	t.Run("stream goes quiet", func(t *testing.T) {
		url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "a")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}), func(c *config) { c.readTimeout = 200 * time.Millisecond }, "")
		start := time.Now()
		resp, err := http.Get(url + "/api/x")
		check(t, err)
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		if err == nil || time.Since(start) > 3*time.Second {
			t.Errorf("quiet stream stayed open: err=%v after %s", err, time.Since(start))
		}
	})
	t.Run("busy stream outlives the timeout", func(t *testing.T) {
		url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for range 8 {
				io.WriteString(w, "x")
				w.(http.Flusher).Flush()
				time.Sleep(50 * time.Millisecond)
			}
		}), func(c *config) { c.readTimeout = 200 * time.Millisecond }, "")
		_, body := get(t, url+"/api/x")
		if body != "xxxxxxxx" {
			t.Errorf("body %q", body)
		}
	})
}

func TestParse(t *testing.T) {
	for in, want := range map[string]time.Duration{"1800s": 30 * time.Minute, "30m": 30 * time.Minute, "1800": 30 * time.Minute, "1h 30m": 90 * time.Minute, "500ms": 500 * time.Millisecond, "0": 0} {
		if got, ok := parseDuration(in); !ok || got != want {
			t.Errorf("parseDuration(%q) = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "abc", "-1s", "1.5s"} {
		if _, ok := parseDuration(in); ok {
			t.Errorf("parseDuration(%q) accepted", in)
		}
	}
	for in, want := range map[string]int64{"20m": 20 << 20, "512k": 512 << 10, "1G": 1 << 30, "100": 100, "0": 0} {
		if got, ok := parseSize(in); !ok || got != want {
			t.Errorf("parseSize(%q) = %v %v", in, got, ok)
		}
	}
	if _, ok := parseSize("20x"); ok {
		t.Error("parseSize accepted 20x")
	}
}

func TestDropsUnderscoreHeaders(t *testing.T) {
	url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, v := range r.Header {
			w.Header().Add("Got", name+"="+strings.Join(v, ","))
		}
	}), nil, "")
	resp, _ := get(t, url+"/api/x", "X_Auth_Request_User", "evil", "X-Custom", "ok")
	got := strings.Join(resp.Header.Values("Got"), " ")
	if strings.Contains(got, "_") || !strings.Contains(got, "X-Custom=ok") {
		t.Errorf("upstream headers: %s", got)
	}
}

func TestWebSocketUpgrade(t *testing.T) {
	url := start(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		rw.Flush()
		line, _ := rw.ReadString('\n')
		io.WriteString(rw, "echo "+line)
		rw.Flush()
	}), nil, "")
	conn, err := net.Dial("tcp", strings.TrimPrefix(url, "http://"))
	check(t, err)
	defer conn.Close()
	io.WriteString(conn, "GET /api/ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	check(t, err)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	io.WriteString(conn, "ping\n")
	if line, _ := br.ReadString('\n'); line != "echo ping\n" {
		t.Errorf("reply %q", line)
	}
}
