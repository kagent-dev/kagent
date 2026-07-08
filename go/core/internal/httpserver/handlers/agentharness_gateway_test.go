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

func TestACPProxyForwardsToAtenetRouterWithActorHost(t *testing.T) {
	t.Parallel()
	const actorHost = "ahr-kagent-my-claw.kagent.actors.resources.substrate.ate.dev"

	var gotHost, gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxy := newAgentHarnessACPProxy(target, actorHost, testLog{t})
	req := httptest.NewRequest(http.MethodGet, "/api/agentharnesses/kagent/my-claw/acp", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotHost != actorHost {
		t.Fatalf("upstream Host = %q, want %q", gotHost, actorHost)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want empty", gotAuth)
	}
	if gotPath != "/acp" {
		t.Fatalf("upstream path = %q, want /acp", gotPath)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "ok") {
		t.Fatalf("response body missing upstream content: %s", body)
	}
}

func TestACPProxyRewriteTargetsAtenetRouterHost(t *testing.T) {
	t.Parallel()
	const actorHost = "ahr-kagent-my-claw.kagent.actors.resources.substrate.ate.dev"

	target, host, err := substrate.GatewayRouterTarget(substrate.DefaultAtenetRouterURL, "kagent", "ahr-kagent-my-claw")
	if err != nil {
		t.Fatal(err)
	}
	if host != actorHost {
		t.Fatalf("host = %q, want %q", host, actorHost)
	}
	proxy := newAgentHarnessACPProxy(target, host, testLog{t})
	req := httptest.NewRequest(http.MethodGet, "/api/agentharnesses/kagent/my-claw/acp", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	outReq := req.Clone(req.Context())

	proxy.Rewrite(&httputil.ProxyRequest{In: req, Out: outReq})

	if outReq.Host != actorHost {
		t.Fatalf("Host = %q, want actor host", outReq.Host)
	}
	if outReq.URL.Host != target.Host {
		t.Fatalf("URL.Host = %q, want router %q", outReq.URL.Host, target.Host)
	}
	if outReq.URL.Path != "/acp" {
		t.Fatalf("URL.Path = %q, want /acp", outReq.URL.Path)
	}
	if outReq.Header.Get("Authorization") != "" {
		t.Fatalf("Authorization should not be set: %q", outReq.Header.Get("Authorization"))
	}
	if outReq.Header.Get("x-openclaw-scopes") != "" {
		t.Fatalf("scopes header should not be set: %q", outReq.Header.Get("x-openclaw-scopes"))
	}
}

type testLog struct {
	t *testing.T
}

func (l testLog) Error(err error, msg string, _ ...any) {
	l.t.Helper()
	l.t.Logf("%s: %v", msg, err)
}
