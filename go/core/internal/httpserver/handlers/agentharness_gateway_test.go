package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

func TestSubstrateGatewayPodTarget(t *testing.T) {
	t.Parallel()
	target, host, err := substrateGatewayPodTarget("10.244.0.29", 80)
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

func TestSubstrateGatewayPodTargetCustomPort(t *testing.T) {
	t.Parallel()
	target, host, err := substrateGatewayPodTarget("10.244.0.29", 8080)
	if err != nil {
		t.Fatal(err)
	}
	if host != "10.244.0.29" {
		t.Fatalf("host = %q", host)
	}
	if target.Scheme != "http" || target.Host != "10.244.0.29:8080" {
		t.Fatalf("target = %s", target.String())
	}
}

func TestSubstrateGatewayPodTargetRejectsInvalidIP(t *testing.T) {
	t.Parallel()
	_, _, err := substrateGatewayPodTarget("not-an-ip", 80)
	if err == nil {
		t.Fatal("expected error for invalid pod IP")
	}
}

func TestGatewayProxyForwardsToPodIPWithAuthHeaders(t *testing.T) {
	t.Parallel()
	const podIP = "10.244.0.29"
	const token = "some-token"
	ns, name := "kagent", "my-claw"
	publicPrefix := substrate.AgentHarnessGatewayUIPath(ns, name)

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

	proxy := newAgentHarnessGatewayProxy(target, podIP, token, publicPrefix, ns, name, 80, testLog{t})
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
	if gotPath != publicPrefix {
		t.Fatalf("upstream path = %q, want %q", gotPath, publicPrefix)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("response body missing upstream content: %s", body)
	}
}

func TestGatewayProxyRewriteTargetsPodIPOnWebSocketPath(t *testing.T) {
	t.Parallel()
	const podIP = "10.244.0.29"
	ns, name := "kagent", "my-claw"
	publicPrefix := substrate.AgentHarnessGatewayUIPath(ns, name)

	target, err := url.Parse("http://" + podIP + ":80")
	if err != nil {
		t.Fatal(err)
	}
	proxy := newAgentHarnessGatewayProxy(target, podIP, "tok", publicPrefix, ns, name, 80, testLog{t})
	req := httptest.NewRequest(http.MethodGet, strings.TrimSuffix(publicPrefix, "/"), nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Origin", "http://localhost:8001")
	req.Header.Set("Referer", "http://localhost:8001/api/agentharnesses/kagent/my-claw/gateway/")
	outReq := req.Clone(req.Context())

	proxy.Rewrite(&httputil.ProxyRequest{In: req, Out: outReq})

	if outReq.Host != podIP {
		t.Fatalf("Host = %q, want pod IP", outReq.Host)
	}
	if outReq.URL.Host != podIP+":80" {
		t.Fatalf("URL.Host = %q", outReq.URL.Host)
	}
	if outReq.URL.Path != publicPrefix {
		t.Fatalf("URL.Path = %q, want %q", outReq.URL.Path, publicPrefix)
	}
	if outReq.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("missing Authorization")
	}
	if outReq.Header.Get("x-openclaw-scopes") != openclawDefaultOperatorScopes {
		t.Fatalf("missing scopes header")
	}
	if outReq.Header.Get("Origin") != openclawLoopbackOrigin(80) {
		t.Fatalf("Origin = %q, want %q", outReq.Header.Get("Origin"), openclawLoopbackOrigin(80))
	}
	if outReq.Header.Get("Referer") != openclawLoopbackOrigin(80)+"/" {
		t.Fatalf("Referer = %q", outReq.Header.Get("Referer"))
	}
}

type testLog struct {
	t *testing.T
}

func (l testLog) Error(err error, msg string, _ ...any) {
	l.t.Helper()
	l.t.Logf("%s: %v", msg, err)
}
