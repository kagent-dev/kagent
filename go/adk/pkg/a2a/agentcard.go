package a2a

import (
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
)

// EnrichAgentCard fills the gaps in the compiler-generated agent card from the
// ADK agent: a missing description, the HITL extension and a default interface.
// It never touches skills. The compiler-generated card is authoritative, and
// skills built from the ADK agent embed its instructions, which would expose
// the system prompt on the pod's discovery endpoint (kagent-dev/kagent#2549).
func EnrichAgentCard(card *a2atype.AgentCard, agent adkagent.Agent) {
	if card == nil || agent == nil {
		return
	}

	if card.Description == "" && agent.Description() != "" {
		card.Description = agent.Description()
	}
	// If the agent card does not have the HITL extension, add it.
	// Kagent's harness always supports it.
	if !hasHITLExtension(card.Capabilities.Extensions) {
		card.Capabilities.Extensions = append(card.Capabilities.Extensions, apia2a.HITLExtension())
	}

	// Default to JSONRPC when no interface is explicitly configured.
	if len(card.SupportedInterfaces) == 0 {
		card.SupportedInterfaces = []*a2atype.AgentInterface{
			a2atype.NewAgentInterface("/", a2atype.TransportProtocolJSONRPC),
		}
	}
}

func hasHITLExtension(extensions []a2atype.AgentExtension) bool {
	for _, extension := range extensions {
		if extension.URI == HITLExtensionURI {
			return true
		}
	}
	return false
}
