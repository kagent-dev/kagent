package agent

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

// AgentManifestInputs holds the translated data needed to emit Kubernetes resources.
type AgentManifestInputs struct {
	Config          *adk.AgentConfig
	Sandbox         *v1alpha2.SandboxConfig
	Deployment      *resolvedDeployment
	AgentCard       *server.AgentCard
	SecretHashBytes []byte
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

func (s *tState) with(agent v1alpha2.AgentObject) *tState {
	visited := make([]string, len(s.visitedAgents), len(s.visitedAgents)+1)
	copy(visited, s.visitedAgents)
	visited = append(visited, utils.GetObjectRef(agent))
	return &tState{
		depth:         s.depth + 1,
		visitedAgents: visited,
	}
}

func (t *tState) isVisited(agentName string) bool {
	return slices.Contains(t.visitedAgents, agentName)
}

func TranslateAgent(
	ctx context.Context,
	translator AdkApiTranslator,
	agent v1alpha2.AgentObject,
) (*AgentOutputs, error) {
	inputs, err := translator.CompileAgent(ctx, agent)
	if err != nil {
		return nil, err
	}
	return translator.BuildManifest(ctx, agent, inputs)
}

func (a *adkApiTranslator) CompileAgent(
	ctx context.Context,
	agent v1alpha2.AgentObject,
) (*AgentManifestInputs, error) {
	spec := agent.GetAgentSpec()
	err := a.validateAgent(ctx, agent, &tState{})
	if err != nil {
		return nil, err
	}

	var cfg *adk.AgentConfig
	var dep *resolvedDeployment
	var secretHashBytes []byte

	switch spec.Type {
	case v1alpha2.AgentType_Declarative:
		var mdd *modelDeploymentData
		cfg, mdd, secretHashBytes, err = a.translateInlineAgent(ctx, agent)
		if err != nil {
			return nil, err
		}
		dep, err = resolveInlineDeployment(agent, mdd)
		if err != nil {
			return nil, err
		}

	case v1alpha2.AgentType_BYO:
		dep, err = resolveByoDeployment(agent)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unknown agent type: %s", spec.Type)
	}

	runInSandbox := agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox
	if runInSandbox && a.sandboxBackend == nil {
		return nil, fmt.Errorf("sandbox backend is not configured")
	}

	card := GetA2AAgentCard(agent)

	return &AgentManifestInputs{
		Config:          cfg,
		Sandbox:         spec.Sandbox,
		Deployment:      dep,
		AgentCard:       card,
		SecretHashBytes: secretHashBytes,
	}, nil
}

func (a *adkApiTranslator) validateAgent(ctx context.Context, agent v1alpha2.AgentObject, state *tState) error {
	agentRef := utils.GetObjectRef(agent)
	spec := agent.GetAgentSpec()

	if state.isVisited(agentRef) {
		return fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, agentRef)
	}

	if state.depth > MAX_DEPTH {
		return fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, agentRef)
	}

	if spec.Type != v1alpha2.AgentType_Declarative || spec.Declarative == nil {
		// We only need to validate loops in declarative agents
		return nil
	}

	for _, tool := range spec.Declarative.Tools {
		switch tool.Type {
		case v1alpha2.ToolProviderType_Agent:
			if tool.Agent == nil {
				return fmt.Errorf("tool must have an agent reference")
			}

			agentRef := tool.Agent.NamespacedName(agent.GetNamespace())

			if agentRef.Namespace == agent.GetNamespace() && agentRef.Name == agent.GetName() {
				return fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			toolAgent := &v1alpha2.Agent{}
			err := a.kube.Get(ctx, agentRef, toolAgent)
			if err != nil {
				return err
			}

			err = a.validateAgent(ctx, toolAgent, state.with(agent))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *adkApiTranslator) translateInlineAgent(ctx context.Context, agent v1alpha2.AgentObject) (*adk.AgentConfig, *modelDeploymentData, []byte, error) {
	spec := agent.GetAgentSpec()
	model, mdd, secretHashBytes, err := a.translateModel(ctx, agent.GetNamespace(), spec.Declarative.ModelConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	// Resolve the raw system message (template processing happens after tools are translated).
	rawSystemMessage, err := a.resolveRawSystemMessage(ctx, agent)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := &adk.AgentConfig{
		Description: spec.Description,
		Instruction: rawSystemMessage,
		Model:       model,
		ExecuteCode: spec.Declarative.ExecuteCodeBlocks,
		Stream:      new(spec.Declarative.Stream),
	}

	if spec.Sandbox != nil && spec.Sandbox.Network != nil {
		cfg.Network = &adk.NetworkConfig{
			AllowedDomains: append([]string(nil), spec.Sandbox.Network.AllowedDomains...),
		}
	}

	// Translate context management configuration
	if spec.Declarative.Context != nil {
		contextCfg := &adk.AgentContextConfig{}

		if spec.Declarative.Context.Compaction != nil {
			comp := spec.Declarative.Context.Compaction
			compCfg := &adk.AgentCompressionConfig{
				CompactionInterval: comp.CompactionInterval,
				OverlapSize:        comp.OverlapSize,
				TokenThreshold:     comp.TokenThreshold,
				EventRetentionSize: comp.EventRetentionSize,
			}

			if comp.Summarizer != nil {
				if comp.Summarizer.PromptTemplate != nil {
					compCfg.PromptTemplate = *comp.Summarizer.PromptTemplate
				}

				summarizerModelName := ""
				if comp.Summarizer.ModelConfig != nil {
					summarizerModelName = *comp.Summarizer.ModelConfig
				}

				if summarizerModelName == "" || summarizerModelName == spec.Declarative.ModelConfig {
					compCfg.SummarizerModel = model
				} else {
					summarizerModel, summarizerMdd, summarizerSecretHash, err := a.translateModel(ctx, agent.GetNamespace(), summarizerModelName)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("failed to translate summarizer model config %q: %w", summarizerModelName, err)
					}
					compCfg.SummarizerModel = summarizerModel
					mergeDeploymentData(mdd, summarizerMdd)
					if len(summarizerSecretHash) > 0 {
						secretHashBytes = append(secretHashBytes, summarizerSecretHash...)
					}
				}
			}

			contextCfg.Compaction = compCfg
		}

		cfg.ContextConfig = contextCfg
	}

	// Handle Memory Configuration: presence of Memory field enables it.
	if spec.Declarative.Memory != nil {
		embCfg, embMdd, embHash, err := a.translateEmbeddingConfig(ctx, agent.GetNamespace(), spec.Declarative.Memory.ModelConfig)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to resolve embedding config: %w", err)
		}

		cfg.Memory = &adk.MemoryConfig{
			TTLDays:   spec.Declarative.Memory.TTLDays,
			Embedding: embCfg,
		}

		mergeDeploymentData(mdd, embMdd)
		if spec.Declarative.Memory.ModelConfig != spec.Declarative.ModelConfig {
			secretHashBytes = append(secretHashBytes, embHash...)
		}
	}

	// Translate retry policy configuration
	if spec.Declarative.RetryPolicy != nil {
		retryPolicy, err := translateRetryPolicy(spec.Declarative.RetryPolicy)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to translate retry policy: %w", err)
		}
		cfg.RetryPolicy = retryPolicy
	}

	for _, tool := range spec.Declarative.Tools {
		headers, err := tool.ResolveHeaders(ctx, a.kube, agent.GetNamespace())
		if err != nil {
			return nil, nil, nil, err
		}

		switch {
		case tool.McpServer != nil:
			err := a.translateMCPServerTarget(ctx, cfg, agent.GetNamespace(), tool.McpServer, headers, a.globalProxyURL)
			if err != nil {
				return nil, nil, nil, err
			}
		case tool.Agent != nil:
			agentRef := tool.Agent.NamespacedName(agent.GetNamespace())

			if agentRef.Namespace == agent.GetNamespace() && agentRef.Name == agent.GetName() {
				return nil, nil, nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			toolAgent := &v1alpha2.Agent{}
			err := a.kube.Get(ctx, agentRef, toolAgent)
			if err != nil {
				return nil, nil, nil, err
			}

			switch toolAgent.Spec.Type {
			case v1alpha2.AgentType_BYO, v1alpha2.AgentType_Declarative:
				originalURL := fmt.Sprintf("http://%s.%s:8080", toolAgent.Name, toolAgent.Namespace)

				targetURL := originalURL
				if a.globalProxyURL != "" {
					targetURL, headers, err = applyProxyURL(originalURL, a.globalProxyURL, headers)
					if err != nil {
						return nil, nil, nil, err
					}
				}

				cfg.RemoteAgents = append(cfg.RemoteAgents, adk.RemoteAgentConfig{
					Name:        utils.ConvertToPythonIdentifier(utils.GetObjectRef(toolAgent)),
					Url:         targetURL,
					Headers:     headers,
					Description: toolAgent.Spec.Description,
				})
			default:
				return nil, nil, nil, fmt.Errorf("unknown agent type: %s", toolAgent.Spec.Type)
			}

		default:
			return nil, nil, nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}

	if spec.Declarative.PromptTemplate != nil && len(spec.Declarative.PromptTemplate.DataSources) > 0 {
		lookup, err := resolvePromptSources(ctx, a.kube, agent.GetNamespace(), spec.Declarative.PromptTemplate.DataSources)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to resolve prompt sources: %w", err)
		}

		tplCtx := buildTemplateContext(agent, cfg)

		resolved, err := executeSystemMessageTemplate(cfg.Instruction, lookup, tplCtx)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to execute system message template: %w", err)
		}
		cfg.Instruction = resolved
	}

	return cfg, mdd, secretHashBytes, nil
}

// resolveRawSystemMessage gets the raw system message string from the agent spec
// without applying any template processing.
func (a *adkApiTranslator) resolveRawSystemMessage(ctx context.Context, agent v1alpha2.AgentObject) (string, error) {
	spec := agent.GetAgentSpec()
	if spec.Declarative.SystemMessageFrom != nil {
		return spec.Declarative.SystemMessageFrom.Resolve(ctx, a.kube, agent.GetNamespace())
	}
	if spec.Declarative.SystemMessage != "" {
		return spec.Declarative.SystemMessage, nil
	}
	return "", fmt.Errorf("at least one system message source (SystemMessage or SystemMessageFrom) must be specified")
}

func translateRetryPolicy(spec *v1alpha2.RetryPolicySpec) (*adk.RetryPolicyConfig, error) {
	if spec == nil {
		return nil, nil
	}

	cfg := &adk.RetryPolicyConfig{}

	if spec.MaxRetries != nil {
		cfg.MaxRetries = *spec.MaxRetries
	}

	if spec.InitialRetryDelay != nil {
		d, err := time.ParseDuration(*spec.InitialRetryDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid initialRetryDelay %q: %w", *spec.InitialRetryDelay, err)
		}
		cfg.InitialRetryDelay = d.Seconds()
	}

	if spec.MaxRetryDelay != nil {
		d, err := time.ParseDuration(*spec.MaxRetryDelay)
		if err != nil {
			return nil, fmt.Errorf("invalid maxRetryDelay %q: %w", *spec.MaxRetryDelay, err)
		}
		secs := d.Seconds()
		cfg.MaxRetryDelay = &secs
	}

	return cfg, nil
}
