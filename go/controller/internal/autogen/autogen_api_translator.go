package autogen

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	GlobalUserID = "admin@kagent.dev"

	// suffix applied to all system prompts:
	defaultSystemMessageSuffix = `
# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and friendly
    - If you don't know how to answer the question DO NOT make things up, tell the user "Sorry, I don't know how to answer that" and ask them to clarify the question further
    - Do not delete the original Deployment until the user explicitly confirms that the Rollout is ready to take over production traffic.


# Response format:
    - ALWAYS format your response as Markdown
    - Your response will include a summary of actions you took and an explanation of the result
    - If you created any artifacts such as files or resources, you will include those in your response as well`
)

var (
	// hard-coded array of tools that require a model client
	// this is automatically populated from the parent agent's model client
	toolsProvidersRequiringModelClient = []string{
		"kagent.tools.prometheus.GeneratePromQLTool",
		"kagent.tools.k8s.GenerateResourceTool",
	}
)

type ApiTranslator interface {
	TranslateGroupChatForTeam(
		ctx context.Context,
		team *v1alpha1.Team,
	) (*autogen_client.Team, error)

	TranslateGroupChatForAgent(
		ctx context.Context,
		team *v1alpha1.Agent,
	) (*autogen_client.Team, error)

	TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*autogen_client.ToolServer, error)
}

type apiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
}

func (a *apiTranslator) TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*autogen_client.ToolServer, error) {
	// provder = "kagent.tool_servers.StdioMcpToolServer" || "kagent.tool_servers.SseMcpToolServer"
	provider, toolServerConfig, err := translateToolServerConfig(toolServer.Spec.Config)
	if err != nil {
		return nil, err
	}

	return &autogen_client.ToolServer{
		UserID: GlobalUserID,
		Component: api.Component{
			Provider:      provider,
			ComponentType: "tool_server",
			Version:       1,
			Description:   toolServer.Spec.Description,
			Label:         toolServer.Name,
			Config:        api.MustToConfig(toolServerConfig),
		},
	}, nil
}

func translateToolServerConfig(config v1alpha1.ToolServerConfig) (string, *api.ToolServerConfig, error) {
	switch {
	case config.Stdio != nil:
		return "kagent.tool_servers.StdioMcpToolServer", &api.ToolServerConfig{
			StdioMcpServerConfig: &api.StdioMcpServerConfig{
				Command: config.Stdio.Command,
				Args:    config.Stdio.Args,
				Env:     config.Stdio.Env,
			},
		}, nil
	case config.Sse != nil:
		headers, err := convertMapFromAnytype(config.Sse.Headers)
		if err != nil {
			return "", nil, err
		}
		timeout, err := convertDurationToSeconds(config.Sse.Timeout)
		if err != nil {
			return "", nil, err
		}
		sseReadTimeout, err := convertDurationToSeconds(config.Sse.SseReadTimeout)
		if err != nil {
			return "", nil, err
		}

		return "kagent.tool_servers.SseMcpToolServer", &api.ToolServerConfig{
			SseMcpServerConfig: &api.SseMcpServerConfig{
				URL:            config.Sse.URL,
				Headers:        headers,
				Timeout:        timeout,
				SseReadTimeout: sseReadTimeout,
			},
		}, nil
	}

	return "", nil, fmt.Errorf("unsupported tool server config")
}

func convertDurationToSeconds(timeout string) (int, error) {
	if timeout == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return 0, err
	}
	return int(d.Seconds()), nil
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
	modelConfig := a.defaultModelConfig
	// Use the provided model config if set, otherwise use the default one
	if agent.Spec.ModelConfigRef != "" {
		modelConfig = types.NamespacedName{
			Name:      agent.Spec.ModelConfigRef,
			Namespace: agent.Namespace,
		}
	}
	if err := a.kube.Get(ctx, modelConfig, &v1alpha1.ModelConfig{}); err != nil {
		return nil, err
	}

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
			ModelConfig:          modelConfig.Name,
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

	modelClientWithStreaming, err := createModelClientForProvider(modelConfig, modelApiKey, true)
	if err != nil {
		return nil, err
	}

	modelClientWithoutStreaming, err := createModelClientForProvider(modelConfig, modelApiKey, false)
	if err != nil {
		return nil, err
	}

	modelContext := &api.Component{
		Provider:      "autogen_core.model_context.UnboundedChatCompletionContext",
		ComponentType: "chat_completion_context",
		Version:       1,
		Description:   "An unbounded chat completion context that keeps a view of the all the messages.",
		Label:         "UnboundedChatCompletionContext",
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

		if topLevelTeam {
			participant, err := a.translateTaskAgent(
				ctx,
				agent,
				modelContext,
			)
			if err != nil {
				return nil, err
			}
			participants = append(participants, participant)
		} else {
			participant, err := translateAssistantAgent(
				ctx,
				a.kube,
				agent.Name,
				agent.Namespace,
				agent.Spec,
				modelClientWithStreaming,
				modelClientWithoutStreaming,
				modelContext,
			)
			if err != nil {
				return nil, err
			}

			participants = append(participants, participant)
		}
	}

	//  add user proxy agent to top level
	if topLevelTeam {
		participants = append(participants, userProxyAgent)
	}

	if swarmTeamConfig != nil {
		planningAgent := MakeBuiltinPlanningAgent(
			"planning_agent",
			participants,
			modelClientWithStreaming,
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
			Version:       1,
			Description:   team.Spec.Description,
			Config: api.MustToConfig(&api.RoundRobinGroupChatConfig{
				CommonTeamConfig: commonTeamConfig,
			}),
		}
	} else if selectorTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.SelectorGroupChat",
			ComponentType: "team",
			Version:       1,
			Description:   team.Spec.Description,
			Config: api.MustToConfig(&api.SelectorGroupChatConfig{
				CommonTeamConfig: commonTeamConfig,
				SelectorPrompt:   selectorTeamConfig.SelectorPrompt,
			}),
		}
	} else if magenticOneTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.MagenticOneGroupChat",
			ComponentType: "team",
			Version:       1,
			Description:   team.Spec.Description,
			Config: api.MustToConfig(&api.MagenticOneGroupChatConfig{
				CommonTeamConfig:  commonTeamConfig,
				MaxStalls:         magenticOneTeamConfig.MaxStalls,
				FinalAnswerPrompt: magenticOneTeamConfig.FinalAnswerPrompt,
			}),
		}
	} else if swarmTeamConfig != nil {
		teamConfig = &api.Component{
			Provider:      "autogen_agentchat.teams.SwarmTeam",
			ComponentType: "team",
			Version:       1,
			Description:   team.Spec.Description,
			Config: api.MustToConfig(&api.SwarmTeamConfig{
				CommonTeamConfig: commonTeamConfig,
			}),
		}
	} else {
		return nil, fmt.Errorf("no team config specified")
	}

	teamConfig.Label = team.Name

	return &autogen_client.Team{
		Component: teamConfig,
		BaseObject: autogen_client.BaseObject{
			UserID: GlobalUserID, // always use global id
		},
	}, nil
}

// internally we convert all agents to a society-of-mind agent
func (a *apiTranslator) translateTaskAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	modelContext *api.Component,
) (*api.Component, error) {
	// generate an internal round robin "team" for the society of mind agent
	meta := agent.ObjectMeta.DeepCopy()
	// This is important so we don't output this message in the CLI/UI
	meta.Name += "-society-of-mind-wrapper"
	team := &v1alpha1.Team{
		ObjectMeta: *meta,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Team",
			APIVersion: "kagent.dev/v1alpha1",
		},
		Spec: v1alpha1.TeamSpec{
			Participants:         []string{agent.Name},
			Description:          agent.Spec.Description,
			ModelConfig:          agent.Spec.ModelConfigRef,
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

	agentName := convertToPythonIdentifier(team.Name)
	return &api.Component{
		Provider:      "kagent.agents.TaskAgent",
		ComponentType: "agent",
		Version:       1,
		Label:         agentName,
		Description:   "An agent that runs a team of agents",
		Config: api.MustToConfig(&api.TaskAgentConfig{
			Team:         societyOfMindTeam.Component,
			Name:         agentName,
			ModelContext: modelContext,
		}),
	}, nil
}

func translateAssistantAgent(
	ctx context.Context,
	kube client.Client,
	agentName string,
	agentNamespace string,
	agentSpec v1alpha1.AgentSpec,
	modelClientWithStreaming *api.Component,
	modelClientWithoutStreaming *api.Component,
	modelContext *api.Component,
) (*api.Component, error) {

	tools := []*api.Component{}
	for _, tool := range agentSpec.Tools {
		switch {
		case tool.Inline != nil:
			autogenTool, err := translateBuiltinTool(
				modelClientWithoutStreaming,
				tool.Inline,
			)
			if err != nil {
				return nil, err
			}
			tools = append(tools, autogenTool)
		case tool.McpServer != nil:
			for _, toolName := range tool.McpServer.ToolNames {
				autogenTool, err := translateToolServerTool(
					ctx,
					kube,
					tool.McpServer.ToolServer,
					toolName,
					agentNamespace,
				)
				if err != nil {
					return nil, err
				}
				tools = append(tools, autogenTool)
			}
		default:
			return nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}

	sysMsg := agentSpec.SystemMessage + "\n" + defaultSystemMessageSuffix
	if agentSpec.SystemMessage == "" {
		sysMsg = ""
	}

	return &api.Component{
		Provider:      "autogen_agentchat.agents.AssistantAgent",
		ComponentType: "agent",
		Version:       1,
		Description:   agentSpec.Description,
		Config: api.MustToConfig(&api.AssistantAgentConfig{
			Name:         convertToPythonIdentifier(agentName),
			ModelClient:  modelClientWithStreaming,
			Tools:        tools,
			ModelContext: modelContext,
			Description:  agentSpec.Description,
			// TODO(ilackarms): convert to non-ptr with omitempty?
			SystemMessage:         sysMsg,
			ReflectOnToolUse:      false,
			ModelClientStream:     true,
			ToolCallSummaryFormat: "\nTool: \n{tool_name}\n\nArguments:\n\n{arguments}\n\nResult: \n{result}\n",
		}),
	}, nil
}

func translateBuiltinTool(
	modelClient *api.Component,
	tool *v1alpha1.InlineTool,
) (*api.Component, error) {

	toolConfig, err := convertMapFromAnytype(tool.Config)
	if err != nil {
		return nil, err
	}
	// special case where we put the model client in the tool config
	if toolNeedsModelClient(tool.Provider) {
		if err := addModelClientToConfig(modelClient, &toolConfig); err != nil {
			return nil, fmt.Errorf("failed to add model client to tool config: %v", err)
		}
	}

	providerParts := strings.Split(tool.Provider, ".")
	toolLabel := providerParts[len(providerParts)-1]

	return &api.Component{
		Provider:      tool.Provider,
		Description:   tool.Description,
		ComponentType: "tool",
		Version:       1,
		Config:        toolConfig,
		Label:         toolLabel,
	}, nil
}

func translateToolServerTool(
	ctx context.Context,
	kube client.Client,
	toolServerName string,
	toolName string,
	agentNamespace string,
) (*api.Component, error) {
	toolServer := &v1alpha1.ToolServer{}
	err := fetchObjKube(
		ctx,
		kube,
		toolServer,
		toolServerName,
		agentNamespace,
	)
	if err != nil {
		return nil, err
	}

	// requires the tool to have been discovered
	for _, discoveredTool := range toolServer.Status.DiscoveredTools {
		if discoveredTool.Name == toolName {
			return convertComponent(discoveredTool.Component)
		}
	}

	return nil, fmt.Errorf("tool %v not found in discovered tools in ToolServer %v", toolName, toolServer.Name)
}

func convertComponent(component v1alpha1.Component) (*api.Component, error) {
	config, err := convertMapFromAnytype(component.Config)
	if err != nil {
		return nil, err
	}
	return &api.Component{
		Provider:         component.Provider,
		ComponentType:    component.ComponentType,
		Version:          component.Version,
		ComponentVersion: component.ComponentVersion,
		Description:      component.Description,
		Label:            component.Label,
		Config:           config,
	}, nil
}

func convertMapFromAnytype(config map[string]v1alpha1.AnyType) (map[string]interface{}, error) {
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
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.MaxMessageTerminationConfig{
				MaxMessages: terminationCondition.MaxMessageTermination.MaxMessages,
			}),
		}, nil
	case terminationCondition.TextMentionTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.TextMentionTermination",
			ComponentType: "termination",
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.TextMentionTerminationConfig{
				Text: terminationCondition.TextMentionTermination.Text,
			}),
		}, nil
	case terminationCondition.TextMessageTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.TextMessageTermination",
			ComponentType: "termination",
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.TextMessageTerminationConfig{
				Source: terminationCondition.TextMessageTermination.Source,
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
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.OrTerminationConfig{
				Conditions: conditions,
			}),
		}, nil
	case terminationCondition.StopMessageTermination != nil:
		return &api.Component{
			Provider:      "autogen_agentchat.conditions.StopMessageTermination",
			ComponentType: "termination",
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.StopMessageTerminationConfig{}),
			Label:  "StopMessageTermination",
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

func toolNeedsModelClient(provider string) bool {
	for _, p := range toolsProvidersRequiringModelClient {
		if p == provider {
			return true
		}
	}
	return false
}

func addModelClientToConfig(
	modelClient *api.Component,
	toolConfig *map[string]interface{},
) error {
	if *toolConfig == nil {
		*toolConfig = make(map[string]interface{})
	}

	cfg, err := modelClient.ToConfig()
	if err != nil {
		return err
	}

	(*toolConfig)["model_client"] = cfg
	return nil
}

// createModelClientForProvider creates a model client component based on the model provider
func createModelClientForProvider(modelConfig *v1alpha1.ModelConfig, apiKey []byte, includeUsage bool) (*api.Component, error) {
	switch modelConfig.Spec.Provider {
	case v1alpha1.Anthropic:
		config := &api.AnthropicClientConfiguration{
			BaseAnthropicClientConfiguration: api.BaseAnthropicClientConfiguration{
				APIKey: string(apiKey),
				Model:  modelConfig.Spec.Model,
			},
		}

		// Add provider-specific configurations
		if modelConfig.Spec.Anthropic != nil {
			anthropicConfig := modelConfig.Spec.Anthropic

			config.BaseURL = anthropicConfig.BaseURL
			if anthropicConfig.MaxTokens > 0 {
				config.MaxTokens = anthropicConfig.MaxTokens
			}

			if anthropicConfig.Temperature != "" {
				temp, err := strconv.ParseFloat(anthropicConfig.Temperature, 64)
				if err == nil {
					config.Temperature = temp
				}
			}

			if anthropicConfig.TopP != "" {
				topP, err := strconv.ParseFloat(anthropicConfig.TopP, 64)
				if err == nil {
					config.TopP = topP
				}
			}

			config.TopK = anthropicConfig.TopK
		}

		// Convert to map
		configMap, err := config.ToConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to convert Anthropic config: %w", err)
		}

		return &api.Component{
			Provider:      "autogen_ext.models.anthropic.AnthropicChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        configMap,
		}, nil

	case v1alpha1.AzureOpenAI:
		config := &api.AzureOpenAIClientConfig{
			BaseOpenAIClientConfig: api.BaseOpenAIClientConfig{
				Model:  modelConfig.Spec.Model,
				APIKey: string(apiKey),
			},
			Stream: true,
		}

		if includeUsage {
			config.StreamOptions = &api.StreamOptions{
				IncludeUsage: true,
			}
		}

		// Add provider-specific configurations
		if modelConfig.Spec.AzureOpenAI != nil {
			azureConfig := modelConfig.Spec.AzureOpenAI

			config.AzureEndpoint = azureConfig.Endpoint
			config.APIVersion = azureConfig.APIVersion
			config.AzureDeployment = azureConfig.DeploymentName
			config.AzureADToken = azureConfig.AzureADToken

			if azureConfig.Temperature != "" {
				temp, err := strconv.ParseFloat(azureConfig.Temperature, 64)
				if err == nil {
					config.Temperature = temp
				}
			}

			if azureConfig.TopP != "" {
				topP, err := strconv.ParseFloat(azureConfig.TopP, 64)
				if err == nil {
					config.TopP = topP
				}
			}
		}

		return &api.Component{
			Provider:      "autogen_ext.models.openai.AzureOpenAIChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	case v1alpha1.OpenAI:
		config := &api.OpenAIClientConfig{
			BaseOpenAIClientConfig: api.BaseOpenAIClientConfig{
				Model:  modelConfig.Spec.Model,
				APIKey: string(apiKey),
			},
		}

		if includeUsage {
			config.StreamOptions = &api.StreamOptions{
				IncludeUsage: true,
			}
		}

		// Add provider-specific configurations
		if modelConfig.Spec.OpenAI != nil {
			openAIConfig := modelConfig.Spec.OpenAI

			if openAIConfig.BaseURL != "" {
				config.BaseURL = &openAIConfig.BaseURL
			}

			if openAIConfig.Organization != "" {
				config.Organization = &openAIConfig.Organization
			}

			if openAIConfig.MaxTokens > 0 {
				config.MaxTokens = openAIConfig.MaxTokens
			}

			if openAIConfig.Temperature != "" {
				temp, err := strconv.ParseFloat(openAIConfig.Temperature, 64)
				if err == nil {
					config.Temperature = temp
				}
			}

			if openAIConfig.TopP != "" {
				topP, err := strconv.ParseFloat(openAIConfig.TopP, 64)
				if err == nil {
					config.TopP = topP
				}
			}

			if openAIConfig.FrequencyPenalty != "" {
				freqP, err := strconv.ParseFloat(openAIConfig.FrequencyPenalty, 64)
				if err == nil {
					config.FrequencyPenalty = freqP
				}
			}

			if openAIConfig.PresencePenalty != "" {
				presP, err := strconv.ParseFloat(openAIConfig.PresencePenalty, 64)
				if err == nil {
					config.PresencePenalty = presP
				}
			}
		}

		return &api.Component{
			Provider:      "autogen_ext.models.openai.OpenAIChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	case v1alpha1.Ollama:
		config := &api.OllamaClientConfiguration{
			OllamaCreateArguments: api.OllamaCreateArguments{
				Model: modelConfig.Spec.Model,
				Host:  modelConfig.Spec.Ollama.Host,
			},
			FollowRedirects: true,
		}

		if modelConfig.Spec.Ollama != nil {
			ollamaConfig := modelConfig.Spec.Ollama

			if ollamaConfig.Options != nil {
				config.Options = ollamaConfig.Options
			}
		}

		return &api.Component{
			Provider:      "autogen_ext.models.ollama.OllamaChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported model provider: %s", modelConfig.Spec.Provider)
	}
}
