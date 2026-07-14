package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestVisibilityAllowsApp pins the spec rule the call-tool handler enforces:
// visibility defaults to ["model","app"], so only a tool that explicitly omits
// "app" is rejected for app-originated calls.
func TestVisibilityAllowsApp(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{name: "no meta defaults to app-callable", meta: nil, want: true},
		{name: "empty ui defaults to app-callable", meta: map[string]any{"ui": map[string]any{}}, want: true},
		{name: "model and app list", meta: map[string]any{"ui": map[string]any{"visibility": []any{"model", "app"}}}, want: true},
		{name: "app-only string", meta: map[string]any{"ui": map[string]any{"visibility": "app"}}, want: true},
		{name: "app-only list", meta: map[string]any{"ui": map[string]any{"visibility": []any{"app"}}}, want: true},
		{name: "model-only is rejected", meta: map[string]any{"ui": map[string]any{"visibility": []any{"model"}}}, want: false},
		{name: "model-only string is rejected", meta: map[string]any{"ui": map[string]any{"visibility": "model"}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := visibilityAllowsApp(tt.meta); got != tt.want {
				t.Errorf("visibilityAllowsApp(%v) = %v, want %v", tt.meta, got, tt.want)
			}
		})
	}
}

// TestResolveMCPServerEndpoint pins the dual-CRD resolution: groupKind selects
// which CRD to read (so a RemoteMCPServer and MCPServer sharing a namespace/name
// resolve deterministically to the one the caller selected), a kmcp MCPServer is
// converted to the in-cluster Service URL, and a missing ref returns a clear
// error. An empty groupKind keeps the legacy RemoteMCPServer-first fallback.
func TestResolveMCPServerEndpoint(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("add v1alpha2 to scheme: %v", err)
	}
	if err := kmcp.AddToScheme(scheme); err != nil {
		t.Fatalf("add kmcp to scheme: %v", err)
	}

	remote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "default"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "https://example.com/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}
	mcpServer := &kmcp.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"},
	}
	mcpServer.Spec.Deployment.Port = 8080

	// Same namespace/name registered as both CRD kinds, to prove groupKind
	// disambiguates them.
	collideRemote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "clash"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "https://remote.example.com/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}
	collideMCP := &kmcp.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "clash"},
	}
	collideMCP.Spec.Deployment.Port = 9090

	tests := []struct {
		name      string
		objects   []client.Object
		namespace string
		server    string
		groupKind string
		wantURL   string
		wantErr   string
	}{
		{
			name:      "RemoteMCPServer used directly",
			objects:   []client.Object{remote},
			namespace: "default",
			server:    "remote",
			groupKind: "RemoteMCPServer.kagent.dev",
			wantURL:   "https://example.com/mcp",
		},
		{
			name:      "kmcp MCPServer converted to service URL",
			objects:   []client.Object{mcpServer},
			namespace: "team",
			server:    "local",
			groupKind: "MCPServer.kagent.dev",
			wantURL:   "http://local.team:8080/mcp",
		},
		{
			name:      "empty groupKind falls back to RemoteMCPServer first",
			objects:   []client.Object{remote},
			namespace: "default",
			server:    "remote",
			wantURL:   "https://example.com/mcp",
		},
		{
			name:      "empty groupKind falls back to MCPServer when no RemoteMCPServer",
			objects:   []client.Object{mcpServer},
			namespace: "team",
			server:    "local",
			wantURL:   "http://local.team:8080/mcp",
		},
		{
			name:      "collision resolves to MCPServer when kind is MCPServer",
			objects:   []client.Object{collideRemote, collideMCP},
			namespace: "clash",
			server:    "shared",
			groupKind: "MCPServer.kagent.dev",
			wantURL:   "http://shared.clash:9090/mcp",
		},
		{
			name:      "collision resolves to RemoteMCPServer when kind is RemoteMCPServer",
			objects:   []client.Object{collideRemote, collideMCP},
			namespace: "clash",
			server:    "shared",
			groupKind: "RemoteMCPServer.kagent.dev",
			wantURL:   "https://remote.example.com/mcp",
		},
		{
			name:      "kind without group suffix still resolves",
			objects:   []client.Object{collideRemote, collideMCP},
			namespace: "clash",
			server:    "shared",
			groupKind: "MCPServer",
			wantURL:   "http://shared.clash:9090/mcp",
		},
		{
			name:      "explicit RemoteMCPServer kind but only MCPServer exists",
			objects:   []client.Object{mcpServer},
			namespace: "team",
			server:    "local",
			groupKind: "RemoteMCPServer.kagent.dev",
			wantErr:   "no RemoteMCPServer team/local found",
		},
		{
			name:      "neither CRD exists",
			objects:   nil,
			namespace: "default",
			server:    "missing",
			wantErr:   "no RemoteMCPServer or MCPServer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			h := &MCPAppsHandler{Base: &Base{KubeClient: kubeClient}}

			got, err := h.resolveMCPServerEndpoint(context.Background(), tt.namespace, tt.server, tt.groupKind)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveMCPServerEndpoint() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMCPServerEndpoint() unexpected error: %v", err)
			}
			if got.Spec.URL != tt.wantURL {
				t.Errorf("resolveMCPServerEndpoint() URL = %q, want %q", got.Spec.URL, tt.wantURL)
			}
		})
	}
}
