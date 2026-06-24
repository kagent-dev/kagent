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

func TestGatewayProxyForwardsToAtenetRouterWithActorHost(t *testing.T) {
	t.Parallel()
	const actorHost = "ahr-kagent-my-claw.actors.resources.substrate.ate.dev"
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

	proxy := newAgentHarnessGatewayProxy(target, actorHost, token, publicPrefix, ns, name, testLog{t})
	req := httptest.NewRequest(http.MethodGet, publicPrefix, nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotHost != actorHost {
		t.Fatalf("upstream Host = %q, want %q", gotHost, actorHost)
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

func TestGatewayProxyRewriteTargetsAtenetRouterHostOnWebSocketPath(t *testing.T) {
	t.Parallel()
	const actorHost = "ahr-kagent-my-claw.actors.resources.substrate.ate.dev"
	ns, name := "kagent", "my-claw"
	publicPrefix := agentHarnessGatewayPublicPrefix(ns, name)

	target, host, err := substrate.GatewayRouterTarget(substrate.DefaultAtenetRouterURL, "ahr-kagent-my-claw")
	if err != nil {
		t.Fatal(err)
	}
	if host != actorHost {
		t.Fatalf("host = %q, want %q", host, actorHost)
	}
	proxy := newAgentHarnessGatewayProxy(target, host, "tok", publicPrefix, ns, name, testLog{t})
	req := httptest.NewRequest(http.MethodGet, strings.TrimSuffix(publicPrefix, "/"), nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Origin", "http://localhost:8001")
	req.Header.Set("Referer", "http://localhost:8001/api/agentharnesses/kagent/my-claw/gateway/")
	outReq := req.Clone(req.Context())

	proxy.Rewrite(&httputil.ProxyRequest{In: req, Out: outReq})

	if outReq.Host != actorHost {
		t.Fatalf("Host = %q, want actor host", outReq.Host)
	}
	if outReq.URL.Host != target.Host {
		t.Fatalf("URL.Host = %q, want router %q", outReq.URL.Host, target.Host)
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
	if outReq.Header.Get("Origin") != openclawLoopbackOrigin {
		t.Fatalf("Origin = %q, want %q", outReq.Header.Get("Origin"), openclawLoopbackOrigin)
	}
	if outReq.Header.Get("Referer") != openclawLoopbackOrigin+"/" {
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
