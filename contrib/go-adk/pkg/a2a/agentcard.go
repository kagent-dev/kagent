package a2a

import (
	a2atype "github.com/a2aproject/a2a-go/a2a"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/server/adka2a"
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
}
