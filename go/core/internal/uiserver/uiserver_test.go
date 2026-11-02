package uiserver

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHandler(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"index.html":      `<head><base href="/" /></head>`,
		"favicon.svg":     "<svg/>",
		"assets/app-1.js": "js",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var seen *http.Request
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		if r.URL.Path == "/api/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: one\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(200 * time.Millisecond)
			fmt.Fprint(w, "data: two\n\n")
			return
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		fmt.Fprint(w, "api "+r.URL.RequestURI())
	})
	handler, err := NewHandler(t.Context(), Config{
		Root:            root,
		Environ:         []string{"KAGENT_UI_BASE_PATH=/ui"},
		API:             api,
		StrippedHeaders: StrippedHeaders(nil, true),
	})
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
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	body := func(resp *http.Response) string {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	rendered := `<head><base href="/ui/" /></head>`
	for _, tc := range []struct{ path, status, body, cacheControl, contentType string }{
		{"/", "200", rendered, "no-store", "text/html"},
		{"/index.html", "200", rendered, "no-store", "text/html"},
		{"/agents/kagent/chat", "200", rendered, "no-store", "text/html"},
		{"/ui/agents", "200", rendered, "no-store", "text/html"},
		{"/env-config.js", "200", "", "no-store", "application/javascript"},
		{"/assets/app-1.js", "200", "js", "public, max-age=31536000, immutable", "text/javascript"},
		{"/favicon.svg", "200", "<svg/>", "", "image/svg+xml"},
		{"/mockServiceWorker.js", "404", "", "", ""},
		{"/health", "200", "OK", "", ""},
		{"/ui/api/agents?x=1", "200", "api /api/agents?x=1", "", ""},
		{"/mcp", "200", "api /mcp", "", ""},
		{"/.well-known/jwks.json", "200", "api /.well-known/jwks.json", "", ""},
	} {
		resp := get(tc.path)
		got := body(resp)
		if fmt.Sprint(resp.StatusCode) != tc.status || (tc.body != "" && got != tc.body) ||
			resp.Header.Get("Cache-Control") != tc.cacheControl || !strings.HasPrefix(resp.Header.Get("Content-Type"), tc.contentType) {
			t.Errorf("%s: %d %q cache %q type %q", tc.path, resp.StatusCode, got, resp.Header.Get("Cache-Control"), resp.Header.Get("Content-Type"))
		}
	}

	body(get("/api/x", "X-Forwarded-Email", "spoof@test", "X-User-Id", "spoof", "Authorization", "Bearer t"))
	if seen.Header.Get("X-Forwarded-Email") != "" || seen.Header.Get("X-User-Id") != "" || seen.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("api headers: %v", seen.Header)
	}

	// The first event must arrive before the handler finishes, under the base path too.
	start := time.Now()
	resp := get("/ui/api/stream")
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	if line != "data: one\n" || time.Since(start) > 150*time.Millisecond {
		t.Errorf("stream: first line %q after %v", line, time.Since(start))
	}
	body(resp)

	// CONNECT skips ServeMux's path cleaning, so it must never reach the file system.
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "CONNECT /assets/../../%s/favicon.svg HTTP/1.1\r\nHost: x\r\n\r\n", filepath.Base(root))
	status, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(status, "405") {
		t.Errorf("CONNECT traversal: %q", status)
	}

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := noRedirect.Get(server.URL + "/ui/api"); err != nil || resp.Header.Get("Location") != "/ui/api/" {
		t.Errorf("redirect under base path: %v %v", resp, err)
	}

	big, err := http.Post(server.URL+"/api/upload", "text/plain", strings.NewReader(strings.Repeat("x", maxBodyBytes+1)))
	if err != nil || big.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %v %v", big, err)
	}
}

func TestNewHandlerWithoutBundle(t *testing.T) {
	if _, err := NewHandler(t.Context(), Config{Root: t.TempDir()}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want a not-exist error, got %v", err)
	}
}

func TestRender(t *testing.T) {
	for name, tc := range map[string]struct {
		environ  []string
		config   string
		basePath string
	}{
		"defaults": {
			config: "window.environmentVariables = {\n" +
				"  \"API_BASE_URL\": \"/api\",\n" +
				"  \"SSO_REDIRECT_PATH\": \"/oauth2/start\",\n" +
				"  \"STREAM_TIMEOUT_MS\": \"1800000\",\n" +
				"  \"BASE_PATH\": \"\",\n" +
				"  \"ENABLE_MOCK_UI\": \"false\"\n" +
				"};\n",
		},
		"all set, escaped and sorted": {
			environ: []string{
				"KAGENT_UI_API_BASE_URL=https://x.test/api?a=\"b\"&c=<d>\\e",
				"KAGENT_UI_SSO_REDIRECT_PATH=/sso",
				"KAGENT_UI_STREAM_TIMEOUT_MS=0042",
				"KAGENT_UI_ENABLE_MOCK=true",
				"KAGENT_UI_BASE_PATH=/ui/",
				"KAGENT_UI_EXTENSION_B=two=equals=",
				"KAGENT_UI_EXTENSION_A=</script>\"\\",
				"KAGENT_UI_EXTENSION_LINES=first\nsecond",
				"KAGENT_UI_EXTENSIONLESS=ignored",
			},
			config: "window.environmentVariables = {\n" +
				"  \"API_BASE_URL\": \"https://x.test/api?a=\\\"b\\\"\\u0026c=\\u003cd\\u003e\\\\e\",\n" +
				"  \"SSO_REDIRECT_PATH\": \"/sso\",\n" +
				"  \"STREAM_TIMEOUT_MS\": \"0042\",\n" +
				"  \"KAGENT_UI_EXTENSION_A\": \"\\u003c/script\\u003e\\\"\\\\\",\n" +
				"  \"KAGENT_UI_EXTENSION_B\": \"two=equals=\",\n" +
				"  \"KAGENT_UI_EXTENSION_LINES\": \"first\",\n" +
				"  \"BASE_PATH\": \"/ui\",\n" +
				"  \"ENABLE_MOCK_UI\": \"true\"\n" +
				"};\n",
			basePath: "/ui",
		},
	} {
		t.Run(name, func(t *testing.T) {
			config, basePath := render(slog.Default(), tc.environ)
			if config != tc.config || basePath != tc.basePath {
				t.Errorf("got %q, %q\n%s", config, basePath, config)
			}
		})
	}

	for value, want := range map[string]string{
		"/a/b.c~d-e_f": "/a/b.c~d-e_f",
		"/api/x":       "",
		"/a/../b":      "",
		"ui":           "",
		"/u i":         "",
	} {
		if _, got := render(slog.Default(), []string{"KAGENT_UI_BASE_PATH=" + value}); got != want {
			t.Errorf("base path %q: got %q, want %q", value, got, want)
		}
	}
	if config, _ := render(slog.Default(), []string{"KAGENT_UI_STREAM_TIMEOUT_MS=-1"}); !strings.Contains(config, `"STREAM_TIMEOUT_MS": "1800000"`) {
		t.Errorf("bad timeout not replaced: %s", config)
	}
}

func TestStrippedHeaders(t *testing.T) {
	got := StrippedHeaders([]string{" X-Forwarded-User", "x-user-id"}, true)
	if slices.Contains(got, "x-forwarded-user") || !slices.Contains(got, "x-forwarded-email") || !slices.Contains(got, "x-user-id") {
		t.Errorf("trusted proxy: %v", got)
	}
	if got := StrippedHeaders(nil, false); slices.Contains(got, "x-user-id") || slices.Contains(got, "x-agent-name") {
		t.Errorf("insecure: %v", got)
	}
}
