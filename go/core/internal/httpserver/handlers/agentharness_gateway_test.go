package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSubstrateGatewayPodTarget(t *testing.T) {
	t.Parallel()
	target, host, err := substrateGatewayPodTarget("10.244.0.29")
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.244.0.29" {
		t.Fatalf("host = %q", host)
	}
	if target.Scheme != "http" || target.Host != "10.244.0.29:80" {
		t.Fatalf("target = %s", target.String())
	}
}

func TestSubstrateGatewayPodTargetRejectsInvalidIP(t *testing.T) {
	t.Parallel()
	_, _, err := substrateGatewayPodTarget("not-an-ip")
	if err == nil {
		t.Fatal("expected error for invalid pod IP")
	}
}

func TestGatewayProxyForwardsToPodIPWithAuthHeaders(t *testing.T) {
	t.Parallel()
	const podIP = "10.244.0.29"
	const token = "some-token"
	ns, name := "kagent", "my-claw"
	publicPrefix := agentHarnessGatewayPublicPrefix(ns, name)

	var gotHost, gotAuth, gotScopes, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotAuth = r.Header.Get("Authorization")
		gotScopes = r.Header.Get("x-openclaw-scopes")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head></head><body>ok</body></html>"))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxy := newAgentHarnessGatewayProxy(target, podIP, token, publicPrefix, ns, name, testLog{t})
	req := httptest.NewRequest(http.MethodGet, publicPrefix, nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotHost != podIP {
		t.Fatalf("upstream Host = %q, want %q", gotHost, podIP)
	}
	if gotAuth != "Bearer "+token {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotScopes != openclawDefaultOperatorScopes {
		t.Fatalf("x-openclaw-scopes = %q", gotScopes)
	}
	if gotPath != "/" {
		t.Fatalf("upstream path = %q, want /", gotPath)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("response body missing upstream content: %s", body)
	}
}

func TestGatewayProxyDirectorTargetsPodIPOnWebSocketPath(t *testing.T) {
	t.Parallel()
	const podIP = "10.244.0.29"
	ns, name := "kagent", "my-claw"
	publicPrefix := agentHarnessGatewayPublicPrefix(ns, name)

	target, err := url.Parse("http://" + podIP + ":80")
	if err != nil {
		t.Fatal(err)
	}
	proxy := newAgentHarnessGatewayProxy(target, podIP, "tok", publicPrefix, ns, name, testLog{t})
	req := httptest.NewRequest(http.MethodGet, strings.TrimSuffix(publicPrefix, "/"), nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Origin", "http://localhost:8001")
	req.Header.Set("Referer", "http://localhost:8001/api/agentharnesses/kagent/my-claw/gateway/")

	proxy.Director(req)

	if req.Host != podIP {
		t.Fatalf("Host = %q, want pod IP", req.Host)
	}
	if req.URL.Host != podIP+":80" {
		t.Fatalf("URL.Host = %q", req.URL.Host)
	}
	if req.URL.Path != "/" {
		t.Fatalf("URL.Path = %q, want /", req.URL.Path)
	}
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("missing Authorization")
	}
	if req.Header.Get("x-openclaw-scopes") != openclawDefaultOperatorScopes {
		t.Fatalf("missing scopes header")
	}
	if req.Header.Get("Origin") != openclawLoopbackOrigin {
		t.Fatalf("Origin = %q, want %q", req.Header.Get("Origin"), openclawLoopbackOrigin)
	}
	if req.Header.Get("Referer") != openclawLoopbackOrigin+"/" {
		t.Fatalf("Referer = %q", req.Header.Get("Referer"))
	}
}

type testLog struct {
	t *testing.T
}

func (l testLog) Error(err error, msg string, _ ...any) {
	l.t.Helper()
	l.t.Logf("%s: %v", msg, err)
}
