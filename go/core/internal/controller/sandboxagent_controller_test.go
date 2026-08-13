package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func secretHeaderRef(name string) v1alpha3.ValueRef {
	return v1alpha3.ValueRef{
		Name: "Authorization",
		ValueFrom: &v1alpha3.ValueSource{
			Type: v1alpha3.SecretValueSource,
			Name: name,
			Key:  "token",
		},
	}
}

func remoteMCPServerTool(name, namespace string) *v1alpha3.Tool {
	return &v1alpha3.Tool{
		McpServer: &v1alpha3.McpServerTool{
			TypedReference: v1alpha3.TypedReference{
				ApiGroup:  "kagent.dev",
				Kind:      "RemoteMCPServer",
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

func remoteMCPServerToolEmptyGroup(name, namespace string) *v1alpha3.Tool {
	return &v1alpha3.Tool{
		McpServer: &v1alpha3.McpServerTool{
			TypedReference: v1alpha3.TypedReference{
				Kind:      "RemoteMCPServer",
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

func declarativeSandboxAgent(name, namespace string, tools ...*v1alpha3.Tool) *v1alpha3.SandboxAgent {
	return &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha3.AgentSpec{
			Type: v1alpha3.AgentType_Declarative,
			Declarative: &v1alpha3.DeclarativeAgentSpec{
				Tools: tools,
			},
		},
	}
}

func TestSandboxAgentSecretFinder(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))

	tests := []struct {
		name       string
		secret     types.NamespacedName
		objects    []client.Object
		wantAgents []types.NamespacedName
	}{
		{
			name:   "indirect RemoteMCPServer reference",
			secret: types.NamespacedName{Name: "credentials", Namespace: "tools"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"tools",
					remoteMCPServerTool("remote", ""),
				),
			},
			wantAgents: []types.NamespacedName{{Name: "agent", Namespace: "tools"}},
		},
		{
			name:   "direct tool header reference",
			secret: types.NamespacedName{Name: "credentials", Namespace: "agents"},
			objects: []client.Object{
				declarativeSandboxAgent(
					"agent",
					"agents",
					&v1alpha3.Tool{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
					},
				),
			},
			wantAgents: []types.NamespacedName{{Name: "agent", Namespace: "agents"}},
		},
		{
			name:   "cross namespace RemoteMCPServer allowed from all",
			secret: types.NamespacedName{Name: "credentials", Namespace: "tools"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
						AllowedNamespaces: &v1alpha3.AllowedNamespaces{
							From: v1alpha3.NamespacesFromAll,
						},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"agents",
					remoteMCPServerTool("remote", "tools"),
				),
			},
			wantAgents: []types.NamespacedName{{Name: "agent", Namespace: "agents"}},
		},
		{
			name:   "Secret and RemoteMCPServer namespaces differ",
			secret: types.NamespacedName{Name: "credentials", Namespace: "other"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
						AllowedNamespaces: &v1alpha3.AllowedNamespaces{
							From: v1alpha3.NamespacesFromAll,
						},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"agents",
					remoteMCPServerTool("remote", "tools"),
				),
			},
		},
		{
			name:   "cross namespace RemoteMCPServer denied by default",
			secret: types.NamespacedName{Name: "credentials", Namespace: "tools"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"agents",
					remoteMCPServerTool("remote", "tools"),
				),
			},
		},
		{
			name:   "empty apiGroup same namespace RemoteMCPServer reference",
			secret: types.NamespacedName{Name: "credentials", Namespace: "tools"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"tools",
					remoteMCPServerToolEmptyGroup("remote", ""),
				),
			},
			wantAgents: []types.NamespacedName{{Name: "agent", Namespace: "tools"}},
		},
		{
			name:   "empty apiGroup cross namespace RemoteMCPServer allowed from all",
			secret: types.NamespacedName{Name: "credentials", Namespace: "tools"},
			objects: []client.Object{
				&v1alpha3.RemoteMCPServer{
					ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
					Spec: v1alpha3.RemoteMCPServerSpec{
						HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
						AllowedNamespaces: &v1alpha3.AllowedNamespaces{
							From: v1alpha3.NamespacesFromAll,
						},
					},
				},
				declarativeSandboxAgent(
					"agent",
					"agents",
					remoteMCPServerToolEmptyGroup("remote", "tools"),
				),
			},
			wantAgents: []types.NamespacedName{{Name: "agent", Namespace: "agents"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			got := (&SandboxAgentController{}).sandboxAgentSecretFinder(
				context.Background(),
				cl,
				tt.secret,
			)

			assert.ElementsMatch(t, tt.wantAgents, got)
		})
	}
}

func TestSandboxAgentSecretFinderReturnsDirectMatchesWhenRemoteMCPServerListFails(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))

	agent := declarativeSandboxAgent(
		"agent",
		"agents",
		&v1alpha3.Tool{
			HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
		},
	)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(
				ctx context.Context,
				c client.WithWatch,
				list client.ObjectList,
				opts ...client.ListOption,
			) error {
				if _, ok := list.(*v1alpha3.RemoteMCPServerList); ok {
					return fmt.Errorf("simulated RemoteMCPServer list failure")
				}
				return c.List(ctx, list, opts...)
			},
		}).
		Build()

	got := (&SandboxAgentController{}).sandboxAgentSecretFinder(
		context.Background(),
		cl,
		types.NamespacedName{Name: "credentials", Namespace: "agents"},
	)

	assert.Equal(t, []types.NamespacedName{{Name: "agent", Namespace: "agents"}}, got)
}

func TestSecretDataChangeSelectsReferencingAgent(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha3.AddToScheme(scheme))

	oldSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: "agents"},
		Data:       map[string][]byte{"token": []byte("old")},
	}
	newSecret := oldSecret.DeepCopy()
	newSecret.Data["token"] = []byte("new")

	assert.True(t, secretDataChangedPredicate().Update(event.UpdateEvent{
		ObjectOld: oldSecret,
		ObjectNew: newSecret,
	}))

	agent := declarativeSandboxAgent(
		"agent",
		"agents",
		&v1alpha3.Tool{
			HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
		},
	)
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(agent).
		Build()

	assert.Equal(
		t,
		[]types.NamespacedName{{Name: "agent", Namespace: "agents"}},
		(&SandboxAgentController{}).sandboxAgentSecretFinder(
			context.Background(),
			cl,
			types.NamespacedName{Name: "credentials", Namespace: "agents"},
		),
	)

	unchanged := newSecret.DeepCopy()
	assert.False(t, secretDataChangedPredicate().Update(event.UpdateEvent{
		ObjectOld: newSecret,
		ObjectNew: unchanged,
	}))
}

func TestRemoteMCPServerReferencesHeaderSecret(t *testing.T) {
	server := &v1alpha3.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: "tools"},
		Spec: v1alpha3.RemoteMCPServerSpec{
			HeadersFrom: []v1alpha3.ValueRef{secretHeaderRef("credentials")},
		},
	}

	assert.True(t, remoteMCPServerReferencesSecret(
		server,
		types.NamespacedName{Name: "credentials", Namespace: "tools"},
	))
	assert.False(t, remoteMCPServerReferencesSecret(
		server,
		types.NamespacedName{Name: "credentials", Namespace: "other"},
	))
}
