package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

func GetA2AAgentCard(agent *v1alpha2.Agent) *server.AgentCard {
	card := server.AgentCard{
		Name:        strings.ReplaceAll(agent.Name, "-", "_"),
		Description: agent.Spec.Description,
		URL:         fmt.Sprintf("http://%s.%s:8080", agent.Name, agent.Namespace),
		Capabilities: server.AgentCapabilities{
			Streaming:              new(true),
			PushNotifications:      new(false),
			StateTransitionHistory: new(true),
		},
		// Can't be null for Python, so set to empty list
		Skills:             []server.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	if agent.Spec.Type == v1alpha2.AgentType_Declarative && agent.Spec.Declarative != nil && agent.Spec.Declarative.A2AConfig != nil {
		card.Skills = slices.Collect(utils.Map(slices.Values(agent.Spec.Declarative.A2AConfig.Skills), func(skill v1alpha2.AgentSkill) server.AgentSkill {
			return server.AgentSkill(skill)
		}))
	}
	return &card
}
