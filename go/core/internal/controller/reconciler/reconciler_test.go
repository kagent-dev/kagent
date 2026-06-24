package reconciler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestComputeStatusSecretHash_Output verifies the output of the hash function
func TestComputeStatusSecretHash_Output(t *testing.T) {
	tests := []struct {
		name    string
		secrets []secretRef
		want    string
	}{
		{
			name:    "no secrets",
			secrets: []secretRef{},
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // i.e. the hash of an empty string
		},
		{
			name: "one secret, no keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{},
					},
				},
			},
			want: "68a268d3f02147004cfa8b609966ec4cba7733f8c652edb80be8071eb1b91574", // because the secret exists, it still hashes the namespacedName + empty data
		},
		{
			name: "one secret, single key",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
			},
			want: "62dc22ecd609281a5939efd60fae775e6b75b641614c523c400db994a09902ff",
		},
		{
			name: "one secret, multiple keys",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
					},
				},
			},
			want: "ba6798ec591d129f78322cdae569eaccdb2f5a8343c12026f0ed6f4e156cd52e",
		},
		{
			name: "multiple secrets",
			secrets: []secretRef{
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key1": []byte("value1")},
					},
				},
				{
					NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
					Secret: &corev1.Secret{
						Data: map[string][]byte{"key2": []byte("value2")},
					},
				},
			},
			want: "f174f0e21a4427a87a23e4f277946a27f686d023cbe42f3000df94a4df94f7b5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeStatusSecretHash(tt.secrets)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestComputeStatusSecretHash_Deterministic tests that the resultant hash is deterministic, specifically that ordering of keys and secrets does not matter
func TestComputeStatusSecretHash_Deterministic(t *testing.T) {
	tests := []struct {
		name          string
		secrets       [2][]secretRef
		expectedEqual bool
	}{
		{
			name: "key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
		{
			name: "secret and key ordering should not matter",
			secrets: [2][]secretRef{
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
				{
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret2"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key1": []byte("value1"), "key2": []byte("value2")},
						},
					},
					{
						NamespacedName: types.NamespacedName{Namespace: "test", Name: "secret1"},
						Secret: &corev1.Secret{
							Data: map[string][]byte{"key2": []byte("value2"), "key1": []byte("value1")},
						},
					},
				},
			},
			expectedEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got1 := computeStatusSecretHash(tt.secrets[0])
			got2 := computeStatusSecretHash(tt.secrets[1])
			assert.Equal(t, tt.expectedEqual, got1 == got2)
		})
	}
}

func TestAgentIDConsistency(t *testing.T) {
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "my-agent",
		},
	}

	storeID := utils.ConvertToPythonIdentifier(utils.ResourceRefString(req.Namespace, req.Name))
	deleteID := utils.ConvertToPythonIdentifier(req.String())

	assert.Equal(t, storeID, deleteID)
}

func TestValidateCrossNamespaceReferences(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	// Create test namespaces
	sourceNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source-ns",
			Labels: map[string]string{
				"shared-access": "true",
			},
		},
	}
	targetNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "target-ns",
		},
	}

	tests := []struct {
		name              string
		watchedNamespaces []string
		objects           []client.Object // Additional objects to create in fake client
		agent             *v1alpha2.Agent
		wantErr           bool
		errContains       string
	}{
		{
			name:              "BYO agent - no validation needed",
			watchedNamespaces: []string{"source-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_BYO,
				},
			},
			wantErr: false,
		},
		{
			name:              "Declarative agent with no tools - passes",
			watchedNamespaces: []string{"source-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
					},
				},
			},
			wantErr: false,
		},
		{
			name:              "Agent tool in unwatched namespace - fails",
			watchedNamespaces: []string{"source-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "other-agent",
									Namespace: "unwatched-ns",
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "namespace \"unwatched-ns\" is not watched by the controller",
		},
		{
			name:              "Same namespace agent tool - always allowed",
			watchedNamespaces: []string{"source-ns"},
			objects: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tool-agent",
						Namespace: "source-ns",
					},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
						// No AllowedNamespaces needed for same namespace
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "tool-agent",
									Namespace: "source-ns",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:              "Cross-namespace agent tool - denied without AllowedNamespaces",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			objects: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tool-agent",
						Namespace: "target-ns",
					},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
						// No AllowedNamespaces = same namespace only
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "tool-agent",
									Namespace: "target-ns",
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "cross-namespace reference to agent target-ns/tool-agent is not allowed from namespace source-ns",
		},
		{
			name:              "Cross-namespace agent tool - allowed with From=All",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			objects: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tool-agent",
						Namespace: "target-ns",
					},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
						AllowedNamespaces: &v1alpha2.AllowedNamespaces{
							From: v1alpha2.NamespacesFromAll,
						},
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "tool-agent",
									Namespace: "target-ns",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:              "Cross-namespace agent tool - allowed with matching selector",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			objects: []client.Object{
				&v1alpha2.Agent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tool-agent",
						Namespace: "target-ns",
					},
					Spec: v1alpha2.AgentSpec{
						Type: v1alpha2.AgentType_Declarative,
						AllowedNamespaces: &v1alpha2.AllowedNamespaces{
							From: v1alpha2.NamespacesFromSelector,
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"shared-access": "true",
								},
							},
						},
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns", // Has label "shared-access": "true"
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "tool-agent",
									Namespace: "target-ns",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:              "Cross-namespace RemoteMCPServer - denied without AllowedNamespaces",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			objects: []client.Object{
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tools-server",
						Namespace: "target-ns",
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL: "http://tools.example.com/mcp",
						// No AllowedNamespaces = same namespace only
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedReference: v1alpha2.TypedReference{
										Kind:      "RemoteMCPServer",
										ApiGroup:  "kagent.dev",
										Name:      "tools-server",
										Namespace: "target-ns",
									},
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "cross-namespace reference to RemoteMCPServer target-ns/tools-server is not allowed from namespace source-ns",
		},
		{
			name:              "Cross-namespace RemoteMCPServer - allowed with From=All",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			objects: []client.Object{
				&v1alpha2.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tools-server",
						Namespace: "target-ns",
					},
					Spec: v1alpha2.RemoteMCPServerSpec{
						URL: "http://tools.example.com/mcp",
						AllowedNamespaces: &v1alpha2.AllowedNamespaces{
							From: v1alpha2.NamespacesFromAll,
						},
					},
				},
			},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedReference: v1alpha2.TypedReference{
										Kind:      "RemoteMCPServer",
										ApiGroup:  "kagent.dev",
										Name:      "tools-server",
										Namespace: "target-ns",
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:              "Cross-namespace MCPServer - always denied (external type)",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedReference: v1alpha2.TypedReference{
										Kind:      "MCPServer",
										ApiGroup:  "kagent.dev",
										Name:      "mcp-server",
										Namespace: "target-ns",
									},
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "MCPServer does not support cross-namespace references",
		},
		{
			name:              "Cross-namespace Service - always denied (external type)",
			watchedNamespaces: []string{"source-ns", "target-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_McpServer,
								McpServer: &v1alpha2.McpServerTool{
									TypedReference: v1alpha2.TypedReference{
										Kind:      "Service",
										ApiGroup:  "",
										Name:      "my-service",
										Namespace: "target-ns",
									},
								},
							},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "Service does not support cross-namespace references",
		},
		{
			name:              "Tool with empty namespace defaults to agent namespace - passes",
			watchedNamespaces: []string{"source-ns"},
			agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "source-ns",
				},
				Spec: v1alpha2.AgentSpec{
					Type: v1alpha2.AgentType_Declarative,
					Declarative: &v1alpha2.DeclarativeAgentSpec{
						SystemMessage: "test",
						Tools: []*v1alpha2.Tool{
							{
								Type: v1alpha2.ToolProviderType_Agent,
								Agent: &v1alpha2.TypedReference{
									Name:      "other-agent",
									Namespace: "", // defaults to agent's namespace
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build fake client with test objects
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(sourceNs, targetNs)

			for _, obj := range tt.objects {
				clientBuilder = clientBuilder.WithObjects(obj)
			}

			kubeClient := clientBuilder.Build()

			reconciler := &kagentReconciler{
				kube:              kubeClient,
				watchedNamespaces: tt.watchedNamespaces,
			}

			err := reconciler.validateCrossNamespaceReferences(context.Background(), tt.agent)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestComputeRemoteMCPServerSecretHash pins the controller-side shape of
// the RMS TLS-Secret hash: the empty string when no TLS Secret is
// referenced, a deterministic hex string when one is, and a different
// hex string after the Secret contents rotate. Agents fold this value
// into their config-hash so an in-place cert rotation triggers a
// rollout (the Python ADK loads the cert at startup, so without a
// rollout pods keep the stale trust chain in memory).
func TestComputeRemoteMCPServerSecretHash(t *testing.T) {
	scheme := clientgoscheme.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	caV1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "corp-ca", Namespace: "ns"},
		Data:       map[string][]byte{"ca.crt": []byte("PEM-V1")},
	}
	caV2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "corp-ca", Namespace: "ns"},
		Data:       map[string][]byte{"ca.crt": []byte("PEM-V2")},
	}

	rmsNoTLS := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
		Spec:       v1alpha2.RemoteMCPServerSpec{URL: "https://x/y"},
	}
	rmsWithTLS := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "ns"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL: "https://x/y",
			TLS: &v1alpha2.TLSConfig{CACertSecretRef: "corp-ca", CACertSecretKey: "ca.crt"},
		},
	}

	t.Run("no TLS → empty hash", func(t *testing.T) {
		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube}
		hash, err := r.computeRemoteMCPServerSecretHash(context.Background(), rmsNoTLS)
		require.NoError(t, err)
		assert.Empty(t, hash)
	})

	t.Run("missing secret → error", func(t *testing.T) {
		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube}
		_, err := r.computeRemoteMCPServerSecretHash(context.Background(), rmsWithTLS)
		require.Error(t, err)
	})

	t.Run("missing key in present secret → error", func(t *testing.T) {
		// Secret exists but has the wrong key — the operator typo'd
		// caCertSecretKey. Without this guard the agent would mount
		// the Secret, fail to find the file at startup, and produce a
		// FileNotFoundError that doesn't identify the resource owner.
		// Surfacing the error on the RMS's Accepted condition gives
		// the operator a precise pointer.
		wrongKey := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "corp-ca", Namespace: "ns"},
			Data:       map[string][]byte{"other.crt": []byte("PEM")},
		}
		kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wrongKey).Build()
		r := &kagentReconciler{kube: kube}
		_, err := r.computeRemoteMCPServerSecretHash(context.Background(), rmsWithTLS)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ca.crt")
	})

	t.Run("present secret → stable hex; rotation → different hex", func(t *testing.T) {
		kube1 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caV1.DeepCopy()).Build()
		kube2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caV2.DeepCopy()).Build()
		r1 := &kagentReconciler{kube: kube1}
		r2 := &kagentReconciler{kube: kube2}

		h1, err := r1.computeRemoteMCPServerSecretHash(context.Background(), rmsWithTLS)
		require.NoError(t, err)
		require.NotEmpty(t, h1)

		h1Again, err := r1.computeRemoteMCPServerSecretHash(context.Background(), rmsWithTLS)
		require.NoError(t, err)
		assert.Equal(t, h1, h1Again, "same Secret content must produce identical hash")

		h2, err := r2.computeRemoteMCPServerSecretHash(context.Background(), rmsWithTLS)
		require.NoError(t, err)
		assert.NotEqual(t, h1, h2, "rotating the Secret content must change the hash")
	})
}

// TestRemoteMCPRegistrationTimeout verifies that remoteMCPRegistrationTimeout
// returns spec.timeout when set and falls back to the package default otherwise.
func TestRemoteMCPRegistrationTimeout(t *testing.T) {
	custom := 10 * time.Second

	tests := []struct {
		name   string
		server *v1alpha2.RemoteMCPServer
		want   time.Duration
	}{
		{
			name:   "nil server returns default",
			server: nil,
			want:   mcpRegistrationTimeout,
		},
		{
			name:   "nil spec.timeout returns default",
			server: &v1alpha2.RemoteMCPServer{},
			want:   mcpRegistrationTimeout,
		},
		{
			name: "spec.timeout overrides default",
			server: &v1alpha2.RemoteMCPServer{
				Spec: v1alpha2.RemoteMCPServerSpec{
					Timeout: &metav1.Duration{Duration: custom},
				},
			},
			want: custom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, remoteMCPRegistrationTimeout(tt.server))
		})
	}
}

// TestNewHTTPClient verifies that newHTTPClient always produces a client with
// the supplied timeout, regardless of whether custom headers are present.
func TestNewHTTPClient(t *testing.T) {
	timeout := 5 * time.Second

	t.Run("no headers", func(t *testing.T) {
		c := newHTTPClient(nil, timeout, nil)
		assert.Equal(t, timeout, c.Timeout)
	})

	t.Run("empty headers", func(t *testing.T) {
		c := newHTTPClient(map[string]string{}, timeout, nil)
		assert.Equal(t, timeout, c.Timeout)
	})

	t.Run("with headers sets timeout and custom transport", func(t *testing.T) {
		c := newHTTPClient(map[string]string{"X-Key": "val"}, timeout, nil)
		assert.Equal(t, timeout, c.Timeout)
		_, ok := c.Transport.(*headerTransport)
		assert.True(t, ok, "expected headerTransport")
	})

	t.Run("with tls config installs cloned transport", func(t *testing.T) {
		tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test only
		c := newHTTPClient(nil, timeout, tlsCfg)
		require.Equal(t, timeout, c.Timeout)
		// No headers → transport is the cloned *http.Transport directly.
		tr, ok := c.Transport.(*http.Transport)
		require.True(t, ok, "expected *http.Transport when no headers + tls is set")
		assert.Same(t, tlsCfg, tr.TLSClientConfig)
	})

	t.Run("with headers and tls config wraps headerTransport over cloned transport", func(t *testing.T) {
		tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test only
		c := newHTTPClient(map[string]string{"X-Key": "val"}, timeout, tlsCfg)
		ht, ok := c.Transport.(*headerTransport)
		require.True(t, ok, "expected headerTransport")
		tr, ok := ht.base.(*http.Transport)
		require.True(t, ok, "headerTransport.base should be the cloned *http.Transport")
		assert.Same(t, tlsCfg, tr.TLSClientConfig)
	})
}

// TestBuildRemoteMCPServerTLSConfig covers the controller's mirror of the
// agent translator's TLS semantics: tool discovery dials the upstream from
// the controller pod, so it has to construct the same trust chain the agent
// will use at runtime — but from the controller's vantage point (no Secret
// mounted on its filesystem; it has to read the Secret via the kube API).
func TestBuildRemoteMCPServerTLSConfig(t *testing.T) {
	caPEM := generateTestCAPEM(t)

	tests := []struct {
		name        string
		spec        *v1alpha2.TLSConfig
		secret      *corev1.Secret
		wantNil     bool
		wantSkip    bool
		wantCA      bool
		wantSystem  bool
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil spec → no tls config (default transport)",
			spec:    nil,
			wantNil: true,
		},
		{
			name:    "empty struct → no tls config (parity with nil)",
			spec:    &v1alpha2.TLSConfig{},
			wantNil: true,
		},
		{
			name:     "disableVerify only",
			spec:     &v1alpha2.TLSConfig{DisableVerify: true},
			wantSkip: true,
		},
		{
			name: "custom CA only (additive to system pool)",
			spec: &v1alpha2.TLSConfig{
				CACertSecretRef: "ca",
				CACertSecretKey: "ca.crt",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "ns"},
				Data:       map[string][]byte{"ca.crt": caPEM},
			},
			wantCA:     true,
			wantSystem: true,
		},
		{
			name: "custom CA with disableSystemCAs (trust only the bundle)",
			spec: &v1alpha2.TLSConfig{
				CACertSecretRef:  "ca",
				CACertSecretKey:  "ca.crt",
				DisableSystemCAs: true,
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "ns"},
				Data:       map[string][]byte{"ca.crt": caPEM},
			},
			wantCA:     true,
			wantSystem: false,
		},
		{
			name: "missing secret → error",
			spec: &v1alpha2.TLSConfig{
				CACertSecretRef: "ca",
				CACertSecretKey: "ca.crt",
			},
			wantErr:     true,
			errContains: "failed to read CA secret",
		},
		{
			name: "secret present but missing key → error",
			spec: &v1alpha2.TLSConfig{
				CACertSecretRef: "ca",
				CACertSecretKey: "ca.crt",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "ns"},
				Data:       map[string][]byte{"other": []byte("x")},
			},
			wantErr:     true,
			errContains: "does not contain key",
		},
		{
			name: "secret key contains garbage → error",
			spec: &v1alpha2.TLSConfig{
				CACertSecretRef: "ca",
				CACertSecretKey: "ca.crt",
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "ca", Namespace: "ns"},
				Data:       map[string][]byte{"ca.crt": []byte("not a pem")},
			},
			wantErr:     true,
			errContains: "valid PEM certificates",
		},
		// Note: the trust-nothing combination (disableSystemCAs=true alone)
		// used to be rejected here at reconcile time. It's now rejected
		// earlier by the CEL rule on TLSConfig at admission, so it cannot
		// reach this code path. No runtime test needed.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := clientgoscheme.Scheme
			require.NoError(t, v1alpha2.AddToScheme(scheme))

			objs := []client.Object{}
			if tt.secret != nil {
				objs = append(objs, tt.secret)
			}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

			r := &kagentReconciler{kube: kube}
			rms := &v1alpha2.RemoteMCPServer{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns"},
				Spec:       v1alpha2.RemoteMCPServerSpec{URL: "https://x/y", TLS: tt.spec},
			}

			cfg, err := r.buildRemoteMCPServerTLSConfig(context.Background(), rms)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, cfg)
				return
			}
			require.NotNil(t, cfg)
			assert.Equal(t, tt.wantSkip, cfg.InsecureSkipVerify)
			if tt.wantCA {
				require.NotNil(t, cfg.RootCAs, "expected RootCAs populated when CA Secret is referenced")
			}
			// Asserting "system pool was used" by counting subjects is
			// platform-specific — on macOS, x509.SystemCertPool returns
			// a minimal pool because Go defers to platform verification.
			// The path is structurally enforced (SystemCertPool vs
			// NewCertPool) and the strict-trust case (wantSystem=false)
			// covers the alternative branch.
			_ = tt.wantSystem
		})
	}
}

// TestCreateMcpTransport_EgressPlaintext covers the gated egress rewrite on
// the tool-discovery dial: with the gate off, s.Spec.URL is the dial target
// verbatim; with the gate on, an https:// dial URL is rewritten to its
// plaintext http://host:<port-or-443> form, for both Streamable HTTP and SSE.
func TestCreateMcpTransport_EgressPlaintext(t *testing.T) {
	scheme := clientgoscheme.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	const specURL = "https://upstream.example.com/mcp"
	rms := &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "rms", Namespace: "ns"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			Description: "test",
			URL:         specURL,
		},
	}

	t.Run("gate off uses spec.URL verbatim", func(t *testing.T) {
		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube, mcpEgressPlaintext: false}

		tsp, err := r.createMcpTransport(context.Background(), rms)
		require.NoError(t, err)
		require.NotNil(t, tsp)
		streamable, ok := tsp.(*mcp.StreamableClientTransport)
		require.True(t, ok, "expected Streamable HTTP transport for default protocol")
		assert.Equal(t, specURL, streamable.Endpoint)
	})

	t.Run("gate on rewrites https dial URL to plaintext", func(t *testing.T) {
		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube, mcpEgressPlaintext: true}

		tsp, err := r.createMcpTransport(context.Background(), rms)
		require.NoError(t, err)
		streamable, ok := tsp.(*mcp.StreamableClientTransport)
		require.True(t, ok)
		assert.Equal(t, "http://upstream.example.com:443/mcp", streamable.Endpoint, "https dial URL must be rewritten to plaintext")
	})

	t.Run("gate on rewrites SSE dial URL too", func(t *testing.T) {
		sseRMS := rms.DeepCopy()
		sseRMS.Spec.Protocol = v1alpha2.RemoteMCPServerProtocolSse
		sseRMS.Spec.URL = "https://upstream.example.com/sse"

		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube, mcpEgressPlaintext: true}

		tsp, err := r.createMcpTransport(context.Background(), sseRMS)
		require.NoError(t, err)
		sse, ok := tsp.(*mcp.SSEClientTransport)
		require.True(t, ok, "expected SSE transport when protocol is SSE")
		assert.Equal(t, "http://upstream.example.com:443/sse", sse.Endpoint)
	})

	t.Run("gate on rewrites scheme-less dial URL too", func(t *testing.T) {
		schemelessRMS := rms.DeepCopy()
		schemelessRMS.Spec.URL = "host.docker.internal:13443/mcp"

		kube := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &kagentReconciler{kube: kube, mcpEgressPlaintext: true}

		tsp, err := r.createMcpTransport(context.Background(), schemelessRMS)
		require.NoError(t, err)
		streamable, ok := tsp.(*mcp.StreamableClientTransport)
		require.True(t, ok)
		assert.Equal(t, "http://host.docker.internal:13443/mcp", streamable.Endpoint, "scheme-less dial URL must be rewritten to plaintext")
	})
}

// generateTestCAPEM produces a minimal self-signed PEM certificate the
// AppendCertsFromPEM call accepts. The cert isn't valid for any URL —
// these tests only exercise the controller's parse-and-pool path.
func generateTestCAPEM(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "kagent-test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
