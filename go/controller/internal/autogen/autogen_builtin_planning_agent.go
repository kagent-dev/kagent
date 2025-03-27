package autogen

import (
	_ "embed"
	"fmt"

	"github.com/kagent-dev/kagent/go/autogen/api"
)

// embed planning-agent-system-prompt.txt
//
//go:embed planning-agent-system-prompt.txt
var planningAgentSystemPrompt string

const planningAgentDescription = "The Planning Agent is responsible for planning and scheduling tasks. The planning agent is also responsible for deciding when the user task has been accomplished and terminating the conversation."

func MakeBuiltinPlanningAgent(
	name string,
	teamParticipants []*api.Component,
	modelClient *api.Component,
) *api.Component {
	var handoffs []api.Handoff
	for _, participant := range teamParticipants {
		assistantAgent := &api.AssistantAgentConfig{}
		api.MustFromConfig(assistantAgent, participant.Config)
		targetName := assistantAgent.Name
		handoffs = append(handoffs, api.Handoff{
			Target:      targetName,
			Description: fmt.Sprintf("Handoff to %s. %s", targetName, assistantAgent.Description),
			Name:        fmt.Sprintf("transfer_to_%s", targetName),
			Message:     fmt.Sprintf("Transferred to %s, adopting the role of %s immediately.", targetName, targetName),
		})
	}
	return &api.Component{
		Provider:      "autogen_agentchat.agents.AssistantAgent",
		ComponentType: "agent",
		Version:       1,
		Description:   planningAgentDescription,
		Config: api.MustToConfig(&api.AssistantAgentConfig{
			Name:        name,
			ModelClient: modelClient,
			ModelContext: &api.Component{
				Provider:      "autogen_core.model_context.UnboundedChatCompletionContext",
				ComponentType: "chat_completion_context",
				Version:       1,
				Config:        api.MustToConfig(&api.ChatCompletionContextConfig{}),
			},
			Description: planningAgentDescription,
			// TODO(ilackarms): convert to non-ptr with omitempty?
			SystemMessage:         planningAgentSystemPrompt,
			ReflectOnToolUse:      false,
			ToolCallSummaryFormat: "{result}",
			Handoffs:              handoffs,
		}),
	}
}
