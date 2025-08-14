package translator

import (
	"context"
	"testing"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(s))
	return s
}

// createToolServer creates a ToolServer with the given configuration for testing
func createToolServer(name, namespace string, serverType v1alpha1.ToolServerType, discoveredTools []string) *v1alpha1.ToolServer {
	ts := &v1alpha1.ToolServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: v1alpha1.ToolServerSpec{
			Config: v1alpha1.ToolServerConfig{
				Type: serverType,
			},
		},
		Status: v1alpha1.ToolServerStatus{
			DiscoveredTools: make([]*v1alpha1.MCPTool, len(discoveredTools)),
		},
	}

	// Set the appropriate server config based on type
	switch serverType {
	case v1alpha1.ToolServerTypeSse:
		ts.Spec.Config.Sse = &v1alpha1.SseMcpServerConfig{
			HttpToolServerConfig: v1alpha1.HttpToolServerConfig{URL: "http://example"},
		}
	case v1alpha1.ToolServerTypeStreamableHttp:
		ts.Spec.Config.StreamableHttp = &v1alpha1.StreamableHttpServerConfig{
			HttpToolServerConfig: v1alpha1.HttpToolServerConfig{URL: "http://example"},
		}
	}

	// Convert tool names to MCPTool objects
	for i, toolName := range discoveredTools {
		ts.Status.DiscoveredTools[i] = &v1alpha1.MCPTool{Name: toolName}
	}

	return ts
}

func TestTranslateToolServerTool(t *testing.T) {
	tests := []struct {
		name              string
		serverType        v1alpha1.ToolServerType
		discoveredTools   []string
		providedTools     []string
		expectedTools     []string
		expectedSseCount  int
		expectedHttpCount int
	}{
		{
			name:             "SSE server uses discovered tools when none provided",
			serverType:       v1alpha1.ToolServerTypeSse,
			discoveredTools:  []string{"alpha", "beta"},
			providedTools:    nil,
			expectedTools:    []string{"alpha", "beta"},
			expectedSseCount: 1,
		},
		{
			name:             "SSE server keeps provided tools when specified",
			serverType:       v1alpha1.ToolServerTypeSse,
			discoveredTools:  []string{"alpha"},
			providedTools:    []string{"override"},
			expectedTools:    []string{"override"},
			expectedSseCount: 1,
		},
		{
			name:              "StreamableHTTP server uses discovered tools when none provided",
			serverType:        v1alpha1.ToolServerTypeStreamableHttp,
			discoveredTools:   []string{"one", "two", "three"},
			providedTools:     nil,
			expectedTools:     []string{"one", "two", "three"},
			expectedHttpCount: 1,
		},
		{
			name:              "StreamableHTTP server keeps provided tools when specified",
			serverType:        v1alpha1.ToolServerTypeStreamableHttp,
			discoveredTools:   []string{"one", "two"},
			providedTools:     []string{"custom"},
			expectedTools:     []string{"custom"},
			expectedHttpCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newScheme(t)
			ts := createToolServer("test-server", "test-ns", tt.serverType, tt.discoveredTools)
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ts).Build()
			tr := NewAdkApiTranslator(kube, types.NamespacedName{}).(*adkApiTranslator)

			agent := &adk.AgentConfig{}
			err := tr.translateToolServerTool(context.Background(), agent, "test-server", tt.providedTools, "test-ns")

			require.NoError(t, err)
			assert.Len(t, agent.SseTools, tt.expectedSseCount)
			assert.Len(t, agent.HttpTools, tt.expectedHttpCount)

			if tt.expectedSseCount > 0 {
				assert.Equal(t, tt.expectedTools, agent.SseTools[0].Tools)
			}
			if tt.expectedHttpCount > 0 {
				assert.Equal(t, tt.expectedTools, agent.HttpTools[0].Tools)
			}
		})
	}
}
