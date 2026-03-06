package reconciler

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	fake "github.com/kagent-dev/kagent/go/core/internal/database/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeriveBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "URL with path",
			input: "http://kanban-mcp.kagent.svc:8080/mcp",
			want:  "http://kanban-mcp.kagent.svc:8080",
		},
		{
			name:  "URL without path",
			input: "http://kanban-mcp.kagent.svc:8080",
			want:  "http://kanban-mcp.kagent.svc:8080",
		},
		{
			name:  "URL with deep path",
			input: "http://host:9090/path/to/mcp",
			want:  "http://host:9090",
		},
		{
			name:  "URL with query and fragment",
			input: "http://host:8080/mcp?key=val#frag",
			want:  "http://host:8080",
		},
		{
			name:  "HTTPS URL",
			input: "https://secure-mcp.example.com/mcp",
			want:  "https://secure-mcp.example.com",
		},
		{
			name:    "invalid URL",
			input:   "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveBaseURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("deriveBaseURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("deriveBaseURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestReconciler(t *testing.T) (*kagentReconciler, *fake.InMemoryFakeClient) {
	t.Helper()
	dbClient := fake.NewClient()
	fakeClient := dbClient.(*fake.InMemoryFakeClient)
	r := &kagentReconciler{dbClient: dbClient}
	return r, fakeClient
}

func makeRemoteMCPServer(namespace, name, url string, ui *v1alpha2.PluginUISpec) *v1alpha2.RemoteMCPServer {
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL: url,
			UI:  ui,
		},
	}
}

func TestReconcilePluginUI_CreateWithAllFields(t *testing.T) {
	r, fakeClient := newTestReconciler(t)

	server := makeRemoteMCPServer("kagent", "kanban-mcp", "http://kanban-mcp:8080/mcp", &v1alpha2.PluginUISpec{
		Enabled:     true,
		PathPrefix:  "kanban",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
	})

	err := r.reconcilePluginUI(server)
	if err != nil {
		t.Fatalf("reconcilePluginUI() error = %v", err)
	}

	plugins, _ := fakeClient.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}

	p := plugins[0]
	if p.Name != "kagent/kanban-mcp" {
		t.Errorf("Name = %q, want %q", p.Name, "kagent/kanban-mcp")
	}
	if p.PathPrefix != "kanban" {
		t.Errorf("PathPrefix = %q, want %q", p.PathPrefix, "kanban")
	}
	if p.DisplayName != "Kanban Board" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "Kanban Board")
	}
	if p.Icon != "kanban" {
		t.Errorf("Icon = %q, want %q", p.Icon, "kanban")
	}
	if p.Section != "AGENTS" {
		t.Errorf("Section = %q, want %q", p.Section, "AGENTS")
	}
	if p.UpstreamURL != "http://kanban-mcp:8080" {
		t.Errorf("UpstreamURL = %q, want %q", p.UpstreamURL, "http://kanban-mcp:8080")
	}
}

func TestReconcilePluginUI_DefaultValues(t *testing.T) {
	r, fakeClient := newTestReconciler(t)

	server := makeRemoteMCPServer("default", "my-plugin", "http://my-plugin:9090/api", &v1alpha2.PluginUISpec{
		Enabled: true,
	})

	err := r.reconcilePluginUI(server)
	if err != nil {
		t.Fatalf("reconcilePluginUI() error = %v", err)
	}

	plugins, _ := fakeClient.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}

	p := plugins[0]
	if p.PathPrefix != "my-plugin" {
		t.Errorf("PathPrefix default = %q, want %q", p.PathPrefix, "my-plugin")
	}
	if p.DisplayName != "my-plugin" {
		t.Errorf("DisplayName default = %q, want %q", p.DisplayName, "my-plugin")
	}
	if p.Icon != "puzzle" {
		t.Errorf("Icon default = %q, want %q", p.Icon, "puzzle")
	}
	if p.Section != "PLUGINS" {
		t.Errorf("Section default = %q, want %q", p.Section, "PLUGINS")
	}
}

func TestReconcilePluginUI_DeleteWhenDisabled(t *testing.T) {
	r, fakeClient := newTestReconciler(t)

	// First create
	server := makeRemoteMCPServer("kagent", "kanban-mcp", "http://kanban-mcp:8080/mcp", &v1alpha2.PluginUISpec{
		Enabled:    true,
		PathPrefix: "kanban",
	})
	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("create error = %v", err)
	}

	plugins, _ := fakeClient.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin after create, got %d", len(plugins))
	}

	// Disable
	server.Spec.UI.Enabled = false
	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("disable error = %v", err)
	}

	plugins, _ = fakeClient.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins after disable, got %d", len(plugins))
	}
}

func TestReconcilePluginUI_DeleteWhenUIIsNil(t *testing.T) {
	r, fakeClient := newTestReconciler(t)

	// First create
	server := makeRemoteMCPServer("kagent", "kanban-mcp", "http://kanban-mcp:8080/mcp", &v1alpha2.PluginUISpec{
		Enabled:    true,
		PathPrefix: "kanban",
	})
	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("create error = %v", err)
	}

	// Remove UI spec entirely
	server.Spec.UI = nil
	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("nil UI error = %v", err)
	}

	plugins, _ := fakeClient.ListPlugins()
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins after nil UI, got %d", len(plugins))
	}
}

func TestReconcilePluginUI_Update(t *testing.T) {
	r, fakeClient := newTestReconciler(t)

	server := makeRemoteMCPServer("kagent", "kanban-mcp", "http://kanban-mcp:8080/mcp", &v1alpha2.PluginUISpec{
		Enabled:     true,
		PathPrefix:  "kanban",
		DisplayName: "Kanban Board",
		Icon:        "kanban",
		Section:     "AGENTS",
	})

	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("create error = %v", err)
	}

	// Update display name and icon
	server.Spec.UI.DisplayName = "Updated Board"
	server.Spec.UI.Icon = "layout-kanban"
	if err := r.reconcilePluginUI(server); err != nil {
		t.Fatalf("update error = %v", err)
	}

	plugins, _ := fakeClient.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin after update, got %d", len(plugins))
	}

	p := plugins[0]
	if p.DisplayName != "Updated Board" {
		t.Errorf("DisplayName after update = %q, want %q", p.DisplayName, "Updated Board")
	}
	if p.Icon != "layout-kanban" {
		t.Errorf("Icon after update = %q, want %q", p.Icon, "layout-kanban")
	}
}
