/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mcp

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha2.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

func setupToolServerTestHandler(t *testing.T, objects ...client.Object) *MCPHandler {
	t.Helper()
	kubeClient := fake.NewClientBuilder().
		WithScheme(setupTestScheme()).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha2.RemoteMCPServer{}, &v1alpha1.MCPServer{}).
		Build()

	return &MCPHandler{
		kubeClient: kubeClient,
	}
}

// --- parseServerRef tests ---

func TestParseServerRef(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantKind  string
		wantNS    string
		wantName  string
		wantError string
	}{
		{
			name:     "valid RemoteMCPServer ref",
			ref:      "RemoteMCPServer/default/my-server",
			wantKind: "RemoteMCPServer",
			wantNS:   "default",
			wantName: "my-server",
		},
		{
			name:     "valid Service ref",
			ref:      "Service/tools/prometheus-mcp",
			wantKind: "Service",
			wantNS:   "tools",
			wantName: "prometheus-mcp",
		},
		{
			name:     "valid MCPServer ref",
			ref:      "MCPServer/default/weather",
			wantKind: "MCPServer",
			wantNS:   "default",
			wantName: "weather",
		},
		{
			name:      "missing kind - two parts only",
			ref:       "default/my-server",
			wantError: "invalid server reference",
		},
		{
			name:      "no slashes",
			ref:       "just-a-name",
			wantError: "invalid server reference",
		},
		{
			name:      "empty parts",
			ref:       "RemoteMCPServer//",
			wantError: "invalid server reference",
		},
		{
			name:      "unknown kind",
			ref:       "Deployment/default/my-deploy",
			wantError: "unknown server kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, ns, name, err := parseServerRef(tt.ref)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, kind)
			assert.Equal(t, tt.wantNS, ns)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

// --- resolveToRemoteMCPServer tests ---

func TestResolveToRemoteMCPServer(t *testing.T) {
	remoteMCP := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-remote",
			Namespace: "default",
		},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      "http://my-remote.default:8080/mcp",
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-svc",
			Namespace: "tools",
			Labels:    map[string]string{"kagent.dev/mcp-service": "true"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: 9090},
			},
		},
	}

	mcpServer := &v1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "weather",
			Namespace: "default",
		},
		Spec: v1alpha1.MCPServerSpec{
			TransportType: v1alpha1.TransportTypeStdio,
			Deployment: v1alpha1.MCPServerDeployment{
				Port: 3000,
			},
		},
	}

	handler := setupToolServerTestHandler(t, remoteMCP, svc, mcpServer)
	ctx := context.Background()

	t.Run("resolves RemoteMCPServer", func(t *testing.T) {
		result, err := handler.resolveToRemoteMCPServer(ctx, "RemoteMCPServer", "default", "my-remote")
		require.NoError(t, err)
		assert.Equal(t, "http://my-remote.default:8080/mcp", result.Spec.URL)
	})

	t.Run("resolves Service", func(t *testing.T) {
		result, err := handler.resolveToRemoteMCPServer(ctx, "Service", "tools", "my-svc")
		require.NoError(t, err)
		assert.Contains(t, result.Spec.URL, "my-svc.tools")
		assert.Contains(t, result.Spec.URL, "9090")
	})

	t.Run("resolves MCPServer", func(t *testing.T) {
		result, err := handler.resolveToRemoteMCPServer(ctx, "MCPServer", "default", "weather")
		require.NoError(t, err)
		assert.Contains(t, result.Spec.URL, "weather.default")
		assert.Contains(t, result.Spec.URL, "3000")
	})

	t.Run("RemoteMCPServer not found", func(t *testing.T) {
		_, err := handler.resolveToRemoteMCPServer(ctx, "RemoteMCPServer", "default", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Service not found", func(t *testing.T) {
		_, err := handler.resolveToRemoteMCPServer(ctx, "Service", "default", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("MCPServer not found", func(t *testing.T) {
		_, err := handler.resolveToRemoteMCPServer(ctx, "MCPServer", "default", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// --- handleListToolServers tests ---

func TestHandleListToolServers(t *testing.T) {
	tests := []struct {
		name          string
		objects       []client.Object
		input         ListToolServersInput
		expectedCount int
		checkFunc     func(t *testing.T, output ListToolServersOutput)
	}{
		{
			name:          "empty cluster returns empty list",
			objects:       nil,
			input:         ListToolServersInput{},
			expectedCount: 0,
		},
		{
			name: "returns RemoteMCPServers",
			objects: []client.Object{
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote-1", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL:      "http://remote-1.default:8080/mcp",
						Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
					},
				},
			},
			input:         ListToolServersInput{},
			expectedCount: 1,
			checkFunc: func(t *testing.T, output ListToolServersOutput) {
				assert.Equal(t, "RemoteMCPServer/default/remote-1", output.Servers[0].Ref)
				assert.Equal(t, "RemoteMCPServer", output.Servers[0].Kind)
				assert.Equal(t, "http://remote-1.default:8080/mcp", output.Servers[0].URL)
			},
		},
		{
			name: "returns Services with MCP label",
			objects: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcp-svc",
						Namespace: "tools",
						Labels:    map[string]string{"kagent.dev/mcp-service": "true"},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 9090}},
					},
				},
				// Service without MCP label should be excluded
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "regular-svc",
						Namespace: "tools",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				},
			},
			input:         ListToolServersInput{},
			expectedCount: 1,
			checkFunc: func(t *testing.T, output ListToolServersOutput) {
				assert.Equal(t, "Service/tools/mcp-svc", output.Servers[0].Ref)
				assert.Equal(t, "Service", output.Servers[0].Kind)
				assert.Contains(t, output.Servers[0].URL, "9090")
			},
		},
		{
			name: "returns MCPServers with status",
			objects: []client.Object{
				&v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "weather", Namespace: "default"},
					Spec: v1alpha1.MCPServerSpec{
						TransportType: v1alpha1.TransportTypeStdio,
						Deployment:    v1alpha1.MCPServerDeployment{Port: 3000},
					},
					Status: v1alpha1.MCPServerStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(v1alpha1.MCPServerConditionReady),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			input:         ListToolServersInput{},
			expectedCount: 1,
			checkFunc: func(t *testing.T, output ListToolServersOutput) {
				assert.Equal(t, "MCPServer/default/weather", output.Servers[0].Ref)
				assert.Equal(t, "MCPServer", output.Servers[0].Kind)
				assert.Equal(t, "Ready", output.Servers[0].Status)
			},
		},
		{
			name: "returns all types combined",
			objects: []client.Object{
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote-1", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL:      "http://remote-1.default:8080/mcp",
						Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcp-svc",
						Namespace: "tools",
						Labels:    map[string]string{"kagent.dev/mcp-service": "true"},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 9090}},
					},
				},
				&v1alpha1.MCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "weather", Namespace: "default"},
					Spec: v1alpha1.MCPServerSpec{
						TransportType: v1alpha1.TransportTypeStdio,
						Deployment:    v1alpha1.MCPServerDeployment{Port: 3000},
					},
				},
			},
			input:         ListToolServersInput{},
			expectedCount: 3,
			checkFunc: func(t *testing.T, output ListToolServersOutput) {
				kinds := make(map[string]bool)
				for _, s := range output.Servers {
					kinds[s.Kind] = true
				}
				assert.True(t, kinds["RemoteMCPServer"])
				assert.True(t, kinds["Service"])
				assert.True(t, kinds["MCPServer"])
			},
		},
		{
			name: "filters by namespace",
			objects: []client.Object{
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote-1", Namespace: "default"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL:      "http://remote-1.default:8080/mcp",
						Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
					},
				},
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote-2", Namespace: "tools"},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL:      "http://remote-2.tools:8080/mcp",
						Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
					},
				},
			},
			input:         ListToolServersInput{Namespace: "tools"},
			expectedCount: 1,
			checkFunc: func(t *testing.T, output ListToolServersOutput) {
				assert.Equal(t, "RemoteMCPServer/tools/remote-2", output.Servers[0].Ref)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupToolServerTestHandler(t, tt.objects...)
			ctx := context.Background()

			result, output, err := handler.handleListToolServers(ctx, &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err)
			assert.False(t, result.IsError)
			assert.Len(t, output.Servers, tt.expectedCount)

			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

// --- handleListTools validation tests ---

func TestHandleListToolsValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     ListToolsInput
		wantError string
	}{
		{
			name:      "invalid ref format - two parts only",
			input:     ListToolsInput{Server: "default/my-server"},
			wantError: "invalid server reference",
		},
		{
			name:      "invalid ref format - no slashes",
			input:     ListToolsInput{Server: "just-a-name"},
			wantError: "invalid server reference",
		},
		{
			name:      "server not found",
			input:     ListToolsInput{Server: "RemoteMCPServer/default/nonexistent"},
			wantError: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupToolServerTestHandler(t)
			ctx := context.Background()

			result, _, err := handler.handleListTools(ctx, &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err) // protocol-level error should not occur
			assert.True(t, result.IsError)

			for _, content := range result.Content {
				if textContent, ok := content.(*mcpsdk.TextContent); ok {
					assert.Contains(t, textContent.Text, tt.wantError)
				}
			}
		})
	}
}

// --- handleCallTool validation tests ---

func TestHandleCallToolValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     CallToolInput
		wantError string
	}{
		{
			name:      "missing tool name",
			input:     CallToolInput{Server: "RemoteMCPServer/default/test", Tool: ""},
			wantError: "tool name is required",
		},
		{
			name:      "invalid ref format",
			input:     CallToolInput{Server: "bad-ref", Tool: "some_tool"},
			wantError: "invalid server reference",
		},
		{
			name:      "server not found",
			input:     CallToolInput{Server: "RemoteMCPServer/default/nonexistent", Tool: "some_tool"},
			wantError: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := setupToolServerTestHandler(t)
			ctx := context.Background()

			result, _, err := handler.handleCallTool(ctx, &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err) // protocol-level error should not occur
			assert.True(t, result.IsError)

			for _, content := range result.Content {
				if textContent, ok := content.(*mcpsdk.TextContent); ok {
					assert.Contains(t, textContent.Text, tt.wantError)
				}
			}
		})
	}
}
