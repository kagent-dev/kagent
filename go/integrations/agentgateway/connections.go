package agentgateway

import (
	"fmt"
	"strings"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// Model builds the kagent consumer for one concrete gateway model. Templates
// and source objects remain unchanged.
func (c Config) Model(parent *agw.AgentgatewayModel) (*kagentv1alpha3.ModelConfig, error) {
	u, err := endpoint(c.ModelURL)
	if err != nil {
		return nil, err
	}
	if c.Gateway.Name != "" && !c.attached(parent) {
		return nil, fmt.Errorf("model is not attached to the configured Gateway")
	}
	name := parent.Name
	if parent.Spec.Match != nil && parent.Spec.Match.Model != nil {
		name = *parent.Spec.Match.Model
	}
	if name == "" || strings.Contains(name, "*") {
		return nil, fmt.Errorf("a concrete model is required")
	}
	model := &kagentv1alpha3.ModelConfig{}
	if c.ModelTemplate != nil {
		model, err = c.ModelTemplate(parent.DeepCopy())
		if err != nil {
			return nil, fmt.Errorf("failed to build model template: %w", err)
		}
		if model == nil {
			return nil, fmt.Errorf("model template returned nil")
		}
		model = model.DeepCopy()
	}
	model.Name, model.Namespace, model.Spec.Model = parent.Name, parent.Namespace, name
	if model.Spec.Provider == "" && parent.Spec.Provider != nil && *parent.Spec.Provider == agw.ModelProviderAnthropic {
		model.Spec.Provider = kagentv1alpha3.ModelProviderAnthropic
	}
	if model.Spec.Provider == kagentv1alpha3.ModelProviderAnthropic {
		if model.Spec.Anthropic == nil {
			model.Spec.Anthropic = &kagentv1alpha3.AnthropicConfig{}
		}
		model.Spec.Anthropic.BaseURL = u.String()
		model.Spec.OpenAI = nil
	} else {
		model.Spec.Provider = kagentv1alpha3.ModelProviderOpenAI
		if model.Spec.OpenAI == nil {
			model.Spec.OpenAI = &kagentv1alpha3.OpenAIConfig{}
		}
		model.Spec.OpenAI.BaseURL = u.String() + "/v1"
		model.Spec.Anthropic = nil
	}
	return model, nil
}

func (c Config) attached(parent *agw.AgentgatewayModel) bool {
	for _, ref := range parent.Spec.ParentRefs {
		if ref.Name != c.Gateway.Name || (ref.Kind != nil && *ref.Kind != "Gateway") ||
			(ref.Group != nil && *ref.Group != gatewayv1.GroupName) {
			continue
		}
		sourceNamespace, targetNamespace := parent.Namespace, parent.Namespace
		if ref.Namespace != nil {
			sourceNamespace = string(*ref.Namespace)
		}
		if c.Gateway.Namespace != nil {
			targetNamespace = string(*c.Gateway.Namespace)
		}
		if sourceNamespace == targetNamespace &&
			(ref.SectionName == nil || c.Gateway.SectionName == nil || *ref.SectionName == *c.Gateway.SectionName) &&
			(ref.Port == nil || c.Gateway.Port == nil || *ref.Port == *c.Gateway.Port) {
			return true
		}
	}
	return false
}

// MCP builds the route, kagent consumer and optional route policy for one MCP
// backend. Proxy configuration is left to the agentgateway controller.
func (c Config) MCP(parent *agw.AgentgatewayBackend) ([]client.Object, error) {
	if parent.Spec.MCP == nil {
		return nil, fmt.Errorf("an MCP backend is required")
	}
	if c.Gateway.Name == "" {
		return nil, fmt.Errorf("gateway parent name is required")
	}
	u, err := endpoint(c.MCPURL)
	if err != nil {
		return nil, err
	}
	remote := &kagentv1alpha3.RemoteMCPServer{}
	if c.MCPTemplate != nil {
		remote, err = c.MCPTemplate(parent.DeepCopy())
		if err != nil {
			return nil, fmt.Errorf("failed to build MCP template: %w", err)
		}
		if remote == nil {
			return nil, fmt.Errorf("MCP template returned nil")
		}
		remote = remote.DeepCopy()
	}
	path := "/mcp/" + parent.Name
	remote.Name, remote.Namespace = parent.Name, parent.Namespace
	remote.Spec.URL = u.String() + path
	remote.Spec.Protocol = kagentv1alpha3.RemoteMCPServerProtocolStreamableHttp
	if remote.Spec.Description == "" {
		remote.Spec.Description = parent.Name
	}
	routeName := "mcp-" + parent.Name
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: parent.Namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{c.Gateway}},
			Rules: []gatewayv1.HTTPRouteRule{{
				Matches: []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchPathPrefix), Value: &path}}},
				BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: new(gatewayv1.Group(agw.GroupVersion.Group)), Kind: new(gatewayv1.Kind("AgentgatewayBackend")), Name: gatewayv1.ObjectName(parent.Name),
				}}}},
			}},
		},
	}
	objects := []client.Object{route, remote}
	if c.MCPPolicy != nil {
		objects = append(objects, &agw.AgentgatewayPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: parent.Name + "-runtime-auth", Namespace: parent.Namespace},
			Spec: agw.AgentgatewayPolicySpec{
				TargetRefs: []agw.LocalPolicyTargetReferenceWithSectionName{{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: gatewayv1.ObjectName(routeName)}},
				Traffic:    c.MCPPolicy.DeepCopy(),
			},
		})
	}
	return objects, nil
}
