package a2a

import (
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	adkagent "google.golang.org/adk/v2/agent"
)

// EnrichAgentCard fills the gaps in the curated agent card from the ADK agent.
// The curated card is authoritative: skills it provides (including an explicit
// empty list) are kept verbatim. Only when the card carries no skills field at
// all are skills derived, and then only from agent and sub-agent names and
// descriptions — never from instructions, which would expose the system
// prompt on the discovery endpoint (kagent-dev/kagent#2549).
func EnrichAgentCard(card *a2atype.AgentCard, agent adkagent.Agent) {
	if card == nil || agent == nil {
		return
	}

	if card.Skills == nil {
		card.Skills = deriveAgentSkills(agent)
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

// deriveAgentSkills builds a skill list from the agent's own description and
// its sub-agents. It deliberately ignores instructions and tools: the former
// is the system prompt, the latter is already reachable through tool listing.
func deriveAgentSkills(agent adkagent.Agent) []a2atype.AgentSkill {
	skills := []a2atype.AgentSkill{{
		ID:          agent.Name(),
		Name:        agent.Name(),
		Description: agentSkillDescription(agent),
		Tags:        []string{"llm"},
	}}
	for _, sub := range agent.SubAgents() {
		skills = append(skills, a2atype.AgentSkill{
			ID:          fmt.Sprintf("%s_%s", agent.Name(), sub.Name()),
			Name:        sub.Name(),
			Description: agentSkillDescription(sub),
			Tags:        []string{"llm", fmt.Sprintf("sub_agent:%s", sub.Name())},
		})
	}
	return skills
}

func agentSkillDescription(agent adkagent.Agent) string {
	if description := agent.Description(); description != "" {
		return description
	}
	return fmt.Sprintf("Agent %s", agent.Name())
}

func hasHITLExtension(extensions []a2atype.AgentExtension) bool {
	for _, extension := range extensions {
		if extension.URI == HITLExtensionURI {
			return true
		}
	}
	return false
}
