package translator

import (
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ManagedAgentCard describes the common private A2A contract implemented by
// the kagent, Codex, and Claude harnesses. The gateway replaces the private
// interface while preserving these runtime capabilities.
func ManagedAgentCard(template *v1alpha3.AgentTemplate) *a2atype.AgentCard {
	return &a2atype.AgentCard{
		Name: strings.ReplaceAll(template.Name, "-", "_"), Description: template.Spec.Description, Version: "v1",
		SupportedInterfaces: []*a2atype.AgentInterface{{URL: "http://127.0.0.1:80", ProtocolBinding: a2atype.TransportProtocolGRPC, ProtocolVersion: a2atype.Version}},
		Capabilities:        a2atype.AgentCapabilities{Streaming: true, Extensions: []a2atype.AgentExtension{apia2a.HITLExtension()}},
		Skills:              []a2atype.AgentSkill{}, DefaultInputModes: []string{"text"}, DefaultOutputModes: []string{"text"},
	}
}
