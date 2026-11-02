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
	server := httptest.NewServer(newHandler(root, generated, "/ui"))
	defer server.Close()

	for _, tc := range []struct{ path, status, body, cacheControl, contentType string }{
		{"/", "200", "rendered", "no-store", "text/html"},
		{"/index.html", "200", "rendered", "no-store", "text/html"},
		{"/agents/kagent/chat", "200", "rendered", "no-store", "text/html"},
		{"/ui", "200", "rendered", "no-store", "text/html"},
		{"/ui/agents", "200", "rendered", "no-store", "text/html"},
		{"/ui/assets/app-1.js", "200", "js", "public, max-age=31536000, immutable", "text/javascript"},
		{"/env-config.js", "200", "window.environmentVariables = {};", "no-store", "application/javascript"},
		{"/assets/app-1.js", "200", "js", "public, max-age=31536000, immutable", "text/javascript"},
		{"/assets/missing.js", "404", "", "public, max-age=31536000, immutable", ""},
		{"/favicon.svg", "200", "<svg/>", "", "image/svg+xml"},
		{"/mockServiceWorker.js", "404", "", "", ""},
		{"/health", "200", "OK", "", ""},
	} {
		resp, err := http.Get(server.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		bufio.NewReader(resp.Body).WriteTo(&b)
		resp.Body.Close()
		if fmt.Sprint(resp.StatusCode) != tc.status || (tc.body != "" && b.String() != tc.body) ||
			resp.Header.Get("Cache-Control") != tc.cacheControl || !strings.HasPrefix(resp.Header.Get("Content-Type"), tc.contentType) {
			t.Errorf("%s: %d %q cache %q type %q", tc.path, resp.StatusCode, b.String(), resp.Header.Get("Cache-Control"), resp.Header.Get("Content-Type"))
		}
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
	if resp, _ := noRedirect.Get(server.URL + "/ui/assets"); resp.Header.Get("Location") != "/ui/assets/" {
		t.Errorf("redirect under base path: %q", resp.Header.Get("Location"))
	}
}
