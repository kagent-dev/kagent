package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	ctrl_client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	agenttranslator "github.com/kagent-dev/kagent/go/core/internal/controller/translator/agent"
)

func egressModelConfig() *v1alpha2.ModelConfig {
	return &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default-model", Namespace: "test"},
		Spec:       v1alpha2.ModelConfigSpec{Provider: "OpenAI", Model: "gpt-4o"},
	}
}

func egressRMS(name, url string) *v1alpha2.RemoteMCPServer {
	return &v1alpha2.RemoteMCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
		Spec: v1alpha2.RemoteMCPServerSpec{
			URL:      url,
			Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
		},
	}
}

func egressAgent(rmsName string) *v1alpha2.Agent {
	return &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: "test"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				SystemMessage: "Test",
				ModelConfig:   "default-model",
				Tools: []*v1alpha2.Tool{
					{
						Type: v1alpha2.ToolProviderType_McpServer,
						McpServer: &v1alpha2.McpServerTool{
							TypedReference: v1alpha2.TypedReference{Name: rmsName, Kind: "RemoteMCPServer"},
							ToolNames:      []string{"test-tool"},
						},
					},
				},
			},
		},
	}
}

func egressTranslator(t *testing.T, mcpEgressPlaintext bool, proxyURL string, objs ...ctrl_client.Object) agenttranslator.AdkApiTranslator {
	t.Helper()
	scheme := schemev1.Scheme
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return agenttranslator.NewAdkApiTranslatorWithWatchedNamespaces(
		kubeClient,
		nil,
		types.NamespacedName{Name: "default-model", Namespace: "test"},
		nil,
		proxyURL,
		nil,
		mcpEgressPlaintext,
	)
}

func egressToolURL(t *testing.T, result *agenttranslator.AgentOutputs) string {
	t.Helper()
	require.NotNil(t, result)
	require.NotNil(t, result.Config)
	require.Len(t, result.Config.HttpTools, 1)
	return result.Config.HttpTools[0].Params.Url
}

// TestEgressRewrite_ThroughTranslateAgent exercises the inline egress rewrite on
// the agent tool URL: an external RemoteMCPServer is emitted in plaintext form
// when the gate is on, verbatim when it is off, and proxy routing still wins for
// internal-k8s URLs.
func TestEgressRewrite_ThroughTranslateAgent(t *testing.T) {
	ctx := context.Background()
	testNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	t.Run("external RMS with gate on is rewritten to plaintext effective port", func(t *testing.T) {
		rms := egressRMS("external-mcp", "https://external-mcp.example.com/mcp")
		tr := egressTranslator(t, true, "", egressAgent("external-mcp"), rms, egressModelConfig(), testNamespace)

		result, err := agenttranslator.TranslateAgent(ctx, tr, egressAgent("external-mcp"))
		require.NoError(t, err)
		assert.Equal(t, "http://external-mcp.example.com:443/mcp", egressToolURL(t, result))
	})

	t.Run("external RMS with gate off is left verbatim", func(t *testing.T) {
		rms := egressRMS("external-mcp", "https://external-mcp.example.com/mcp")
		tr := egressTranslator(t, false, "", egressAgent("external-mcp"), rms, egressModelConfig(), testNamespace)

		result, err := agenttranslator.TranslateAgent(ctx, tr, egressAgent("external-mcp"))
		require.NoError(t, err)
		assert.Equal(t, "https://external-mcp.example.com/mcp", egressToolURL(t, result))
	})

	t.Run("internal RMS with proxy still routes through proxy, not egress", func(t *testing.T) {
		// The URL's namespace (kagent) must exist for isInternalK8sURL to treat
		// it as in-cluster and prefer the proxy over the egress rewrite.
		kagentNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kagent"}}
		rms := egressRMS("internal-mcp", "http://test-mcp-server.kagent:8084/mcp")
		tr := egressTranslator(t, true, "http://proxy.kagent.svc.cluster.local:8080",
			egressAgent("internal-mcp"), rms, egressModelConfig(), testNamespace, kagentNamespace)

		result, err := agenttranslator.TranslateAgent(ctx, tr, egressAgent("internal-mcp"))
		require.NoError(t, err)
		httpTool := result.Config.HttpTools[0]
		assert.Equal(t, "http://proxy.kagent.svc.cluster.local:8080/mcp", httpTool.Params.Url)
		assert.Equal(t, "test-mcp-server.kagent", httpTool.Params.Headers[agenttranslator.ProxyHostHeader])
	})

	t.Run("scheme-less TLS RMS with gate on resolves to effective 443", func(t *testing.T) {
		rms := egressRMS("tls-mcp", "tls-mcp.example.com/mcp")
		rms.Spec.TLS = &v1alpha2.TLSConfig{}
		tr := egressTranslator(t, true, "", egressAgent("tls-mcp"), rms, egressModelConfig(), testNamespace)

		result, err := agenttranslator.TranslateAgent(ctx, tr, egressAgent("tls-mcp"))
		require.NoError(t, err)
		assert.Equal(t, "http://tls-mcp.example.com:443/mcp", egressToolURL(t, result))
	})
}
