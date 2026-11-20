package a2a

import (
	"slices"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	adkagent "google.golang.org/adk/v2/agent"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// EnrichAgentCard fills gaps in the agent card from the ADK agent: a missing
// description, the HITL and usage extensions, and a default interface. It does
// not change the card's skills. Skills built from the ADK agent copy its
// instructions, so they would put the system prompt on the card.
func EnrichAgentCard(card *a2atype.AgentCard, agent adkagent.Agent) {
	if card == nil || agent == nil {
		return
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
