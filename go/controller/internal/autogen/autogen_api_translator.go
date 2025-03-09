package autogen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const GlobalUserID = "admin@kagent.dev"

type ApiTranslator interface {
	TranslateGroupChatForTeam(
		ctx context.Context,
		team *v1alpha1.Team,
	) (*autogen_client.Team, error)

	TranslateGroupChatForAgent(
		ctx context.Context,
		team *v1alpha1.Agent,
	) (*autogen_client.Team, error)
}

type apiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
}

func NewAutogenApiTranslator(
	kube client.Client,
	defaultModelConfig types.NamespacedName,
) ApiTranslator {
	return &apiTranslator{
		kube:               kube,
		defaultModelConfig: defaultModelConfig,
	}
}

func (a *apiTranslator) TranslateGroupChatForAgent(ctx context.Context, agent *v1alpha1.Agent) (*autogen_client.Team, error) {
	// generate an internal round robin "team" for the individual agent
	team := &v1alpha1.Team{
		ObjectMeta: agent.ObjectMeta,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Team",
			APIVersion: "kagent.dev/v1alpha1",
		},
		Spec: v1alpha1.TeamSpec{
			Participants:         []string{agent.Name},
			Description:          agent.Spec.Description,
			RoundRobinTeamConfig: &v1alpha1.RoundRobinTeamConfig{},
			TerminationCondition: v1alpha1.TerminationCondition{
				StopMessageTermination: &v1alpha1.StopMessageTermination{},
			},
		},
	}

	return a.TranslateGroupChatForTeam(ctx, team)
}

func (a *apiTranslator) TranslateGroupChatForTeam(
	ctx context.Context,
	team *v1alpha1.Team,
) (*autogen_client.Team, error) {
	return a.translateGroupChatForTeam(ctx, team, true)
}

func (a *apiTranslator) translateGroupChatForTeam(
	ctx context.Context,
	team *v1alpha1.Team,
	topLevelTeam bool,
) (*autogen_client.Team, error) {
	// get model config
	roundRobinTeamConfig := team.Spec.RoundRobinTeamConfig
	selectorTeamConfig := team.Spec.SelectorTeamConfig
	magenticOneTeamConfig := team.Spec.MagenticOneTeamConfig
	swarmTeamConfig := team.Spec.SwarmTeamConfig

	modelConfigRef := a.defaultModelConfig
	if team.Spec.ModelConfig != "" {
		modelConfigRef = types.NamespacedName{
			Name:      team.Spec.ModelConfig,
			Namespace: team.Namespace,
		}
	}
	modelConfig := &v1alpha1.ModelConfig{}
	err := fetchObjKube(
		ctx,
		a.kube,
		modelConfig,
		modelConfigRef.Name,
		modelConfigRef.Namespace,
	)
	if err != nil {
		return nil, err
	}

	// get model api key
	modelApiKeySecret := &v1.Secret{}
	err = fetchObjKube(
		ctx,
		a.kube,
		modelApiKeySecret,
		modelConfig.Spec.APIKeySecretName,
		modelConfig.Namespace,
	)
	if err != nil {
		return nil, err
	}

	if modelApiKeySecret.Data == nil {
		return nil, fmt.Errorf("model api key secret data not found")
	}

	modelApiKey, ok := modelApiKeySecret.Data[modelConfig.Spec.APIKeySecretKey]
	if !ok {
		return nil, fmt.Errorf("model api key not found")
	}

	modelClient := &api.Component{
		Provider:      "autogen_ext.models.openai.OpenAIChatCompletionClient",
		ComponentType: "model",
		Version:       makePtr(1),
		//ComponentVersion: 1,
		Config: api.MustToConfig(&api.OpenAIClientConfig{
			BaseOpenAIClientConfig: api.BaseOpenAIClientConfig{
				Model:  modelConfig.Spec.Model,
				APIKey: makePtr(string(modelApiKey)),
			},
		}),
	}
	modelContext := &api.Component{
		Provider:      "autogen_core.model_context.UnboundedChatCompletionContext",
		ComponentType: "chat_completion_context",
		Version:       makePtr(1),
		Description:   makePtr("An unbounded chat completion context that keeps a view of the all the messages."),
		Label:         makePtr("UnboundedChatCompletionContext"),
		Config:        map[string]interface{}{},
	}

	var participants []*api.Component
	for _, agentName := range team.Spec.Participants {
		agent := &v1alpha1.Agent{}
		err := fetchObjKube(
			ctx,
			a.kube,
			agent,
			agentName,
			team.Namespace,
		)
		if err != nil {
			return nil, err
		}

		var participant *api.Component
		if topLevelTeam {
			participant, err = a.translateTaskAgent(
				ctx,
				agent,
				modelContext,
			)
		} else {
			participant, err = translateAssistantAgent(
				agent.Name,
				agent.Spec,
				modelClient,
				modelContext,
			)
		}
		if err != nil {
			return nil, err
		}

		participants = append(participants, participant)
	}

	//  add user proxy agent to top level
	if topLevelTeam {
		participants = append(participants, userProxyAgent)
	}

	if swarmTeamConfig != nil {
		planningAgent := MakeBuiltinPlanningAgent(
			"planning_agent",
			participants,
			modelClient,
		)
		// prepend builtin planning agent when using swarm mode
		participants = append(
			[]*api.Component{planningAgent},
			participants...,
		)
	}

	terminationCondition, err := translateTerminationCondition(team.Spec.TerminationCondition)
	if err != nil {
		return nil, err
	}

	commonTeamConfig := api.CommonTeamConfig{
		Participants: participants,
		Termination:  terminationCondition,
	}

	var teamConfig *api.Component
	if roundRobinTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.RoundRobinGroupChat",
			ComponentType: "team",
			Version:       makePtr(1),
			Description:   makePtr(team.Spec.Description),
			Config: api.MustToConfig(&api.RoundRobinGroupChatConfig{
				CommonTeamConfig: commonTeamConfig,
			}),
		}
	} else if selectorTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.SelectorGroupChat",
			ComponentType: "team",
			Version:       makePtr(1),
			Description:   makePtr(team.Spec.Description),
			Config: api.MustToConfig(&api.SelectorGroupChatConfig{
				CommonTeamConfig: commonTeamConfig,
				SelectorPrompt:   makePtr(selectorTeamConfig.SelectorPrompt),
			}),
		}
	} else if magenticOneTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.MagenticOneGroupChat",
			ComponentType: "team",
			Version:       makePtr(1),
			Description:   makePtr(team.Spec.Description),
			Config: api.MustToConfig(&api.MagenticOneGroupChatConfig{
				CommonTeamConfig:  commonTeamConfig,
				MaxStalls:         makePtr(magenticOneTeamConfig.MaxStalls),
				FinalAnswerPrompt: makePtr(magenticOneTeamConfig.FinalAnswerPrompt),
			}),
		}
	} else if swarmTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.SwarmTeam",
			ComponentType: "team",
			Version:       makePtr(1),
			Description:   makePtr(team.Spec.Description),
			Config: api.MustToConfig(&api.SwarmTeamConfig{
				CommonTeamConfig: commonTeamConfig,
			}),
		}
	} else {
		return nil, fmt.Errorf("no team config specified")
	}

	teamConfig.Label = makePtr(team.Name)

	return &autogen_client.Team{
		UserID:    GlobalUserID, // always use global id
		Component: teamConfig,
	}, nil
}

// internally we convert all agents to a society-of-mind agent
func (a *apiTranslator) translateTaskAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	modelContext *api.Component,
) (*api.Component, error) {
	// generate an internal round robin "team" for the society of mind agent
	team := &v1alpha1.Team{
		ObjectMeta: agent.ObjectMeta,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Team",
			APIVersion: "kagent.dev/v1alpha1",
		},
		Spec: v1alpha1.TeamSpec{
			Participants:         []string{agent.Name},
			Description:          agent.Spec.Description,
			RoundRobinTeamConfig: &v1alpha1.RoundRobinTeamConfig{},
			TerminationCondition: v1alpha1.TerminationCondition{
				TextMessageTermination: &v1alpha1.TextMessageTermination{
					Source: convertToPythonIdentifier(agent.Name),
				},
			},
		},
	}

	societyOfMindTeam, err := a.translateGroupChatForTeam(ctx, team, false)
	if err != nil {
		return nil, err
	}

	return &api.Component{
		Provider:      "kagent.agents.TaskAgent",
		ComponentType: "agent",
		Version:       makePtr(1),
		Label:         makePtr("society_of_mind_agent"),
		Description:   makePtr("An agent that runs a team of agents"),
		Config: api.MustToConfig(&api.TaskAgentConfig{
			Team:         societyOfMindTeam.Component,
			Name:         "society_of_mind_agent",
			ModelContext: modelContext,
		}),
	}, nil
}

func translateAssistantAgent(
	agentName string,
	agentSpec v1alpha1.AgentSpec,
	modelClient *api.Component,
	modelContext *api.Component,
) (*api.Component, error) {

	var tools []*api.Component
	for _, tool := range agentSpec.Tools {
		toolConfig, err := convertToolConfig(tool.Config)
		if err != nil {
			return nil, err
		}

		providerParts := strings.Split(tool.Provider, ".")
		toolLabel := providerParts[len(providerParts)-1]

		tool := &api.Component{
			Provider:      tool.Provider,
			ComponentType: "tool",
			Version:       makePtr(1),
			Config:        api.GenericToolConfig(toolConfig),
			Label:         makePtr(toolLabel),
		}

		tools = append(tools, tool)
	}

	sysMsgPtr := makePtr(agentSpec.SystemMessage)
	if agentSpec.SystemMessage == "" {
		sysMsgPtr = nil
	}

	return &api.Component{
		Provider:      "autogen_agentchat.agents.AssistantAgent",
		ComponentType: "agent",
		Version:       makePtr(1),
		Description:   makePtr(agentSpec.Description),
		Config: api.MustToConfig(&api.AssistantAgentConfig{
			Name:         convertToPythonIdentifier(agentName),
			ModelClient:  modelClient,
			Tools:        tools,
			ModelContext: modelContext,
			Description:  agentSpec.Description,
			// TODO(ilackarms): convert to non-ptr with omitempty?
			SystemMessage:         sysMsgPtr,
			ReflectOnToolUse:      false,
			ToolCallSummaryFormat: "{result}",
		}),
	}, nil
}

func convertToolConfig(config map[string]v1alpha1.AnyType) (map[string]interface{}, error) {
	// convert to map[string]interface{} to allow kubebuilder schemaless validation
	// see https://github.com/kubernetes-sigs/controller-tools/issues/636 for more info
	// must unmarshal to interface{} to avoid json.RawMessage
	convertedConfig := make(map[string]interface{})

	if config == nil {
		return convertedConfig, nil
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(raw, &convertedConfig)
	if err != nil {
		return nil, err
	}

	return convertedConfig, nil
}

func makePtr[T any](v T) *T {
	return &v
}

func translateTerminationCondition(terminationCondition v1alpha1.TerminationCondition) (*api.Component, error) {
	// ensure only one termination condition is set
	var conditionsSet int
	if terminationCondition.MaxMessageTermination != nil {
		conditionsSet++
	}
	if terminationCondition.TextMentionTermination != nil {
		conditionsSet++
	}
	if terminationCondition.OrTermination != nil {
		conditionsSet++
	}
	if terminationCondition.StopMessageTermination != nil {
		conditionsSet++
	}
	if terminationCondition.TextMessageTermination != nil {
		conditionsSet++
	}
	if conditionsSet != 1 {
		return nil, fmt.Errorf("exactly one termination condition must be set")
	}

	switch {
	case terminationCondition.MaxMessageTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.MaxMessageTermination",
			ComponentType: "termination",
			Version:       makePtr(1),
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.MaxMessageTerminationConfig{
				MaxMessages: makePtr(terminationCondition.MaxMessageTermination.MaxMessages),
			}),
		}, nil
	case terminationCondition.TextMentionTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.TextMentionTermination",
			ComponentType: "termination",
			Version:       makePtr(1),
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.TextMentionTerminationConfig{
				Text: makePtr(terminationCondition.TextMentionTermination.Text),
			}),
		}, nil
	case terminationCondition.TextMessageTermination != nil:
		return &api.Component{
			Provider:      "kagent.terminations.TextMessageTermination",
			ComponentType: "termination",
			Version:       makePtr(1),
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.TextMessageTerminationConfig{
				Source: makePtr(terminationCondition.TextMessageTermination.Source),
			}),
		}, nil
	case terminationCondition.OrTermination != nil:
		var conditions []*api.Component
		for _, c := range terminationCondition.OrTermination.Conditions {
			subConditon := v1alpha1.TerminationCondition{
				MaxMessageTermination:  c.MaxMessageTermination,
				TextMentionTermination: c.TextMentionTermination,
			}

			condition, err := translateTerminationCondition(subConditon)
			if err != nil {
				return nil, err
			}
			conditions = append(conditions, condition)
		}
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.OrTerminationCondition",
			ComponentType: "termination",
			Version:       makePtr(1),
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.OrTerminationConfig{
				Conditions: conditions,
			}),
		}, nil
	case terminationCondition.StopMessageTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.StopMessageTermination",
			ComponentType: "termination",
			Version:       makePtr(1),
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.StopMessageTerminationConfig{}),
			Label:  makePtr("StopMessageTermination"),
		}, nil
	}

	return nil, fmt.Errorf("unsupported termination condition")
}

func fetchObjKube(ctx context.Context, kube client.Client, obj client.Object, objName, objNamespace string) error {
	err := kube.Get(ctx, types.NamespacedName{
		Name:      objName,
		Namespace: objNamespace,
	}, obj)
	if err != nil {
		return err
	}
	return nil
}

func convertToPythonIdentifier(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}
