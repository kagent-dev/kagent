package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/utils"
)

func GetA2AAgentCard(agent *v1alpha2.Agent) *a2a.AgentCard {
	card := a2a.AgentCard{
		Name:        strings.ReplaceAll(agent.Name, "-", "_"),
		Description: agent.Spec.Description,
		URL:         fmt.Sprintf("http://%s.%s:8080", agent.Name, agent.Namespace),
		Capabilities: a2a.AgentCapabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		// Can't be null for Python, so set to empty list
		Skills:             []a2a.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	if agent.Spec.Type == v1alpha2.AgentType_Declarative && agent.Spec.Declarative != nil && agent.Spec.Declarative.A2AConfig != nil {
		card.Skills = slices.Collect(utils.Map(slices.Values(agent.Spec.Declarative.A2AConfig.Skills), func(skill v1alpha2.AgentSkill) a2a.AgentSkill {
			return a2a.AgentSkill(skill)
		}))
	}
	return &card
}
