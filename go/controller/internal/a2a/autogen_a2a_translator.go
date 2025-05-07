package a2a

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// translates A2A Handlers from autogen agents/teams
type AutogenA2ATranslator interface {
	TranslateHandlerForAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
		autogenTeam *autogen_client.Team,
	) (*A2AHandlerParams, error)
}

type autogenA2ATranslator struct {
	a2aBaseUrl    string
	autogenClient *autogen_client.Client
}

var _ AutogenA2ATranslator = &autogenA2ATranslator{}

func NewAutogenA2ATranslator(
	a2aBaseUrl string,
	autogenClient *autogen_client.Client,
) AutogenA2ATranslator {
	return &autogenA2ATranslator{
		a2aBaseUrl:    a2aBaseUrl,
		autogenClient: autogenClient,
	}
}

func (a *autogenA2ATranslator) TranslateHandlerForAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	autogenTeam *autogen_client.Team,
) (*A2AHandlerParams, error) {
	card, err := a.translateCardForAgent(ctx, agent)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}

	handler, err := a.makeHandlerForTeam(ctx, autogenTeam)
	if err != nil {
		return nil, err
	}

	return &A2AHandlerParams{
		AgentCard:  *card,
		HandleTask: handler,
	}, nil
}

func (a *autogenA2ATranslator) translateCardForAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
) (*server.AgentCard, error) {
	a2AConfig := agent.Spec.A2AConfig
	if a2AConfig == nil {
		return nil, nil
	}
	skills := a2AConfig.Skills
	if len(skills) == 0 {
		return nil, fmt.Errorf("no skills found for agent %s", agent.Name)
	}
	var convertedSkills []server.AgentSkill
	for _, skill := range skills {
		convertedSkills = append(convertedSkills, server.AgentSkill(skill))
	}
	return &server.AgentCard{
		Name:        agent.Name,
		Description: common.MakePtr(agent.Spec.Description),
		URL:         fmt.Sprintf("%s/%s", a.a2aBaseUrl, agent.Namespace+"/"+agent.Name),
		//Provider:           nil,
		Version: fmt.Sprintf("%v", agent.Generation),
		//DocumentationURL:   nil,
		//Capabilities:       server.AgentCapabilities{},
		//Authentication:     nil,
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             convertedSkills,
	}, nil
}

func (a *autogenA2ATranslator) makeHandlerForTeam(
	ctx context.Context,
	autogenTeam *autogen_client.Team,
) (TaskHandler, error) {
	teamComponent, err := fetchAgentTeam(autogenTeam.Component)
	if err != nil {
		return nil, fmt.Errorf("failed to get team component: %w", err)
	}

	return func(ctx context.Context, task string) (string, error) {
		resp, err := a.autogenClient.InvokeTask(&autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: teamComponent,
		})
		if err != nil {
			return "", fmt.Errorf("failed to invoke task: %w", err)
		}

		var lastMessageContent string
		for _, msg := range resp.TaskResult.Messages {
			switch msg.Content.(type) {
			case string:
				lastMessageContent = msg.Content.(string)
			default:
				b, err := json.Marshal(msg.Content)
				if err != nil {
					return "", fmt.Errorf("failed to marshal message content: %w", err)
				}
				lastMessageContent = string(b)
			}
		}

		return lastMessageContent, nil
	}, nil
}

// TODO(ilackarms): remove this once we stop translating the user proxy agent
// this is a hack to fetch the internally "wrapped" agent team that is produced by the translator, without the society of mind agent and user proxy agent
func fetchAgentTeam(teamComponent *api.Component) (*api.Component, error) {
	teamConfig := &api.RoundRobinGroupChatConfig{}
	err := teamConfig.FromConfig(teamComponent.Config)
	if err != nil {
		return nil, err
	}

	for _, participant := range teamConfig.Participants {
		switch participant.Provider {
		case "kagent.agents.TaskAgent":
			taskAgentConfig := &api.TaskAgentConfig{}
			err := taskAgentConfig.FromConfig(participant.Config)
			if err != nil {
				return nil, err
			}

			// this is the "society of mind" TaskAgent agent, it wraps another team which contains our agent participant, so we must unwrap
			// this is created per-agent for each agent internally by the kagent translator
			return taskAgentConfig.Team, nil
		}
	}

	return nil, fmt.Errorf("failed to find agent team in component")
}
