package tool

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
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
	remote := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "default"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			URL:      "https://example.com/mcp",
			Protocol: v1alpha3.RemoteMCPServerProtocolStreamableHttp,
		},
	}
	collideRemote := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "clash"},
		Spec:       v1alpha3.RemoteMCPServerSpec{URL: "https://remote.example.com/mcp"},
	}
	tests := []struct {
		name      string
		objects   []client.Object
		ref       types.NamespacedName
		groupKind string
		wantURL   string
		wantError string
	}{
		{name: "remote selected", objects: []client.Object{remote}, ref: types.NamespacedName{Namespace: "default", Name: "remote"}, groupKind: "RemoteMCPServer.api.kagent.dev", wantURL: "https://example.com/mcp"},
		{name: "empty kind selects remote", objects: []client.Object{collideRemote}, ref: types.NamespacedName{Namespace: "clash", Name: "shared"}, wantURL: "https://remote.example.com/mcp"},
		{name: "explicit remote missing", ref: types.NamespacedName{Namespace: "team", Name: "missing"}, groupKind: "RemoteMCPServer.api.kagent.dev", wantError: "no RemoteMCPServer team/missing found"},
		{name: "unsupported kind", ref: types.NamespacedName{Namespace: "default", Name: "local"}, groupKind: "MCPServer.kagent.dev", wantError: "unsupported MCP server kind"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewRuntimeMCPClient(toolTestKube(t, test.objects...))
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

func TestResolveHeaders(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "team"},
		Data:       map[string][]byte{"token": []byte("secret-token")},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "team"},
		Data:       map[string]string{"tenant": "acme"},
	}

	t.Run("Given inline, Secret, and ConfigMap headers, when the server headers are resolved, assert that every value is loaded from the server namespace", func(t *testing.T) {
		kube := toolTestKube(t, secret, configMap)

		headers, err := resolveHeaders(t.Context(), kube, "team", []v1alpha3.ValueRef{
			{Name: "X-Static", Value: "static"},
			{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "creds", Key: "token"}},
			{Name: "X-Tenant", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "settings", Key: "tenant"}},
		})

		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"X-Static":      "static",
			"Authorization": "secret-token",
			"X-Tenant":      "acme",
		}, headers)
	})

	scenarios := []struct {
		name      string
		objects   []client.Object
		ref       v1alpha3.ValueRef
		wantError string
	}{
		{
			name:      "Given the Secret lives in another namespace, when a Secret header is resolved, assert that resolution fails",
			objects:   []client.Object{&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "other"}, Data: secret.Data}},
			ref:       v1alpha3.ValueRef{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "creds", Key: "token"}},
			wantError: "failed to get Secret team/creds",
		},
		{
			name:      "Given the Secret lacks the key, when a Secret header is resolved, assert that resolution fails",
			objects:   []client.Object{secret},
			ref:       v1alpha3.ValueRef{Name: "Authorization", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.SecretValueSource, Name: "creds", Key: "missing"}},
			wantError: "key missing not found in Secret team/creds",
		},
		{
			name:      "Given the ConfigMap lacks the key, when a ConfigMap header is resolved, assert that resolution fails",
			objects:   []client.Object{configMap},
			ref:       v1alpha3.ValueRef{Name: "X-Tenant", ValueFrom: &v1alpha3.ValueSource{Type: v1alpha3.ConfigMapValueSource, Name: "settings", Key: "missing"}},
			wantError: "key missing not found in ConfigMap team/settings",
		},
		{
			name:      "Given an unknown source type, when the header is resolved, assert that resolution fails",
			ref:       v1alpha3.ValueRef{Name: "X-Other", ValueFrom: &v1alpha3.ValueSource{Type: "Vault", Name: "x", Key: "y"}},
			wantError: `unknown value source type "Vault"`,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			kube := toolTestKube(t, scenario.objects...)

			headers, err := resolveHeaders(t.Context(), kube, "team", []v1alpha3.ValueRef{scenario.ref})

			require.ErrorContains(t, err, scenario.wantError)
			assert.Nil(t, headers)
		})
	}
}
