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

// TestResolveRemoteMCPServer pins the dual-CRD resolution: a RemoteMCPServer is
// used as-is, a kmcp MCPServer is converted to the in-cluster Service URL, and a
// missing ref returns a clear error instead of leaking RemoteMCPServer specifics.
func TestResolveRemoteMCPServer(t *testing.T) {
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

	tests := []struct {
		name      string
		objects   []client.Object
		namespace string
		server    string
		wantURL   string
		wantErr   string
	}{
		{
			name:      "RemoteMCPServer used directly",
			objects:   []client.Object{remote},
			namespace: "default",
			server:    "remote",
			wantURL:   "https://example.com/mcp",
		},
		{
			name:      "falls back to kmcp MCPServer service URL",
			objects:   []client.Object{mcpServer},
			namespace: "team",
			server:    "local",
			wantURL:   "http://local.team:8080/mcp",
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

			got, err := h.resolveRemoteMCPServer(context.Background(), tt.namespace, tt.server)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveRemoteMCPServer() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRemoteMCPServer() unexpected error: %v", err)
			}
			if got.Spec.URL != tt.wantURL {
				t.Errorf("resolveRemoteMCPServer() URL = %q, want %q", got.Spec.URL, tt.wantURL)
			}
		})
	}
}
