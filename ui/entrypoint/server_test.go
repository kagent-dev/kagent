package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandler(t *testing.T) {
	root, generated := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{
		root + "/index.html":         "unrendered",
		root + "/favicon.svg":        "<svg/>",
		root + "/assets/app-1.js":    "js",
		generated + "/index.html":    "rendered",
		generated + "/env-config.js": "window.environmentVariables = {};",
	} {
		os.MkdirAll(filepath.Dir(name), 0o755)
		os.WriteFile(name, []byte(body), 0o644)
	}

	var seen *http.Request
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.URL.Path == "/a2a/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: one\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(200 * time.Millisecond)
			fmt.Fprint(w, "data: two\n\n")
			return
		}
		fmt.Fprint(w, "backend "+r.URL.RequestURI())
	}))
	defer backend.Close()

	handler, err := newHandler(config{
		ClientMaxBodySize: "1k", ProxyReadTimeout: "1800s", ProxySendTimeout: "1800",
		Proxies: []proxy{
			{Path: "/api/", Backend: backend.URL, Headers: map[string]string{
				"Host": "$host", "X-Forwarded-For": "$proxy_add_x_forwarded_for", "x-forwarded-email": "",
			}},
			{Path: "/a2a/", Backend: backend.URL, Headers: map[string]string{"Origin": "$scheme://$host"}},
		},
	}, root, generated, "/ui")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	get := func(path string, header ...string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		for i := 0; i < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		req.Host = req.Header.Get("Host")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	body := func(resp *http.Response) string {
		defer resp.Body.Close()
		var b strings.Builder
		bufio.NewReader(resp.Body).WriteTo(&b)
		return b.String()
	}

	for _, tc := range []struct{ path, status, body, cacheControl, contentType string }{
		{"/", "200", "rendered", "no-store", "text/html"},
		{"/index.html", "200", "rendered", "no-store", "text/html"},
		{"/agents/kagent/chat", "200", "rendered", "no-store", "text/html"},
		{"/ui/agents", "200", "rendered", "no-store", "text/html"},
		{"/env-config.js", "200", "window.environmentVariables = {};", "no-store", "application/javascript"},
		{"/assets/app-1.js", "200", "js", "public, max-age=31536000, immutable", "text/javascript"},
		{"/favicon.svg", "200", "<svg/>", "", "image/svg+xml"},
		{"/mockServiceWorker.js", "404", "", "", ""},
		{"/health", "200", "OK", "", ""},
		{"/ui/api/agents?x=1", "200", "backend /api/agents?x=1", "", ""},
	} {
		resp := get(tc.path)
		got := body(resp)
		if fmt.Sprint(resp.StatusCode) != tc.status || (tc.body != "" && got != tc.body) ||
			resp.Header.Get("Cache-Control") != tc.cacheControl || !strings.HasPrefix(resp.Header.Get("Content-Type"), tc.contentType) {
			t.Errorf("%s: %d %q cache %q type %q", tc.path, resp.StatusCode, got, resp.Header.Get("Cache-Control"), resp.Header.Get("Content-Type"))
		}
	}

	body(get("/api/x", "Host", "ui.test:8080", "X-Forwarded-For", "10.0.0.1", "X-Forwarded-Email", "spoof@test", "Authorization", "Bearer t"))
	if seen.Host != "ui.test" || seen.Header.Get("X-Forwarded-For") != "10.0.0.1, 127.0.0.1" ||
		seen.Header.Get("X-Forwarded-Email") != "" || seen.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("api headers: host %q %v", seen.Host, seen.Header)
	}

	// The first event must arrive before the backend finishes the response.
	start := time.Now()
	resp := get("/a2a/stream", "Host", "ui.test")
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	if line != "data: one\n" || time.Since(start) > 150*time.Millisecond {
		t.Errorf("stream: first line %q after %v", line, time.Since(start))
	}
	body(resp)
	if seen.Header.Get("Origin") != "http://ui.test" {
		t.Errorf("a2a origin %q", seen.Header.Get("Origin"))
	}

	// CONNECT skips ServeMux's path cleaning, so it must never reach the file system.
	conn, _ := net.Dial("tcp", server.Listener.Addr().String())
	fmt.Fprintf(conn, "CONNECT /assets/../../%s/favicon.svg HTTP/1.1\r\nHost: x\r\n\r\n", filepath.Base(root))
	status, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(status, "405") {
		t.Errorf("CONNECT traversal: %q", status)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, _ := noRedirect.Get(server.URL + "/ui/api"); resp.Header.Get("Location") != "/ui/api/" {
		t.Errorf("redirect under base path: %q", resp.Header.Get("Location"))
	}

	big, _ := http.Post(server.URL+"/api/upload", "text/plain", strings.NewReader(strings.Repeat("x", 2048)))
	if big.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %d", big.StatusCode)
	}
}

func TestIdleTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer backend.Close()
	handler, _ := newHandler(config{ClientMaxBodySize: "0", ProxyReadTimeout: "100ms", ProxySendTimeout: "1s",
		Proxies: []proxy{{Path: "/a2a/", Backend: backend.URL}}}, t.TempDir(), t.TempDir(), "")
	server := httptest.NewServer(handler)
	defer server.Close()

	done := make(chan struct{})
	go func() {
		// An Upgrade header the backend does not honour must not switch the timeout off.
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/a2a/stream", nil)
		req.Header.Set("Upgrade", "bogus")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			bufio.NewReader(resp.Body).WriteTo(&strings.Builder{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("an idle stream outlived proxyReadTimeout")
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]int64{"20m": 20 << 20, "512k": 512 << 10, "1G": 1 << 30, "0": 0, "100": 100} {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v", in, got, err)
		}
	}
	for in, want := range map[string]time.Duration{"1800s": 30 * time.Minute, "900": 15 * time.Minute, "1h": time.Hour} {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v", in, got, err)
		}
	}
}
