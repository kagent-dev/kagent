package a2a

import (
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/server/adka2a/v2"
)

// EnrichAgentCard populates the agent card with skills derived from the ADK
// agent using adka2a.BuildAgentSkills. It also fills in the description from
// the agent when the card has none.
func EnrichAgentCard(card *a2atype.AgentCard, agent adkagent.Agent) {
	if card == nil || agent == nil {
		return
	}

	if skills := adka2a.BuildAgentSkills(agent); len(skills) > 0 {
		card.Skills = skills
	}

	if card.Description == "" && agent.Description() != "" {
		card.Description = agent.Description()
	}
	// If the agent card does not have the HITL extension, add it.
	// Kagent's harness always supports it.
	if !hasHITLExtension(card.Capabilities.Extensions) {
		card.Capabilities.Extensions = append(card.Capabilities.Extensions, a2atype.AgentExtension{
			URI: HITLExtensionURI, Description: "Human in the loop for tool approval, ask user, and nested subagents",
			Required: false,
		})
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
