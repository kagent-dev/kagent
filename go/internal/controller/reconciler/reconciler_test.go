package reconciler

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/utils"
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
								Agent: &v1alpha2.TypedLocalReference{
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
								Agent: &v1alpha2.TypedLocalReference{
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
								Agent: &v1alpha2.TypedLocalReference{
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
								Agent: &v1alpha2.TypedLocalReference{
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
								Agent: &v1alpha2.TypedLocalReference{
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
									TypedLocalReference: v1alpha2.TypedLocalReference{
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
									TypedLocalReference: v1alpha2.TypedLocalReference{
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
									TypedLocalReference: v1alpha2.TypedLocalReference{
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
									TypedLocalReference: v1alpha2.TypedLocalReference{
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
								Agent: &v1alpha2.TypedLocalReference{
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
