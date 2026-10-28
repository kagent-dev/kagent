// Package agentgateway connects explicitly selected agentgateway resources to
// kagent consumers. The agentgateway controller owns proxy configuration.
package agentgateway

import (
	"fmt"
	"net/url"

	agw "github.com/agentgateway/agentgateway/controller/api/v1alpha1/agentgateway"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
)

const (
	// ConnectionAnnotation opts a gateway resource into consumer reconciliation.
	ConnectionAnnotation = "kagent.dev/connection"
	// ReadyCondition reports whether the derived kagent resources converged.
	ReadyCondition = "KagentReady"
)

// Config supplies gateway endpoints and application-owned consumer settings.
// Register the integration explicitly through SetupWithManager; without it,
// kagent has no dependency on installed agentgateway CRDs.
type Config struct {
	Gateway  gatewayv1.ParentReference
	ModelURL string
	MCPURL   string
	// ModelTemplate supplies native runtime settings and credential references.
	// The integration fills the resource identity, model name and gateway URL.
	ModelTemplate func(*agw.AgentgatewayModel) (*kagentv1alpha3.ModelConfig, error)
	// MCPTemplate supplies discovery authentication, timeouts and metadata.
	// The integration fills the resource identity and gateway URL.
	MCPTemplate func(*agw.AgentgatewayBackend) (*kagentv1alpha3.RemoteMCPServer, error)
	// MCPPolicy is attached to each derived MCP route. Authorization belongs
	// to the application; an omitted policy creates no authentication policy.
	MCPPolicy *agw.Traffic
}

func endpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse gateway endpoint: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("gateway endpoint must be an HTTP(S) origin")
	}
	u.Path = ""
	return u, nil
}

func (c Config) validate() error {
	if c.Gateway.Name == "" {
		return fmt.Errorf("gateway parent name is required")
	}
	if c.Gateway.Group != nil && *c.Gateway.Group != gatewayv1.GroupName {
		return fmt.Errorf("gateway parent must use the Gateway API group")
	}
	if c.Gateway.Kind != nil && *c.Gateway.Kind != "Gateway" {
		return fmt.Errorf("gateway parent must reference a Gateway")
	}
	for _, raw := range []string{c.ModelURL, c.MCPURL} {
		if _, err := endpoint(raw); err != nil {
			return err
		}
	}
	return nil
}
