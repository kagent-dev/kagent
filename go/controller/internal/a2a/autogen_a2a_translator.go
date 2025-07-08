package a2a

import (
	"context"
	"errors"
	"fmt"
	"log"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	"k8s.io/utils/ptr"
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
		AgentCard:   *card,
		TaskHandler: handler,
	}, nil
}

func (a *autogenA2ATranslator) translateCardForAgent(
	agent *v1alpha1.Agent,
) (*server.AgentCard, error) {
	a2AConfig := agent.Spec.A2AConfig
	if a2AConfig == nil {
		return nil, nil
	}

	agentRef := common.GetObjectRef(agent)

	skills := a2AConfig.Skills
	if len(skills) == 0 {
		return nil, fmt.Errorf("no skills found for agent %s", agentRef)
	}

	var convertedSkills []server.AgentSkill
	for _, skill := range skills {
		convertedSkills = append(convertedSkills, server.AgentSkill(skill))
	}

	return &server.AgentCard{
		Name:        agentRef,
		Description: agent.Spec.Description,
		URL:         fmt.Sprintf("%s/%s", a.a2aBaseUrl, agentRef),
		//Provider:           nil,
		Version: fmt.Sprintf("%v", agent.Generation),
		//DocumentationURL:   nil,
		//Authentication:     nil,
		Capabilities: server.AgentCapabilities{
			Streaming: ptr.To(true),
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills:             convertedSkills,
	}, nil
}

func (a *autogenA2ATranslator) makeHandlerForTeam(
	autogenTeam *autogen_client.Team,
) (MessageHandler, error) {
	return &taskHandler{
		team:   autogenTeam,
		client: a.autogenClient,
	}, nil
}

type taskHandler struct {
	team   *autogen_client.Team
	client autogen_client.Client
}

func (t *taskHandler) HandleMessage(ctx context.Context, task string, contextID string) ([]autogen_client.Event, error) {
	var taskResult *autogen_client.TaskResult
	if contextID != "" {
		session, err := t.client.GetSession(contextID, common.GetGlobalUserID())
		if err != nil {
			if errors.Is(err, autogen_client.NotFoundError) {
				session, err = t.client.CreateSession(&autogen_client.CreateSession{
					Name:   contextID,
					UserID: common.GetGlobalUserID(),
				})
				if err != nil {
					return nil, fmt.Errorf("failed to create session: %w", err)
				}
			} else {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
		}
		resp, err := t.client.InvokeSession(session.ID, common.GetGlobalUserID(), &autogen_client.InvokeRequest{
			Task:       task,
			TeamConfig: t.team.Component,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}
		taskResult = &resp.TaskResult
	} else {

		resp, err := t.client.InvokeTask(&autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: t.team.Component,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}
		taskResult = &resp.TaskResult
	}

	events := make([]autogen_client.Event, len(taskResult.Messages))
	for i, msg := range taskResult.Messages {
		parsedEvent, err := autogen_client.ParseEvent(msg)
		if err != nil {
			log.Printf("failed to parse event: %v", err)
			continue
		}
		events[i] = parsedEvent
	}

	return events, nil
}

func (t *taskHandler) HandleMessageStream(ctx context.Context, task string, contextID string) (<-chan autogen_client.Event, error) {
	if contextID != "" {
		session, err := t.client.GetSession(contextID, common.GetGlobalUserID())
		if err != nil {
			if errors.Is(err, autogen_client.NotFoundError) {
				session, err = t.client.CreateSession(&autogen_client.CreateSession{
					Name:   contextID,
					UserID: common.GetGlobalUserID(),
				})
				if err != nil {
					return nil, fmt.Errorf("failed to create session: %w", err)
				}
			} else {
				return nil, fmt.Errorf("failed to get session: %w", err)
			}
		}

		stream, err := t.client.InvokeSessionStream(session.ID, common.GetGlobalUserID(), &autogen_client.InvokeRequest{
			Task:       task,
			TeamConfig: t.team.Component,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}

		events := make(chan autogen_client.Event)
		go func() {
			defer close(events)
			for event := range stream {
				parsedEvent, err := autogen_client.ParseEvent(event.Data)
				if err != nil {
					log.Printf("failed to parse event: %v", err)
					continue
				}
				events <- parsedEvent
			}
		}()

		return events, nil
	} else {

		stream, err := t.client.InvokeTaskStream(&autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: t.team.Component,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}

		events := make(chan autogen_client.Event, 10)
		go func() {
			defer close(events)
			for event := range stream {
				parsedEvent, err := autogen_client.ParseEvent(event.Data)
				if err != nil {
					log.Printf("failed to parse event: %v", err)
					continue
				}
				events <- parsedEvent
			}
		}()

		return events, nil
	}
}
