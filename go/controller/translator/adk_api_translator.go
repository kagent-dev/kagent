package translator

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/utils"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

type AgentOutputs struct {
	Manifest []client.Object `json:"manifest,omitempty"`

	Config     *adk.AgentConfig  `json:"config,omitempty"`
	ConfigHash [sha256.Size]byte `json:"configHash"`
}

var adkLog = ctrllog.Log.WithName("adk")

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha1.Agent,
	) (*AgentOutputs, error)
	TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*database.ToolServer, error)
}

func NewAdkApiTranslator(kube client.Client, defaultModelConfig types.NamespacedName) AdkApiTranslator {
	return &adkApiTranslator{
		kube:               kube,
		defaultModelConfig: defaultModelConfig,
	}
}

type adkApiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
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
	s.visitedAgents = append(s.visitedAgents, common.GetObjectRef(agent))
	return s
}

func (t *tState) isVisited(agentName string) bool {
	return slices.Contains(t.visitedAgents, agentName)
}

func (a *adkApiTranslator) TranslateAgent(
	ctx context.Context,
	agent *v1alpha1.Agent,
) (*AgentOutputs, error) {

	adkAgent, envVars, err := a.translateDeclarativeAgent(ctx, agent, &tState{})
	if err != nil {
		return nil, err
	}

	agentJson, err := json.Marshal(adkAgent)
	if err != nil {
		return nil, err
	}

	byt, err := json.Marshal(struct {
		EnvVars    []corev1.EnvVar
		Deployment *v1alpha1.DeploymentSpec
	}{
		EnvVars:    envVars,
		Deployment: agent.Spec.Deployment,
	})
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256(append(byt, agentJson...))
	configHash := binary.BigEndian.Uint64(hash[:8])

	outputs, err := a.translateOutputs(ctx, agent, configHash, agentJson, envVars...)
	if err != nil {
		return nil, err
	}
	outputs.Config = adkAgent
	outputs.ConfigHash = hash

	return outputs, nil
}

func (a *adkApiTranslator) translateOutputs(_ context.Context, agent *v1alpha1.Agent, configHash uint64, configJson []byte, envVars ...corev1.EnvVar) (*AgentOutputs, error) {
	outputs := &AgentOutputs{}

	podLabels := map[string]string{
		"app":    "kagent",
		"kagent": agent.Name,
	}

	objMeta := metav1.ObjectMeta{
		Name:        agent.Name,
		Namespace:   agent.Namespace,
		Annotations: agent.Annotations,
		Labels:      podLabels,
	}
	if agent.Spec.Deployment != nil {
		envVars = append(envVars, agent.Spec.Deployment.Env...)
	}

	outputs.Manifest = append(outputs.Manifest, &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: objMeta,
	})

	outputs.Manifest = append(outputs.Manifest, &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: objMeta,
		Data: map[string]string{
			"config.json": string(configJson),
		},
	})

	spec := defaultDeploymentSpec(objMeta.Name, podLabels, configHash, envVars...)
	outputs.Manifest = append(outputs.Manifest, &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: objMeta,
		Spec:       spec,
	})

	outputs.Manifest = append(outputs.Manifest, &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: objMeta,
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":    "kagent",
				"kagent": agent.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	})

	for _, obj := range outputs.Manifest {
		if err := controllerutil.SetControllerReference(agent, obj, a.kube.Scheme()); err != nil {
			return nil, err
		}
	}

	return outputs, nil
}

func defaultDeploymentSpec(name string, labels map[string]string, configHash uint64, envVars ...corev1.EnvVar) appsv1.DeploymentSpec {
	// TODO: Come up with a better way to do tracing config for the agents
	envVars = append(envVars, slices.Collect(utils.Map(
		utils.Filter(
			slices.Values(os.Environ()),
			func(envVar string) bool {
				return strings.HasPrefix(envVar, "OTEL_")
			},
		),
		func(envVar string) corev1.EnvVar {
			parts := strings.SplitN(envVar, "=", 2)
			return corev1.EnvVar{
				Name:  parts[0],
				Value: parts[1],
			}
		},
	))...)

	envVars = append(envVars, corev1.EnvVar{
		Name: "KAGENT_NAMESPACE",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.namespace",
			},
		},
	})

	podTemplateLabels := maps.Clone(labels)
	podTemplateLabels["config.kagent.dev/hash"] = fmt.Sprintf("%d", configHash)

	return appsv1.DeploymentSpec{
		Replicas: ptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: podTemplateLabels,
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: name,
				Containers: []corev1.Container{
					{
						Name:            "kagent",
						Image:           fmt.Sprintf("cr.kagent.dev/kagent-dev/kagent/app:%s", version.Get().Version),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"kagent", "static", "--host", "0.0.0.0", "--port", "8080", "--filepath", "/config/config.json"},
						Ports: []corev1.ContainerPort{
							{
								Name:          "http",
								ContainerPort: 8080,
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1000m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						Env: envVars,
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/health",
									Port: intstr.FromString("http"),
								},
							},
							InitialDelaySeconds: 15,
							PeriodSeconds:       3,
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "config",
								MountPath: "/config",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: name,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (a *adkApiTranslator) translateDeclarativeAgent(ctx context.Context, agent *v1alpha1.Agent, state *tState) (*adk.AgentConfig, []corev1.EnvVar, error) {

	model, envVars, err := a.translateModel(ctx, agent.Namespace, agent.Spec.ModelConfig)
	if err != nil {
		return nil, nil, err
	}

	cfg := &adk.AgentConfig{
		KagentUrl:   fmt.Sprintf("http://kagent-controller.%s.svc:8083", common.GetResourceNamespace()),
		Name:        common.ConvertToPythonIdentifier(common.GetObjectRef(agent)),
		Description: agent.Spec.Description,
		Instruction: agent.Spec.SystemMessage,
		Model:       model,
		AgentCard: server.AgentCard{
			Name:        agent.Name,
			Description: agent.Spec.Description,
			URL:         fmt.Sprintf("http://%s.%s.svc:8080", agent.Name, agent.Namespace),
			Capabilities: server.AgentCapabilities{
				Streaming:              ptr.To(true),
				PushNotifications:      ptr.To(false),
				StateTransitionHistory: ptr.To(true),
			},
			// Can't be null for Python, so set to empty list
			Skills:             []server.AgentSkill{},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		},
	}

	if agent.Spec.A2AConfig != nil {
		cfg.AgentCard.Skills = slices.Collect(utils.Map(slices.Values(agent.Spec.A2AConfig.Skills), func(skill v1alpha1.AgentSkill) server.AgentSkill {
			return server.AgentSkill(skill)
		}))
	}

	toolsByServer := make(map[string][]string)
	for _, tool := range agent.Spec.Tools {
		// Skip tools that are not applicable to the model provider
		switch {
		case tool.McpServer != nil:
			for _, toolName := range tool.McpServer.ToolNames {
				toolsByServer[tool.McpServer.ToolServer] = append(toolsByServer[tool.McpServer.ToolServer], toolName)
			}
		case tool.Agent != nil:
			toolNamespacedName, err := common.ParseRefString(tool.Agent.Ref, agent.Namespace)
			if err != nil {
				return nil, nil, err
			}

			toolRef := toolNamespacedName.String()
			agentRef := common.GetObjectRef(agent)

			if toolRef == agentRef {
				return nil, nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			if state.isVisited(toolRef) {
				return nil, nil, fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, toolRef)
			}

			if state.depth > MAX_DEPTH {
				return nil, nil, fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, toolRef)
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
				return nil, nil, err
			}

			var toolAgentCfg *adk.AgentConfig
			toolAgentCfg, _, err = a.translateDeclarativeAgent(ctx, toolAgent, state.with(agent))
			if err != nil {
				return nil, nil, err
			}

			cfg.Agents = append(cfg.Agents, *toolAgentCfg)

		default:
			return nil, nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}
	for server, tools := range toolsByServer {
		err := a.translateToolServerTool(ctx, cfg, server, tools, agent.Namespace)
		if err != nil {
			return nil, nil, err
		}
	}

	return cfg, envVars, nil
}

func (a *adkApiTranslator) translateModel(ctx context.Context, namespace, modelConfig string) (adk.Model, []corev1.EnvVar, error) {
	model := &v1alpha1.ModelConfig{}
	err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: modelConfig}, model)
	if err != nil {
		return nil, nil, err
	}

	var envVars []corev1.EnvVar
	switch model.Spec.Provider {
	case v1alpha1.ModelProviderOpenAI:
		if model.Spec.APIKeySecretRef != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: "OPENAI_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecretRef,
						},
						Key: model.Spec.APIKeySecretKey,
					},
				},
			})
		}
		openai := &adk.OpenAI{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		if model.Spec.OpenAI != nil {
			openai.BaseUrl = model.Spec.OpenAI.BaseURL
			if model.Spec.OpenAI.Organization != "" {
				envVars = append(envVars, corev1.EnvVar{
					Name:  "OPENAI_ORGANIZATION",
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, envVars, nil
	case v1alpha1.ModelProviderAnthropic:
		if model.Spec.APIKeySecretRef != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: "ANTHROPIC_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecretRef,
						},
						Key: model.Spec.APIKeySecretKey,
					},
				},
			})
		}
		anthropic := &adk.Anthropic{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		if model.Spec.Anthropic != nil {
			anthropic.BaseUrl = model.Spec.Anthropic.BaseURL
		}
		return anthropic, envVars, nil
	case v1alpha1.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, fmt.Errorf("AzureOpenAI model config is required")
		}
		envVars = append(envVars, corev1.EnvVar{
			Name: "AZURE_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: model.Spec.APIKeySecretRef,
					},
					Key: model.Spec.APIKeySecretKey,
				},
			},
		})
		if model.Spec.AzureOpenAI.AzureADToken != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AZURE_AD_TOKEN",
				Value: model.Spec.AzureOpenAI.AzureADToken,
			})
		}
		if model.Spec.AzureOpenAI.APIVersion != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AZURE_API_VERSION",
				Value: model.Spec.AzureOpenAI.APIVersion,
			})
		}
		if model.Spec.AzureOpenAI.Endpoint != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "AZURE_API_BASE",
				Value: model.Spec.AzureOpenAI.Endpoint,
			})
		}
		azureOpenAI := &adk.AzureOpenAI{
			BaseModel: adk.BaseModel{
				Model: model.Spec.AzureOpenAI.DeploymentName,
			},
		}
		return azureOpenAI, envVars, nil
	case v1alpha1.ModelProviderGeminiVertexAI:
		if model.Spec.GeminiVertexAI == nil {
			return nil, nil, fmt.Errorf("GeminiVertexAI model config is required")
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_PROJECT",
			Value: model.Spec.GeminiVertexAI.ProjectID,
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_LOCATION",
			Value: model.Spec.GeminiVertexAI.Location,
		})
		if model.Spec.APIKeySecretRef != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: "GOOGLE_APPLICATION_CREDENTIALS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecretRef,
						},
						Key: model.Spec.APIKeySecretKey,
					},
				},
			})
		}
		gemini := &adk.GeminiVertexAI{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return gemini, envVars, nil
	case v1alpha1.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil {
			return nil, nil, fmt.Errorf("AnthropicVertexAI model config is required")
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_PROJECT",
			Value: model.Spec.AnthropicVertexAI.ProjectID,
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_LOCATION",
			Value: model.Spec.AnthropicVertexAI.Location,
		})
		if model.Spec.APIKeySecretRef != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name: "GOOGLE_APPLICATION_CREDENTIALS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecretRef,
						},
						Key: model.Spec.APIKeySecretKey,
					},
				},
			})
		}
		anthropic := &adk.GeminiAnthropic{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return anthropic, envVars, nil
	case v1alpha1.ModelProviderOllama:
		if model.Spec.Ollama == nil {
			return nil, nil, fmt.Errorf("Ollama model config is required")
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  "OLLAMA_API_BASE",
			Value: model.Spec.Ollama.Host,
		})
		ollama := &adk.Ollama{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return ollama, envVars, nil
	case v1alpha1.ModelProviderGemini:
		envVars = append(envVars, corev1.EnvVar{
			Name: "GOOGLE_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: model.Spec.APIKeySecretRef,
					},
					Key: model.Spec.APIKeySecretKey,
				},
			},
		})
		gemini := &adk.Gemini{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return gemini, envVars, nil
	}
	return nil, nil, fmt.Errorf("unknown model provider: %s", model.Spec.Provider)
}

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, tool *v1alpha1.StreamableHttpServerConfig, namespace string) (*adk.StreamableHTTPConnectionParams, error) {
	headers := make(map[string]string)
	for _, header := range tool.HeadersFrom {
		if header.Value != "" {
			headers[header.Name] = header.Value
		} else if header.ValueFrom != nil {
			value, err := resolveValueSource(ctx, a.kube, header.ValueFrom, namespace)
			if err != nil {
				return nil, err
			}
			headers[header.Name] = value
		}
	}

	params := &adk.StreamableHTTPConnectionParams{
		Url:     tool.URL,
		Headers: headers,
	}
	if tool.Timeout != nil {
		params.Timeout = ptr.To(tool.Timeout.Seconds())
	}
	if tool.SseReadTimeout != nil {
		params.SseReadTimeout = ptr.To(tool.SseReadTimeout.Seconds())
	}
	if tool.TerminateOnClose != nil {
		params.TerminateOnClose = tool.TerminateOnClose
	}
	return params, nil
}

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, tool *v1alpha1.SseMcpServerConfig, namespace string) (*adk.SseConnectionParams, error) {
	headers := make(map[string]string)
	for _, header := range tool.HeadersFrom {
		if header.Value != "" {
			headers[header.Name] = header.Value
		} else if header.ValueFrom != nil {
			value, err := resolveValueSource(ctx, a.kube, header.ValueFrom, namespace)
			if err != nil {
				return nil, err
			}
			headers[header.Name] = value
		}
	}
	params := &adk.SseConnectionParams{
		Url:     tool.URL,
		Headers: headers,
	}
	if tool.Timeout != nil {
		params.Timeout = ptr.To(tool.Timeout.Seconds())
	}
	if tool.SseReadTimeout != nil {
		params.SseReadTimeout = ptr.To(tool.SseReadTimeout.Seconds())
	}
	return params, nil
}

func (a *adkApiTranslator) translateToolServerTool(ctx context.Context, agent *adk.AgentConfig, toolServerRef string, toolNames []string, defaultNamespace string) error {
	toolServerObj := &v1alpha1.ToolServer{}
	err := common.GetObject(
		ctx,
		a.kube,
		toolServerObj,
		toolServerRef,
		defaultNamespace,
	)
	if err != nil {
		return err
	}

	switch {
	case toolServerObj.Spec.Config.Sse != nil:
		tool, err := a.translateSseHttpTool(ctx, toolServerObj.Spec.Config.Sse, defaultNamespace)
		if err != nil {
			return err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	case toolServerObj.Spec.Config.StreamableHttp != nil:
		tool, err := a.translateStreamableHttpTool(ctx, toolServerObj.Spec.Config.StreamableHttp, defaultNamespace)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	case toolServerObj.Spec.Config.Stdio != nil:
		return fmt.Errorf("stdio tool server is deprecated")
	default:
		return fmt.Errorf("unknown tool server type: %s", toolServerObj.Spec.Config.Type)
	}
	return nil
}

func (a *adkApiTranslator) TranslateToolServer(ctx context.Context, toolServer *v1alpha1.ToolServer) (*database.ToolServer, error) {
	return &database.ToolServer{
		Name:        common.GetObjectRef(toolServer),
		Description: toolServer.Spec.Description,
		Config:      toolServer.Spec.Config,
	}, nil
}

// resolveValueSource resolves a value from a ValueSource
func resolveValueSource(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	switch source.Type {
	case v1alpha1.ConfigMapValueSource:
		return getConfigMapValue(ctx, kube, source, namespace)
	case v1alpha1.SecretValueSource:
		return getSecretValue(ctx, kube, source, namespace)
	default:
		return "", fmt.Errorf("unknown value source type: %s", source.Type)
	}
}

// getConfigMapValue fetches a value from a ConfigMap
func getConfigMapValue(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	configMap := &corev1.ConfigMap{}
	err := common.GetObject(
		ctx,
		kube,
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
func getSecretValue(ctx context.Context, kube client.Client, source *v1alpha1.ValueSource, namespace string) (string, error) {
	if source == nil {
		return "", fmt.Errorf("source cannot be nil")
	}

	secret := &corev1.Secret{}
	err := common.GetObject(
		ctx,
		kube,
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
