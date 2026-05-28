package agent

import (
	"fmt"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func GetA2AAgentCard(agent v1alpha2.AgentObject) *a2atype.AgentCard {
	spec := agent.GetAgentSpec()
	card := a2atype.AgentCard{
		Name:        strings.ReplaceAll(agent.GetName(), "-", "_"),
		Description: spec.Description,
		SupportedInterfaces: []*a2atype.AgentInterface{
			{
				URL:             fmt.Sprintf("http://%s.%s:8080", agent.GetName(), agent.GetNamespace()),
				ProtocolBinding: a2atype.TransportProtocolJSONRPC,
				ProtocolVersion: a2atype.ProtocolVersion("0.3"),
			},
			{
				URL:             fmt.Sprintf("http://%s.%s:8080", agent.GetName(), agent.GetNamespace()),
				ProtocolBinding: a2atype.TransportProtocolJSONRPC,
				ProtocolVersion: a2atype.Version,
			},
		},
		Capabilities: a2atype.AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
		},
		// Can't be null for Python, so set to empty list.
		Skills:             []a2atype.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	if spec.Type == v1alpha2.AgentType_Declarative && spec.Declarative != nil && spec.Declarative.A2AConfig != nil {
		card.Skills = make([]a2atype.AgentSkill, 0, len(spec.Declarative.A2AConfig.Skills))
		for _, skill := range spec.Declarative.A2AConfig.Skills {
			card.Skills = append(card.Skills, a2atype.AgentSkill{
				ID:          skill.ID,
				Name:        skill.Name,
				Description: skill.Description,
				Tags:        skill.Tags,
				Examples:    skill.Examples,
				InputModes:  skill.InputModes,
				OutputModes: skill.OutputModes,
			})
		}
	}
	return &card
}
