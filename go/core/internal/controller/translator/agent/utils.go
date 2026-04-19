package agent

import (
	"fmt"
	"slices"
	"strings"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

func GetA2AAgentCard(agent v1alpha2.AgentObject) *server.AgentCard {
	spec := agent.GetAgentSpec()
	preferredTransport := string(a2atype.TransportProtocolJSONRPC)
	card := server.AgentCard{
		Name:               strings.ReplaceAll(agent.GetName(), "-", "_"),
		Description:        spec.Description,
		URL:                fmt.Sprintf("http://%s.%s:8080", agent.GetName(), agent.GetNamespace()),
		PreferredTransport: &preferredTransport,
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
	if spec.Type == v1alpha2.AgentType_Declarative && spec.Declarative != nil && spec.Declarative.A2AConfig != nil {
		decl := spec.Declarative
		card.Skills = slices.Collect(utils.Map(slices.Values(decl.A2AConfig.Skills), func(skill v1alpha2.AgentSkill) server.AgentSkill {
			return server.AgentSkill(skill)
		}))
	}
	return &card
}
