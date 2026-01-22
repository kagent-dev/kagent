package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/internal/controller/translator/agent"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
)

func TestConvertMCPServerToRemoteMCPServer(t *testing.T) {
	tests := []struct {
		name      string
		mcpServer *v1alpha1.MCPServer
		want      *v1alpha2.RemoteMCPServerSpec
		wantErr   bool
		errMsg    string
	}{
		{
			name: "basic conversion without TLS",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp",
					Namespace: "default",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 8080,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp.default:8080/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS:      nil,
			},
			wantErr: false,
		},
		{
			name: "conversion with TLS configuration",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-tls",
					Namespace: "production",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 8443,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{
						TLS: &v1alpha1.HTTPTransportTLS{
							SecretRef:          "tls-secret",
							InsecureSkipVerify: false,
						},
					},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp-tls.production:8443/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS: &v1alpha2.TLSConfig{
					CACertSecretRef: "tls-secret",
					CACertSecretKey: "ca.crt",
					DisableVerify:   false,
				},
			},
			wantErr: false,
		},
		{
			name: "conversion with TLS and insecure skip verify",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-insecure",
					Namespace: "dev",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 3000,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{
						TLS: &v1alpha1.HTTPTransportTLS{
							SecretRef:          "dev-tls-secret",
							InsecureSkipVerify: true,
						},
					},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp-insecure.dev:3000/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS: &v1alpha2.TLSConfig{
					CACertSecretRef: "dev-tls-secret",
					CACertSecretKey: "ca.crt",
					DisableVerify:   true,
				},
			},
			wantErr: false,
		},
		{
			name: "conversion with HTTP transport but no TLS",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-no-tls",
					Namespace: "default",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 8080,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{
						TLS: nil,
					},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp-no-tls.default:8080/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS:      nil,
			},
			wantErr: false,
		},
		{
			name: "conversion with stdio transport (no TLS)",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-stdio",
					Namespace: "default",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeStdio,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 3000,
					},
					StdioTransport: &v1alpha1.StdioTransport{},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp-stdio.default:3000/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS:      nil,
			},
			wantErr: false,
		},
		{
			name: "error when port is zero",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-no-port",
					Namespace: "default",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 0,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{},
				},
			},
			want:    nil,
			wantErr: true,
			errMsg:  "cannot determine port for MCP server test-mcp-no-port",
		},
		{
			name: "TLS with empty secret ref",
			mcpServer: &v1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mcp-empty-secret",
					Namespace: "default",
				},
				Spec: v1alpha1.MCPServerSpec{
					TransportType: v1alpha1.TransportTypeHTTP,
					Deployment: v1alpha1.MCPServerDeployment{
						Port: 8080,
					},
					HTTPTransport: &v1alpha1.HTTPTransport{
						TLS: &v1alpha1.HTTPTransportTLS{
							SecretRef:          "",
							InsecureSkipVerify: false,
						},
					},
				},
			},
			want: &v1alpha2.RemoteMCPServerSpec{
				URL:      "http://test-mcp-empty-secret.default:8080/mcp",
				Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
				TLS: &v1alpha2.TLSConfig{
					CACertSecretRef: "",
					CACertSecretKey: "ca.crt",
					DisableVerify:   false,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := agenttranslator.ConvertMCPServerToRemoteMCPServer(tt.mcpServer)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
				assert.Equal(t, tt.want.URL, got.URL, "URL should match")
				assert.Equal(t, tt.want.Protocol, got.Protocol, "Protocol should match")

				if tt.want.TLS == nil {
					assert.Nil(t, got.TLS, "TLS should be nil")
				} else {
					require.NotNil(t, got.TLS, "TLS should not be nil")
					assert.Equal(t, tt.want.TLS.CACertSecretRef, got.TLS.CACertSecretRef, "TLS CACertSecretRef should match")
					assert.Equal(t, tt.want.TLS.CACertSecretKey, got.TLS.CACertSecretKey, "TLS CACertSecretKey should match")
					assert.Equal(t, tt.want.TLS.DisableVerify, got.TLS.DisableVerify, "TLS DisableVerify should match")
				}
			}
		})
	}
}
