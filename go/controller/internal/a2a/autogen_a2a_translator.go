package a2a

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/a2a"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"gorm.io/gorm"
	"k8s.io/utils/ptr"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// translates A2A Handlers from autogen agents/teams
type AutogenA2ATranslator interface {
	TranslateHandlerForAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
		autogenTeam *database.Agent,
	) (*a2a.A2AHandlerParams, error)
}

type autogenA2ATranslator struct {
	a2aBaseUrl    string
	autogenClient autogen_client.Client
	dbService     database.Client
}

var _ AutogenA2ATranslator = &autogenA2ATranslator{}

func NewAutogenA2ATranslator(
	a2aBaseUrl string,
	autogenClient autogen_client.Client,
	dbService database.Client,
) AutogenA2ATranslator {
	return &autogenA2ATranslator{
		a2aBaseUrl:    a2aBaseUrl,
		autogenClient: autogenClient,
		dbService:     dbService,
	}
}

func (a *autogenA2ATranslator) TranslateHandlerForAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	autogenTeam *database.Agent,
) (*a2a.A2AHandlerParams, error) {
	card, err := a.translateCardForAgent(agent)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}

	handler, err := a.makeHandlerForTeam(autogenTeam, a.dbService)
	if err != nil {
		return nil, err
	}

	return &a2a.A2AHandlerParams{
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
	autogenTeam *database.Agent,
	dbService database.Client,
) (a2a.MessageHandler, error) {
	return &taskHandler{
		team:      autogenTeam,
		client:    a.autogenClient,
		dbService: dbService,
	}, nil
}

type taskHandler struct {
	team      *database.Agent
	client    autogen_client.Client
	dbService database.Client
}

func (t *taskHandler) HandleMessage(ctx context.Context, task string, contextID *string) ([]autogen_client.Event, error) {
	var taskResult *autogen_client.TaskResult
	if contextID != nil && *contextID != "" {
		log.Printf("Handling message for session %s", *contextID)
		session, err := t.getOrCreateSession(ctx, *contextID)
		if err != nil {
			return nil, fmt.Errorf("failed to get session: %w", err)
		}

		messages, err := t.prepareMessages(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare messages: %w", err)
		}

		// Debug logging
		log.Printf("DEBUG: About to call InvokeTask with Messages - len: %d, nil: %v", len(messages), messages == nil)

		resp, err := t.client.InvokeTask(ctx, &autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: &t.team.Component,
			Messages:   messages,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}
		taskResult = &resp.TaskResult
	} else {

		resp, err := t.client.InvokeTask(ctx, &autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: &t.team.Component,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to invoke task: %w", err)
		}
		taskResult = &resp.TaskResult
	}

	return taskResult.Messages, nil
}

// getOrCreateSession gets a session from the database or creates a new one if it doesn't exist
func (t *taskHandler) getOrCreateSession(ctx context.Context, contextID string) (*database.Session, error) {
	session, err := t.dbService.GetSession(contextID, common.GetGlobalUserID())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			session = &database.Session{
				ID:      contextID,
				UserID:  common.GetGlobalUserID(),
				AgentID: &t.team.ID,
				Name:    contextID,
			}
			err := t.dbService.CreateSession(session)
			if err != nil {
				return nil, fmt.Errorf("failed to create session: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get session: %w", err)
		}
	}
	return session, nil
}

func (t *taskHandler) prepareMessages(ctx context.Context, session *database.Session) ([]autogen_client.Event, error) {
	messages, err := t.dbService.ListMessagesForSession(session.ID, common.GetGlobalUserID())
	if err != nil {
		return nil, fmt.Errorf("failed to get messages for session: %w", err)
	}

	log.Printf("Retrieved %d messages for session %s", len(messages), session.ID)

	parsedMessages, err := database.ParseMessages(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to parse messages: %w", err)
	}

	autogenEvents, err := utils.ConvertMessagesToAutogenEvents(parsedMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages to autogen events: %w", err)
	}
	return autogenEvents, nil
}

func (t *taskHandler) HandleMessageStream(ctx context.Context, task string, contextID *string) (<-chan autogen_client.Event, error) {
	if contextID != nil && *contextID != "" {
		session, err := t.getOrCreateSession(ctx, *contextID)
		if err != nil {
			return nil, fmt.Errorf("failed to get session: %w", err)
		}

		messages, err := t.prepareMessages(ctx, session)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare messages: %w", err)
		}

		stream, err := t.client.InvokeTaskStream(ctx, &autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: &t.team.Component,
			Messages:   messages,
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

		stream, err := t.client.InvokeTaskStream(ctx, &autogen_client.InvokeTaskRequest{
			Task:       task,
			TeamConfig: &t.team.Component,
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
