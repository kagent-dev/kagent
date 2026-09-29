package a2a

import (
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
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

	EnsureHITLExtension(card)
	EnsureUsageExtension(card)

	// Default to JSONRPC when no interface is explicitly configured.
	if len(card.SupportedInterfaces) == 0 {
		card.SupportedInterfaces = []*a2atype.AgentInterface{
			a2atype.NewAgentInterface("/", a2atype.TransportProtocolJSONRPC),
		}
	}
}

// EnsureHITLExtension declares the optional HITL extension on the card so a client
// can discover it and negotiate. Kagent's harness always supports it, and the
// declaration does not depend on whether an ADK agent was supplied.
func EnsureHITLExtension(card *a2atype.AgentCard) {
	ensureExtension(card, apia2a.HITLExtension())
}

// EnsureUsageExtension declares the optional usage extension on the card. The
// executor emits usage for every agent, so the declaration is unconditional too.
func EnsureUsageExtension(card *a2atype.AgentCard) {
	ensureExtension(card, apia2a.UsageExtension())
}

func ensureExtension(card *a2atype.AgentCard, extension a2atype.AgentExtension) {
	if card == nil || slices.ContainsFunc(card.Capabilities.Extensions, func(declared a2atype.AgentExtension) bool {
		return declared.URI == extension.URI
	}) {
		return
	}
	card.Capabilities.Extensions = append(card.Capabilities.Extensions, extension)
}
