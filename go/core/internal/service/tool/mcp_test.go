package tool

import (
	"strings"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcp "github.com/kagent-dev/kmcp/api/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, visibilityAllowsApp(test.meta))
		})
	}
}

func TestValidateMCPAppResource(t *testing.T) {
	tests := []struct {
		name      string
		result    *mcp.ReadResourceResult
		wantError string
	}{
		{name: "nil result", wantError: "no contents"},
		{name: "empty contents", result: &mcp.ReadResourceResult{}, wantError: "no contents"},
		{name: "nil content", result: &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{nil}}, wantError: "empty content"},
		{
			name: "wrong MIME type",
			result: &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
				URI:      "ui://board",
				MIMEType: "text/html",
				Text:     "<main>Board</main>",
			}}},
			wantError: `expected "text/html;profile=mcp-app"`,
		},
		{
			name: "valid MCP App HTML",
			result: &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
				URI:      "ui://board",
				MIMEType: mcpAppHTMLMimeType,
				Text:     "<main>Board</main>",
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMCPAppResource(test.result)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

func TestRuntimeMCPClientResolveServerMatrix(t *testing.T) {
	remote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "default"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "https://example.com/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}
	local := &kmcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "team"}}
	local.Spec.Deployment.Port = 8080
	collideRemote := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "clash"},
		Spec:       v1alpha2.RemoteMCPServerSpec{URL: "https://remote.example.com/mcp"},
	}
	collideLocal := &kmcp.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "clash"}}
	collideLocal.Spec.Deployment.Port = 9090

	tests := []struct {
		name      string
		objects   []client.Object
		ref       types.NamespacedName
		groupKind string
		wantURL   string
		wantError string
	}{
		{name: "remote selected", objects: []client.Object{remote}, ref: types.NamespacedName{Namespace: "default", Name: "remote"}, groupKind: "RemoteMCPServer.kagent.dev", wantURL: "https://example.com/mcp"},
		{name: "local selected", objects: []client.Object{local}, ref: types.NamespacedName{Namespace: "team", Name: "local"}, groupKind: "MCPServer.kagent.dev", wantURL: "http://local.team:8080/mcp"},
		{name: "empty kind prefers remote", objects: []client.Object{collideRemote, collideLocal}, ref: types.NamespacedName{Namespace: "clash", Name: "shared"}, wantURL: "https://remote.example.com/mcp"},
		{name: "empty kind falls back local", objects: []client.Object{local}, ref: types.NamespacedName{Namespace: "team", Name: "local"}, wantURL: "http://local.team:8080/mcp"},
		{name: "collision selects local", objects: []client.Object{collideRemote, collideLocal}, ref: types.NamespacedName{Namespace: "clash", Name: "shared"}, groupKind: "MCPServer.kagent.dev", wantURL: "http://shared.clash:9090/mcp"},
		{name: "kind without group suffix", objects: []client.Object{collideRemote, collideLocal}, ref: types.NamespacedName{Namespace: "clash", Name: "shared"}, groupKind: "MCPServer", wantURL: "http://shared.clash:9090/mcp"},
		{name: "explicit remote missing", objects: []client.Object{local}, ref: types.NamespacedName{Namespace: "team", Name: "local"}, groupKind: "RemoteMCPServer.kagent.dev", wantError: "no RemoteMCPServer team/local found"},
		{name: "both missing", ref: types.NamespacedName{Namespace: "default", Name: "missing"}, wantError: "no RemoteMCPServer or MCPServer"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewRuntimeMCPClient(toolTestKube(t, true, test.objects...))
			result, err := client.ResolveServer(t.Context(), MCPServerRef{Ref: test.ref, GroupKind: test.groupKind})
			if test.wantError != "" {
				require.Error(t, err)
				assert.True(t, strings.Contains(err.Error(), test.wantError), err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantURL, result.Spec.URL)
		})
	}
}
