package handlers

import (
	"net/http"
	"strings"
	"testing"
)

func TestResolveGatewayUpstreamPath(t *testing.T) {
	t.Parallel()
	ns, name := "kagent", "my-claw"
	public := agentHarnessGatewayPublicPrefix(ns, name)

	tests := []struct {
		name       string
		path       string
		wsUpgrade    bool
		wantUp     string
		wantRedir  string
		wantOK     bool
	}{
		{
			name:      "harness root redirects",
			path:      "/api/agentharnesses/kagent/my-claw",
			wantRedir: public,
			wantOK:    true,
		},
		{
			name:      "gateway without slash redirects",
			path:      "/api/agentharnesses/kagent/my-claw/gateway",
			wantUp:    "/",
			wantRedir: public,
			wantOK:    true,
		},
		{
			name:      "gateway without slash websocket",
			path:      "/api/agentharnesses/kagent/my-claw/gateway",
			wsUpgrade: true,
			wantUp:    "/",
			wantRedir: public,
			wantOK:    true,
		},
		{
			name:   "gateway index",
			path:   "/api/agentharnesses/kagent/my-claw/gateway/",
			wantUp: "/",
			wantOK: true,
		},
		{
			name:   "gateway asset",
			path:   "/api/agentharnesses/kagent/my-claw/gateway/assets/foo.js",
			wantUp: "/assets/foo.js",
			wantOK: true,
		},
		{
			name:   "mis-resolved asset shim",
			path:   "/api/agentharnesses/kagent/my-claw/assets/foo.js",
			wantUp: "/assets/foo.js",
			wantOK: true,
		},
		{
			name:   "manifest shim",
			path:   "/api/agentharnesses/kagent/my-claw/manifest.webmanifest",
			wantUp: "/manifest.webmanifest",
			wantOK: true,
		},
		{
			name:   "unknown path",
			path:   "/api/agentharnesses/kagent/my-claw/api/v1/foo",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			up, redir, ok := resolveGatewayUpstreamPath(tt.path, ns, name, tt.wsUpgrade)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if up != tt.wantUp {
				t.Fatalf("upstream = %q, want %q", up, tt.wantUp)
			}
			if redir != tt.wantRedir {
				t.Fatalf("redirect = %q, want %q", redir, tt.wantRedir)
			}
		})
	}
}

func TestRewriteGatewayRootPaths(t *testing.T) {
	t.Parallel()
	prefix := "/api/agentharnesses/kagent/my-claw/gateway/"
	in := `<script src="/assets/index.js"></script><link href="/manifest.webmanifest"/>`
	out := string(rewriteGatewayRootPaths([]byte(in), prefix))
	if !strings.Contains(out, `src="/api/agentharnesses/kagent/my-claw/gateway/assets/index.js"`) {
		t.Fatalf("script src not rewritten: %s", out)
	}
	if !strings.Contains(out, `href="/api/agentharnesses/kagent/my-claw/gateway/manifest.webmanifest"`) {
		t.Fatalf("link href not rewritten: %s", out)
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api/x/gateway", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	if !isWebSocketUpgrade(req) {
		t.Fatal("expected websocket upgrade")
	}
	req2, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if isWebSocketUpgrade(req2) {
		t.Fatal("expected not websocket upgrade")
	}
}

func TestRewriteGatewayWebSocketPaths(t *testing.T) {
	t.Parallel()
	prefix := "/api/agentharnesses/kagent/my-claw/gateway/"
	in := `const u="ws://localhost:8001/api/agentharnesses/kagent/my-claw/gateway"; const v='wss://host/api/agentharnesses/kagent/my-claw/gateway'`
	out := string(rewriteGatewayWebSocketPaths([]byte(in), prefix))
	want := "/api/agentharnesses/kagent/my-claw/gateway/"
	if !strings.Contains(out, "ws://localhost:8001"+want) {
		t.Fatalf("ws URL not rewritten: %s", out)
	}
	if !strings.Contains(out, "wss://host"+want) {
		t.Fatalf("wss URL not rewritten: %s", out)
	}
}

func TestRewriteGatewayBodyStripsBaseAndCSP(t *testing.T) {
	t.Parallel()
	prefix := "/api/agentharnesses/kagent/my-claw/gateway/"
	in := `<html><head><base href="/"><meta http-equiv="Content-Security-Policy" content="base-uri 'none'"></head><script src="/assets/x.js"></script></html>`
	out := string(rewriteGatewayBody([]byte(in), "text/html", prefix))
	if strings.Contains(strings.ToLower(out), "<base") {
		t.Fatalf("expected base tag removed: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "content-security-policy") {
		t.Fatalf("expected CSP meta removed: %s", out)
	}
	if !strings.Contains(out, `src="/api/agentharnesses/kagent/my-claw/gateway/assets/x.js"`) {
		t.Fatalf("expected script rewritten: %s", out)
	}
}

func TestRewriteGatewayRootPathsSkipsRegexpFlags(t *testing.T) {
	t.Parallel()
	prefix := "/api/agentharnesses/kagent/my-claw/gateway/"
	in := `new RegExp("x","g");const f="/g";const w="/gateway";const a="/assets/a.js"`
	out := string(rewriteGatewayRootPaths([]byte(in), prefix))
	if strings.Contains(out, `"/api/agentharnesses/kagent/my-claw/gateway/g"`) {
		t.Fatalf("rewrote regexp flags string: %s", out)
	}
	if !strings.Contains(out, `f="/g"`) {
		t.Fatalf("expected /g left unchanged: %s", out)
	}
	if !strings.Contains(out, `"/api/agentharnesses/kagent/my-claw/gateway/assets/a.js"`) {
		t.Fatalf("expected /assets rewritten: %s", out)
	}
	if strings.Contains(out, `"/api/agentharnesses/kagent/my-claw/gateway/gateway"`) {
		t.Fatalf("must not rewrite bare /gateway path: %s", out)
	}
}
