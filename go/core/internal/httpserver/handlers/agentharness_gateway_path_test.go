package handlers

import (
	"net/http"
	"testing"
)

func TestResolveGatewayUpstreamPath(t *testing.T) {
	t.Parallel()
	ns, name := "kagent", "my-claw"
	public := agentHarnessGatewayPublicPrefix(ns, name)

	tests := []struct {
		name      string
		path      string
		wsUpgrade bool
		wantUp    string
		wantRedir string
		wantOK    bool
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
			wantUp:    public,
			wantRedir: public,
			wantOK:    true,
		},
		{
			name:      "gateway without slash websocket",
			path:      "/api/agentharnesses/kagent/my-claw/gateway",
			wsUpgrade: true,
			wantUp:    public,
			wantOK:    true,
		},
		{
			name:   "gateway index",
			path:   "/api/agentharnesses/kagent/my-claw/gateway/",
			wantUp: public,
			wantOK: true,
		},
		{
			name:   "gateway asset",
			path:   "/api/agentharnesses/kagent/my-claw/gateway/assets/foo.js",
			wantUp: "/api/agentharnesses/kagent/my-claw/gateway/assets/foo.js",
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
