package autogen

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// hard-coded array of tools that require a model client
	// this is automatically populated from the parent agent's model client
	toolsProvidersRequiringModelClient = []string{
		"kagent.tools.prometheus.GeneratePromQLTool",
		"kagent.tools.k8s.GenerateResourceTool",
	}
	toolsProvidersRequiringOpenaiApiKey = []string{
		"kagent.tools.docs.QueryTool",
	}

	log = ctrllog.Log.WithName("autogen")
)

type ApiTranslator interface {
	TranslateGroupChatForTeam(
		ctx context.Context,
		team *v1alpha1.Team,
	) (*autogen_client.Team, error)

	TranslateGroupChatForAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
	) (*autogen_client.Team, error)

	TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*autogen_client.ToolServer, error)
}

type apiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
}

func (a *apiTranslator) TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*autogen_client.ToolServer, error) {
	// provder = "kagent.tool_servers.StdioMcpToolServer" || "kagent.tool_servers.SseMcpToolServer"
	provider, toolServerConfig, err := a.translateToolServerConfig(ctx, toolServer.Spec.Config, toolServer.Namespace)
	if err != nil {
		return nil, err
	}

	return &autogen_client.ToolServer{
		UserID: common.GetGlobalUserID(),
		Component: api.Component{
			Provider:      provider,
			ComponentType: "tool_server",
			Version:       1,
			Description:   toolServer.Spec.Description,
			Label:         common.GetObjectRef(toolServer),
			Config:        api.MustToConfig(toolServerConfig),
		},
	}, nil
}

// resolveValueSource resolves a value from a ValueSource
func (a *apiTranslator) resolveValueSource(ctx context.Context, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	switch source.Type {
	case v1alpha1.ConfigMapValueSource:
		return a.getConfigMapValue(ctx, source, namespace)
	case v1alpha1.SecretValueSource:
		return a.getSecretValue(ctx, source, namespace)
	default:
		return "", fmt.Errorf("unknown value source type: %s", source.Type)
	}
}

// getConfigMapValue fetches a value from a ConfigMap
func (a *apiTranslator) getConfigMapValue(ctx context.Context, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	configMap := &corev1.ConfigMap{}
	err := common.GetObject(
		ctx,
		a.kube,
		configMap,
		source.ValueRef,
		namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to find ConfigMap for %s: %v", source.ValueRef, err)
	}

	value, exists := configMap.Data[source.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in ConfigMap %s/%s", source.Key, configMap.Namespace, configMap.Name)
	}
	return value, nil
}

// getSecretValue fetches a value from a Secret
func (a *apiTranslator) getSecretValue(ctx context.Context, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	secret := &corev1.Secret{}
	err := common.GetObject(
		ctx,
		a.kube,
		secret,
		source.ValueRef,
		namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to find Secret for %s: %v", source.ValueRef, err)
	}

	value, exists := secret.Data[source.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Secret %s/%s", source.Key, secret.Namespace, secret.Name)
	}
	return string(value), nil
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

func (a *apiTranslator) translateToolServerConfig(ctx context.Context, config v1alpha1.ToolServerConfig, namespace string) (string, *api.ToolServerConfig, error) {
	switch {
	case config.Stdio != nil:
		env := make(map[string]string)

		if config.Stdio.Env != nil {
			for k, v := range config.Stdio.Env {
				env[k] = v
			}
		}

		if len(config.Stdio.EnvFrom) > 0 {
			for _, envVar := range config.Stdio.EnvFrom {
				if envVar.ValueFrom != nil {
					value, err := a.resolveValueSource(ctx, envVar.ValueFrom, namespace)

					if err != nil {
						return "", nil, fmt.Errorf("failed to resolve environment variable %s: %v", envVar.Name, err)
					}

					env[envVar.Name] = value
				} else if envVar.Value != "" {
					env[envVar.Name] = envVar.Value
				}
			}
		}

		return "kagent.tool_servers.StdioMcpToolServer", &api.ToolServerConfig{
			StdioMcpServerConfig: &api.StdioMcpServerConfig{
				Command: config.Stdio.Command,
				Args:    config.Stdio.Args,
				Env:     env,
			},
		}, nil
	case config.Sse != nil:
		headers, err := convertMapFromAnytype(config.Sse.Headers)
		if err != nil {
			return "", nil, err
		}

		if len(config.Sse.HeadersFrom) > 0 {
			for _, header := range config.Sse.HeadersFrom {
				if header.ValueFrom != nil {
					value, err := a.resolveValueSource(ctx, header.ValueFrom, namespace)

					if err != nil {
						return "", nil, fmt.Errorf("failed to resolve header %s: %v", header.Name, err)
					}

					headers[header.Name] = value
				} else if header.Value != "" {
					headers[header.Name] = header.Value
				}
			}
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
	stream := true
	if agent.Spec.Stream != nil {
		stream = *agent.Spec.Stream
	}
	opts := defaultTeamOptions()
	opts.stream = stream

	return a.translateGroupChatForAgent(ctx, agent, opts, &tState{})
}

func (a *apiTranslator) TranslateGroupChatForTeam(
	ctx context.Context,
	team *v1alpha1.Team,
) (*autogen_client.Team, error) {
	return a.translateGroupChatForTeam(ctx, team, defaultTeamOptions(), &tState{})
}

type teamOptions struct {
	stream bool
}

const MAX_DEPTH = 10

type tState struct {
	// used to prevent infinite loops
	// The recursion limit is 10
	depth uint8
	// used to enforce DAG
	// The final member of the list will be the "parent" agent
	visitedAgents []string
}

func (s *tState) with(agent *v1alpha1.Agent) *tState {
	s.depth++
	s.visitedAgents = append(s.visitedAgents, agent.Name)
	return s
}

func (t *tState) isVisited(agentName string) bool {
	return slices.Contains(t.visitedAgents, agentName)
}

func defaultTeamOptions() *teamOptions {
	return &teamOptions{
		stream: true,
	}
}

func (a *apiTranslator) translateGroupChatForAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	opts *teamOptions,
	state *tState,
) (*autogen_client.Team, error) {
	simpleTeam, err := a.simpleRoundRobinTeam(ctx, agent)
	if err != nil {
		return nil, err
	}

	return a.translateGroupChatForTeam(ctx, simpleTeam, opts, state)
}

func (a *apiTranslator) translateGroupChatForTeam(
	ctx context.Context,
	team *v1alpha1.Team,
	opts *teamOptions,
	state *tState,
) (*autogen_client.Team, error) {
	// get model config
	roundRobinTeamConfig := team.Spec.RoundRobinTeamConfig
	selectorTeamConfig := team.Spec.SelectorTeamConfig
	magenticOneTeamConfig := team.Spec.MagenticOneTeamConfig
	swarmTeamConfig := team.Spec.SwarmTeamConfig

	modelConfigObj, err := common.GetModelConfig(
		ctx,
		a.kube,
		team,
		a.defaultModelConfig,
	)
	if err != nil {
		return nil, err
	}

	modelClientWithStreaming, err := a.createModelClientForProvider(ctx, modelConfigObj, true)
	if err != nil {
		return nil, err
	}

	modelClientWithoutStreaming, err := a.createModelClientForProvider(ctx, modelConfigObj, false)
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

	for _, agentRef := range team.Spec.Participants {
		agentObj := &v1alpha1.Agent{}
		if err := common.GetObject(
			ctx,
			a.kube,
			agentObj,
			agentRef,
			team.Namespace,
		); err != nil {
			return nil, err
		}

		participant, err := a.translateAssistantAgent(
			ctx,
			agentObj,
			modelConfigObj,
			modelClientWithStreaming,
			modelClientWithoutStreaming,
			modelContext,
			opts,
			state,
		)
		if err != nil {
			return nil, err
		}

		participants = append(participants, participant)
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

	teamConfig.Label = common.GetObjectRef(team)

	return &autogen_client.Team{
		Component: teamConfig,
		BaseObject: autogen_client.BaseObject{
			UserID: common.GetGlobalUserID(), // always use global id
		},
	}, nil
}

func (a *apiTranslator) simpleRoundRobinTeam(ctx context.Context, agent *v1alpha1.Agent) (*v1alpha1.Team, error) {
	modelConfigObj, err := common.GetModelConfig(
		ctx,
		a.kube,
		agent,
		a.defaultModelConfig,
	)
	if err != nil {
		return nil, err
	}
	modelConfigRef := common.GetObjectRef(modelConfigObj)

	// generate an internal round robin "team" for the society of mind agent
	meta := agent.ObjectMeta.DeepCopy()
	meta.Name = agent.GetName()
	meta.Namespace = agent.GetNamespace()
	agentRef := common.GetObjectRef(agent)

	team := &v1alpha1.Team{
		ObjectMeta: *meta,
		TypeMeta: metav1.TypeMeta{
			Kind:       "Team",
			APIVersion: "kagent.dev/v1alpha1",
		},
		Spec: v1alpha1.TeamSpec{
			Participants:         []string{agentRef},
			Description:          agent.Spec.Description,
			ModelConfig:          modelConfigRef,
			RoundRobinTeamConfig: &v1alpha1.RoundRobinTeamConfig{},
			TerminationCondition: v1alpha1.TerminationCondition{
				FinalTextMessageTermination: &v1alpha1.FinalTextMessageTermination{
					Source: common.ConvertToPythonIdentifier(agentRef),
				},
			},
		},
	}

	return team, nil
}

func (a *apiTranslator) translateAssistantAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
	modelConfig *v1alpha1.ModelConfig,
	modelClientWithStreaming *api.Component,
	modelClientWithoutStreaming *api.Component,
	modelContext *api.Component,
	opts *teamOptions,
	state *tState,
) (*api.Component, error) {

	tools := []*api.Component{}
	for _, tool := range agent.Spec.Tools {
		switch {
		case tool.Builtin != nil:
			autogenTool, err := a.translateBuiltinTool(
				ctx,
				modelClientWithoutStreaming,
				modelConfig,
				tool.Builtin,
			)
			if err != nil {
				return nil, err
			}
			tools = append(tools, autogenTool)
		case tool.McpServer != nil:
			for _, toolName := range tool.McpServer.ToolNames {
				autogenTool, err := translateToolServerTool(
					ctx,
					a.kube,
					tool.McpServer.ToolServer,
					toolName,
					agent.Namespace,
				)
				if err != nil {
					return nil, err
				}
				tools = append(tools, autogenTool)
			}
		case tool.Agent != nil:
			toolNamespacedName, err := common.ParseRefString(tool.Agent.Ref, agent.Namespace)
			if err != nil {
				return nil, err
			}

			toolRef := toolNamespacedName.String()
			agentRef := common.GetObjectRef(agent)

			if toolRef == agentRef {
				return nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			if state.isVisited(toolRef) {
				return nil, fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, toolRef)
			}

			if state.depth > MAX_DEPTH {
				return nil, fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, toolRef)
			}

			// Translate a nested tool
			toolAgent := &v1alpha1.Agent{}

			err = common.GetObject(
				ctx,
				a.kube,
				toolAgent,
				toolRef,
				agent.Namespace, // redundant
			)
			if err != nil {
				return nil, err
			}

			team, err := a.simpleRoundRobinTeam(ctx, toolAgent)
			if err != nil {
				return nil, err
			}
			autogenTool, err := a.translateGroupChatForTeam(ctx, team, &teamOptions{}, state.with(agent))
			if err != nil {
				return nil, err
			}

			toolAgentRef := common.GetObjectRef(toolAgent)
			tool := &api.Component{
				Provider:      "autogen_agentchat.tools.TeamTool",
				ComponentType: "tool",
				Version:       1,
				Config: api.MustToConfig(&api.TeamToolConfig{
					Name:        toolAgentRef,
					Description: toolAgent.Spec.Description,
					Team:        autogenTool.Component,
				}),
			}

			tools = append(tools, tool)

		default:
			return nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}

	sysMsg := agent.Spec.SystemMessage

	agentRef := common.GetObjectRef(agent)

	cfg := &api.AssistantAgentConfig{
		Name:         common.ConvertToPythonIdentifier(agentRef),
		Tools:        tools,
		ModelContext: modelContext,
		Description:  agent.Spec.Description,
		// TODO(ilackarms): convert to non-ptr with omitempty?
		SystemMessage:         sysMsg,
		ReflectOnToolUse:      false,
		ToolCallSummaryFormat: "\nTool: \n{tool_name}\n\nArguments:\n\n{arguments}\n\nResult: \n{result}\n",
	}

	if opts.stream {
		cfg.ModelClient = modelClientWithStreaming
		cfg.ModelClientStream = true
	} else {
		cfg.ModelClient = modelClientWithoutStreaming
		cfg.ModelClientStream = false
	}

	if agent.Spec.Memory != nil {
		for _, memoryRef := range agent.Spec.Memory {
			autogenMemory, err := a.translateMemory(ctx, memoryRef, agent.Namespace)
			if err != nil {
				return nil, err
			}

			cfg.Memory = append(cfg.Memory, autogenMemory)
		}
	}

	return &api.Component{
		Provider:      "autogen_agentchat.agents.AssistantAgent",
		ComponentType: "agent",
		Version:       1,
		Description:   agent.Spec.Description,
		Config:        api.MustToConfig(cfg),
	}, nil
}

func (a *apiTranslator) translateMemory(ctx context.Context, memoryRef string, defaultNamespace string) (*api.Component, error) {
	memoryObj := &v1alpha1.Memory{}
	if err := common.GetObject(ctx, a.kube, memoryObj, memoryRef, defaultNamespace); err != nil {
		return nil, err
	}

	switch memoryObj.Spec.Provider {
	case v1alpha1.Pinecone:
		apiKey, err := a.getMemoryApiKey(ctx, memoryObj)
		if err != nil {
			return nil, err
		}

		threshold, err := strconv.ParseFloat(memoryObj.Spec.Pinecone.ScoreThreshold, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse score threshold: %v", err)
		}

		return &api.Component{
			Provider:      "kagent.memory.PineconeMemory",
			ComponentType: "memory",
			Version:       1,
			Config: api.MustToConfig(&api.PineconeMemoryConfig{
				APIKey:         string(apiKey),
				IndexHost:      memoryObj.Spec.Pinecone.IndexHost,
				TopK:           memoryObj.Spec.Pinecone.TopK,
				Namespace:      memoryObj.Spec.Pinecone.Namespace,
				RecordFields:   memoryObj.Spec.Pinecone.RecordFields,
				ScoreThreshold: threshold,
			}),
		}, nil
	}

	return nil, fmt.Errorf("unsupported memory provider: %s", memoryObj.Spec.Provider)
}

func (a *apiTranslator) translateBuiltinTool(
	ctx context.Context,
	modelClient *api.Component,
	modelConfig *v1alpha1.ModelConfig,
	tool *v1alpha1.BuiltinTool,
) (*api.Component, error) {

	toolConfig, err := convertMapFromAnytype(tool.Config)
	if err != nil {
		return nil, err
	}
	// special case where we put the model client in the tool config
	if toolNeedsModelClient(tool.Name) {
		if err := addModelClientToConfig(modelClient, &toolConfig); err != nil {
			return nil, fmt.Errorf("failed to add model client to tool config: %v", err)
		}
	}
	if toolNeedsOpenaiApiKey(tool.Name) {
		if (modelConfig.Spec.Provider != v1alpha1.OpenAI) && modelConfig.Spec.Provider != v1alpha1.AzureOpenAI {
			return nil, fmt.Errorf("tool %s requires OpenAI API key, but model config is not OpenAI", tool.Name)
		}
		apiKey, err := a.getModelConfigApiKey(ctx, modelConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get model config api key: %v", err)
		}

		if err := addOpenaiApiKeyToConfig(apiKey, &toolConfig); err != nil {
			return nil, fmt.Errorf("failed to add openai api key to tool config: %v", err)
		}
	}

	providerParts := strings.Split(tool.Name, ".")
	toolLabel := providerParts[len(providerParts)-1]

	return &api.Component{
		Provider:      tool.Name,
		ComponentType: "tool",
		Version:       1,
		Config:        toolConfig,
		Label:         toolLabel,
	}, nil
}

func translateToolServerTool(
	ctx context.Context,
	kube client.Client,
	toolServerRef string,
	toolName string,
	defaultNamespace string,
) (*api.Component, error) {
	toolServerObj := &v1alpha1.ToolServer{}
	err := common.GetObject(
		ctx,
		kube,
		toolServerObj,
		toolServerRef,
		defaultNamespace,
	)
	if err != nil {
		return nil, err
	}

	// requires the tool to have been discovered
	for _, discoveredTool := range toolServerObj.Status.DiscoveredTools {
		if discoveredTool.Name == toolName {
			return convertComponent(discoveredTool.Component)
		}
	}

	return nil, fmt.Errorf("tool %v not found in discovered tools in ToolServer %v", toolName, toolServerObj.Namespace+"/"+toolServerObj.Name)
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
	if terminationCondition.FinalTextMessageTermination != nil {
		conditionsSet++
	}
	if conditionsSet != 1 {
		return nil, fmt.Errorf("exactly one termination condition must be set, got %d", conditionsSet)
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
	case terminationCondition.FinalTextMessageTermination != nil:
		return &api.Component{
			Provider:      "kagent.conditions.FinalTextMessageTermination",
			ComponentType: "termination",
			Version:       1,
			//ComponentVersion: 1,
			Config: api.MustToConfig(&api.FinalTextMessageTerminationConfig{
				Source: terminationCondition.FinalTextMessageTermination.Source,
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

func toolNeedsModelClient(provider string) bool {
	return slices.Contains(toolsProvidersRequiringModelClient, provider)
}

func toolNeedsOpenaiApiKey(provider string) bool {
	return slices.Contains(toolsProvidersRequiringOpenaiApiKey, provider)
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

func addOpenaiApiKeyToConfig(
	apiKey []byte,
	toolConfig *map[string]interface{},
) error {
	if *toolConfig == nil {
		*toolConfig = make(map[string]interface{})
	}

	(*toolConfig)["openai_api_key"] = string(apiKey)
	return nil
}

// createModelClientForProvider creates a model client component based on the model provider
func (a *apiTranslator) createModelClientForProvider(ctx context.Context, modelConfig *v1alpha1.ModelConfig, stream bool) (*api.Component, error) {

	switch modelConfig.Spec.Provider {
	case v1alpha1.Anthropic:
		apiKey, err := a.getModelConfigApiKey(ctx, modelConfig)
		if err != nil {
			return nil, err
		}

		config := &api.AnthropicClientConfiguration{
			BaseAnthropicClientConfiguration: api.BaseAnthropicClientConfiguration{
				APIKey:    string(apiKey),
				Model:     modelConfig.Spec.Model,
				ModelInfo: translateModelInfo(modelConfig.Spec.ModelInfo),
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
		config.DefaultHeaders = modelConfig.Spec.DefaultHeaders
		return &api.Component{
			Provider:      "autogen_ext.models.anthropic.AnthropicChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        configMap,
		}, nil

	case v1alpha1.AzureOpenAI:
		apiKey, err := a.getModelConfigApiKey(ctx, modelConfig)
		if err != nil {
			return nil, err
		}
		config := &api.AzureOpenAIClientConfig{
			BaseOpenAIClientConfig: api.BaseOpenAIClientConfig{
				Model:     modelConfig.Spec.Model,
				APIKey:    string(apiKey),
				ModelInfo: translateModelInfo(modelConfig.Spec.ModelInfo),
			},
		}

		if stream {
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
		config.DefaultHeaders = modelConfig.Spec.DefaultHeaders
		return &api.Component{
			Provider:      "autogen_ext.models.openai.AzureOpenAIChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	case v1alpha1.OpenAI:
		apiKey, err := a.getModelConfigApiKey(ctx, modelConfig)
		if err != nil {
			return nil, err
		}
		config := &api.OpenAIClientConfig{
			BaseOpenAIClientConfig: api.BaseOpenAIClientConfig{
				Model:     modelConfig.Spec.Model,
				APIKey:    string(apiKey),
				ModelInfo: translateModelInfo(modelConfig.Spec.ModelInfo),
			},
		}

		if stream {
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

		config.DefaultHeaders = modelConfig.Spec.DefaultHeaders
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
			ModelInfo:       translateModelInfo(modelConfig.Spec.ModelInfo),
			FollowRedirects: true,
		}

		if modelConfig.Spec.Ollama != nil {
			ollamaConfig := modelConfig.Spec.Ollama

			if ollamaConfig.Options != nil {
				config.Options = ollamaConfig.Options
			}
		}

		config.Headers = modelConfig.Spec.DefaultHeaders
		return &api.Component{
			Provider:      "autogen_ext.models.ollama.OllamaChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	case v1alpha1.AnthropicVertexAI:
		var config *api.AnthropicVertexAIConfig

		creds, err := a.getModelConfigGoogleApplicationCredentials(ctx, modelConfig)
		if err != nil {
			return nil, err
		}

		config = &api.AnthropicVertexAIConfig{
			BaseVertexAIConfig: api.BaseVertexAIConfig{
				Model:       modelConfig.Spec.Model,
				ProjectID:   modelConfig.Spec.AnthropicVertexAI.ProjectID,
				Location:    modelConfig.Spec.AnthropicVertexAI.Location,
				Credentials: creds,
			},
		}

		if modelConfig.Spec.AnthropicVertexAI != nil {
			anthropicVertexAIConfig := modelConfig.Spec.AnthropicVertexAI

			if anthropicVertexAIConfig.MaxTokens > 0 {
				config.MaxTokens = &anthropicVertexAIConfig.MaxTokens
			}

			if anthropicVertexAIConfig.Temperature != "" {
				temp, err := strconv.ParseFloat(anthropicVertexAIConfig.Temperature, 64)
				if err == nil {
					config.Temperature = &temp
				}
			}

			if anthropicVertexAIConfig.TopP != "" {
				topP, err := strconv.ParseFloat(anthropicVertexAIConfig.TopP, 64)
				if err == nil {
					config.TopP = &topP
				}
			}

			if anthropicVertexAIConfig.TopK != "" {
				topK, err := strconv.ParseFloat(anthropicVertexAIConfig.TopK, 64)
				if err == nil {
					config.TopK = &topK
				}
			}

			if anthropicVertexAIConfig.StopSequences != nil {
				config.StopSequences = &anthropicVertexAIConfig.StopSequences
			}
		}

		return &api.Component{
			Provider:      "kagent.models.vertexai.AnthropicVertexAIChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	case v1alpha1.GeminiVertexAI:
		var config *api.GeminiVertexAIConfig

		creds, err := a.getModelConfigGoogleApplicationCredentials(ctx, modelConfig)
		if err != nil {
			return nil, err
		}

		config = &api.GeminiVertexAIConfig{
			BaseVertexAIConfig: api.BaseVertexAIConfig{
				Model:       modelConfig.Spec.Model,
				ProjectID:   modelConfig.Spec.GeminiVertexAI.ProjectID,
				Location:    modelConfig.Spec.GeminiVertexAI.Location,
				Credentials: creds,
			},
		}

		if modelConfig.Spec.GeminiVertexAI != nil {
			geminiVertexAIConfig := modelConfig.Spec.GeminiVertexAI

			if geminiVertexAIConfig.MaxOutputTokens > 0 {
				config.MaxOutputTokens = &geminiVertexAIConfig.MaxOutputTokens
			}

			if geminiVertexAIConfig.Temperature != "" {
				temp, err := strconv.ParseFloat(geminiVertexAIConfig.Temperature, 64)
				if err == nil {
					config.Temperature = &temp
				}
			}

			if geminiVertexAIConfig.TopP != "" {
				topP, err := strconv.ParseFloat(geminiVertexAIConfig.TopP, 64)
				if err == nil {
					config.TopP = &topP
				}
			}

			if geminiVertexAIConfig.TopK != "" {
				topK, err := strconv.ParseFloat(geminiVertexAIConfig.TopK, 64)
				if err == nil {
					config.TopK = &topK
				}
			}

			if geminiVertexAIConfig.StopSequences != nil {
				config.StopSequences = &geminiVertexAIConfig.StopSequences
			}

			if geminiVertexAIConfig.CandidateCount > 0 {
				config.CandidateCount = &geminiVertexAIConfig.CandidateCount
			}

			if geminiVertexAIConfig.ResponseMimeType != "" {
				config.ResponseMimeType = &geminiVertexAIConfig.ResponseMimeType
			}
		}

		return &api.Component{
			Provider:      "kagent.models.vertexai.GeminiVertexAIChatCompletionClient",
			ComponentType: "model",
			Version:       1,
			Config:        api.MustToConfig(config),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported model provider: %s", modelConfig.Spec.Provider)
	}
}

func translateModelInfo(modelInfo *v1alpha1.ModelInfo) *api.ModelInfo {
	if modelInfo == nil {
		return nil
	}

	return &api.ModelInfo{
		Vision:                 modelInfo.Vision,
		FunctionCalling:        modelInfo.FunctionCalling,
		JSONOutput:             modelInfo.JSONOutput,
		Family:                 modelInfo.Family,
		StructuredOutput:       modelInfo.StructuredOutput,
		MultipleSystemMessages: modelInfo.MultipleSystemMessages,
	}
}

func (a *apiTranslator) getSecretKey(ctx context.Context, secretRef string, secretKey string, namespace string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := common.GetObject(
		ctx,
		a.kube,
		secret,
		secretRef,
		namespace,
	); err != nil {
		return nil, fmt.Errorf("failed to fetch secret %s/%s: %w", namespace, secretRef, err)
	}

	if secret.Data == nil {
		return nil, fmt.Errorf("secret data not found in %s/%s", namespace, secretRef)
	}

	value, ok := secret.Data[secretKey]
	if !ok {
		return nil, fmt.Errorf("key %s not found in secret %s/%s", secretKey, namespace, secretRef)
	}

	return value, nil
}

func (a *apiTranslator) getMemoryApiKey(ctx context.Context, memory *v1alpha1.Memory) ([]byte, error) {
	return a.getSecretKey(ctx, memory.Spec.APIKeySecretRef, memory.Spec.APIKeySecretKey, memory.Namespace)
}

func (a *apiTranslator) getModelConfigGoogleApplicationCredentials(ctx context.Context, modelConfig *v1alpha1.ModelConfig) (map[string]interface{}, error) {
	googleApplicationCredentialsSecret := &corev1.Secret{}
	err := common.GetObject(
		ctx,
		a.kube,
		googleApplicationCredentialsSecret,
		modelConfig.Spec.APIKeySecretRef,
		modelConfig.Namespace,
	)
	if err != nil {
		return nil, err
	}

	if googleApplicationCredentialsSecret.Data == nil {
		return nil, fmt.Errorf("google application credentials secret data not found")
	}

	googleApplicationCredentialsBytes, ok := googleApplicationCredentialsSecret.Data[modelConfig.Spec.APIKeySecretKey]
	if !ok {
		return nil, fmt.Errorf("google application credentials not found")
	}

	var credsMap map[string]interface{}
	err = json.Unmarshal(googleApplicationCredentialsBytes, &credsMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal google application credentials into map: %w", err)
	}

	return credsMap, nil
}

func (a *apiTranslator) getModelConfigApiKey(ctx context.Context, modelConfig *v1alpha1.ModelConfig) ([]byte, error) {
	return a.getSecretKey(ctx, modelConfig.Spec.APIKeySecretRef, modelConfig.Spec.APIKeySecretKey, modelConfig.Namespace)
}
