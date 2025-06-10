package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	autogenClient autogen_client.Client
}

var _ AutogenA2ATranslator = &autogenA2ATranslator{}

func NewAutogenA2ATranslator(
	a2aBaseUrl string,
	autogenClient autogen_client.Client,
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
	card, err := a.translateCardForAgent(agent)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}

	handler, err := a.makeHandlerForTeam(autogenTeam)
	if err != nil {
		return nil, err
	}

	return &A2AHandlerParams{
		AgentCard:  *card,
		HandleTask: handler,
	}, nil
}

func (a *autogenA2ATranslator) translateCardForAgent(
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
	autogenTeam *autogen_client.Team,
) (TaskHandler, error) {
	return func(ctx context.Context, task string, sessionID *string) (string, error) {
		var taskResult *autogen_client.TaskResult
		if sessionID != nil && *sessionID != "" {
			session, err := a.autogenClient.GetSession(*sessionID, common.GetGlobalUserID())
			if err != nil {
				if errors.Is(err, autogen_client.NotFoundError) {
					session, err = a.autogenClient.CreateSession(&autogen_client.CreateSession{
						Name:   *sessionID,
						UserID: common.GetGlobalUserID(),
						TeamID: autogenTeam.Id,
					})
					if err != nil {
						return "", fmt.Errorf("failed to create session: %w", err)
					}
				} else {
					return "", fmt.Errorf("failed to get session: %w", err)
				}
			}
			resp, err := a.autogenClient.InvokeSession(session.ID, common.GetGlobalUserID(), task)
			if err != nil {
				return "", fmt.Errorf("failed to invoke task: %w", err)
			}
			taskResult = &resp.TaskResult
		} else {

			resp, err := a.autogenClient.InvokeTask(&autogen_client.InvokeTaskRequest{
				Task:       task,
				TeamConfig: autogenTeam.Component,
			})
			if err != nil {
				return "", fmt.Errorf("failed to invoke task: %w", err)
			}
			taskResult = &resp.TaskResult
		}

		var lastMessageContent string
		for _, msg := range taskResult.Messages {
			switch msg["content"].(type) {
			case string:
				lastMessageContent = msg["content"].(string)
			default:
				b, err := json.Marshal(msg["content"])
				if err != nil {
					return "", fmt.Errorf("failed to marshal message content: %w", err)
				}
				lastMessageContent = string(b)
			}
		}

		return lastMessageContent, nil
	}, nil
}
