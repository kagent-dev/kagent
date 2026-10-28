package agentgateway

import (
	"errors"
	"testing"
	"time"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func testConfig() Config {
	return Config{
		Gateway:  gatewayv1.ParentReference{Name: "gateway", SectionName: ptr.To(gatewayv1.SectionName("http"))},
		ModelURL: "https://models.example.com", MCPURL: "https://mcp.example.com",
	}
}

func testModel(name string) *agw.AgentgatewayModel {
	return &agw.AgentgatewayModel{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Annotations: map[string]string{ConnectionAnnotation: "true"}},
		Spec: agw.AgentgatewayModelSpec{Provider: ptr.To(agw.ModelProviderOpenAI),
			ParentRefs: []gatewayv1.ParentReference{testConfig().Gateway},
			Match:      &agw.ModelMatch{Model: new("concrete-model")}},
	}
}

func testBackend(name string) *agw.AgentgatewayBackend {
	return &agw.AgentgatewayBackend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Annotations: map[string]string{ConnectionAnnotation: "true"}},
		Spec: agw.AgentgatewayBackendSpec{MCP: &agw.MCPBackend{
			Targets: []agw.McpTargetSelector{{Name: "tools", Static: &agw.McpTarget{Host: new("tools.example.com"), Port: 8080}}},
		}},
	}
}

func TestModelUsesGatewayAndNativeTemplate(t *testing.T) {
	config := testConfig()
	source := testModel("model")
	template := &kagentv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"example.com/purpose": "embedding"}},
		Spec:       kagentv1alpha3.ModelConfigSpec{APIKeySecret: "client", APIKeySecretKey: "placeholder", OpenAI: &kagentv1alpha3.OpenAIConfig{BaseURL: "https://unused.example.com"}},
	}
	before := template.DeepCopy()
	config.ModelTemplate = func(*agw.AgentgatewayModel) (*kagentv1alpha3.ModelConfig, error) { return template, nil }
	model, err := config.Model(source)
	require.NoError(t, err)
	require.Equal(t, "concrete-model", model.Spec.Model)
	require.Equal(t, "https://models.example.com/v1", model.Spec.OpenAI.BaseURL)
	require.Equal(t, "client", model.Spec.APIKeySecret)
	require.Equal(t, "embedding", model.Annotations["example.com/purpose"])
	require.Equal(t, before, template)
	require.Equal(t, "model", source.Name)

	source.Spec.Provider = ptr.To(agw.ModelProviderAnthropic)
	config.ModelTemplate = nil
	model, err = config.Model(source)
	require.NoError(t, err)
	require.Equal(t, kagentv1alpha3.ModelProviderAnthropic, model.Spec.Provider)
	require.Equal(t, config.ModelURL, model.Spec.Anthropic.BaseURL)
	require.Nil(t, model.Spec.OpenAI)

	source.Spec.Match.Model = new("model-*")
	_, err = config.Model(source)
	require.ErrorContains(t, err, "concrete model")
	config.ModelTemplate = func(*agw.AgentgatewayModel) (*kagentv1alpha3.ModelConfig, error) {
		return nil, errors.New("template failure")
	}
	source.Spec.Match.Model = new("model")
	_, err = config.Model(source)
	require.ErrorContains(t, err, "template failure")
	config.ModelTemplate = nil
	source.Spec.ParentRefs[0].Name = "another-gateway"
	_, err = config.Model(source)
	require.ErrorContains(t, err, "configured Gateway")
	source.Spec.ParentRefs[0].Name = config.Gateway.Name
	source.Spec.ParentRefs[0].Port = new(gatewayv1.PortNumber(8080))
	config.Gateway.Port = new(gatewayv1.PortNumber(9090))
	_, err = config.Model(source)
	require.ErrorContains(t, err, "configured Gateway")
}

func TestMCPKeepsApplicationPolicyAndDiscoverySettings(t *testing.T) {
	config := testConfig()
	config.MCPPolicy = &agw.Traffic{Authorization: &agw.Authorization{
		Action: agw.AuthorizationPolicyActionRequire,
		Policy: agw.AuthorizationPolicy{MatchExpressions: []agw.CELExpression{"jwt.sub == 'actor'"}},
	}}
	config.MCPTemplate = func(*agw.AgentgatewayBackend) (*kagentv1alpha3.RemoteMCPServer, error) {
		return &kagentv1alpha3.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"example.com/discovery": "kubernetes"}},
			Spec:       kagentv1alpha3.RemoteMCPServerSpec{Description: "Team tools", Timeout: &metav1.Duration{Duration: time.Minute}},
		}, nil
	}
	objects, err := config.MCP(testBackend("tools"))
	require.NoError(t, err)
	require.Len(t, objects, 3)
	route := objects[0].(*gatewayv1.HTTPRoute)
	remote := objects[1].(*kagentv1alpha3.RemoteMCPServer)
	policy := objects[2].(*agw.AgentgatewayPolicy)
	require.Equal(t, config.Gateway, route.Spec.ParentRefs[0])
	require.Equal(t, gatewayv1.ObjectName("tools"), route.Spec.Rules[0].BackendRefs[0].Name)
	require.Equal(t, "https://mcp.example.com/mcp/tools", remote.Spec.URL)
	require.Equal(t, "kubernetes", remote.Annotations["example.com/discovery"])
	require.Equal(t, time.Minute, remote.Spec.Timeout.Duration)
	require.Equal(t, config.MCPPolicy, policy.Spec.Traffic)
	policy.Spec.Traffic.Authorization.Policy.MatchExpressions[0] = "false"
	require.Equal(t, agw.CELExpression("jwt.sub == 'actor'"), config.MCPPolicy.Authorization.Policy.MatchExpressions[0])

	config.MCPPolicy = nil
	objects, err = config.MCP(testBackend("tools"))
	require.NoError(t, err)
	require.Len(t, objects, 2)
	_, err = config.MCP(&agw.AgentgatewayBackend{})
	require.ErrorContains(t, err, "MCP backend")
}

func TestConfigurationRejectsAmbiguousEndpoints(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/config", "https://user:password@example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com#fragment"} {
		t.Run(raw, func(t *testing.T) {
			config := testConfig()
			config.ModelURL = raw
			require.Error(t, config.validate())
		})
	}
	config := testConfig()
	config.Gateway.Name = ""
	require.Error(t, SetupWithManager(nil, config))
}
