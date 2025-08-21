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
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"trpc.group/trpc-go/trpc-a2a-go/server"
)

const (
	MCPServiceLabel              = "kagent.dev/mcp-service"
	MCPServicePathAnnotation     = "kagent.dev/mcp-service-path"
	MCPServicePortAnnotation     = "kagent.dev/mcp-service-port"
	MCPServiceProtocolAnnotation = "kagent.dev/mcp-service-protocol"

	MCPServicePathDefault     = "/mcp"
	MCPServiceProtocolDefault = v1alpha2.RemoteMCPServerProtocolStreamableHttp
)

type ImageConfig struct {
	Registry   string `json:"registry,omitempty"`
	Tag        string `json:"tag,omitempty"`
	PullPolicy string `json:"pullPolicy,omitempty"`
	Repository string `json:"repository,omitempty"`
}

var DefaultImageConfig = ImageConfig{
	Registry:   "cr.kagent.dev",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	Repository: "kagent-dev/kagent/app",
}

type AgentOutputs struct {
	Manifest []client.Object `json:"manifest,omitempty"`

	Config    *adk.AgentConfig `json:"config,omitempty"`
	AgentCard server.AgentCard `json:"agentCard"`
}

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha2.Agent,
	) (*AgentOutputs, error)
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

func (s *tState) with(agent *v1alpha2.Agent) *tState {
	s.depth++
	s.visitedAgents = append(s.visitedAgents, utils.GetObjectRef(agent))
	return s
}

func (t *tState) isVisited(agentName string) bool {
	return slices.Contains(t.visitedAgents, agentName)
}

func (a *adkApiTranslator) TranslateAgent(
	ctx context.Context,
	agent *v1alpha2.Agent,
) (*AgentOutputs, error) {

	switch agent.Spec.Type {
	case v1alpha2.AgentType_Declarative:
		cfg, card, mdd, err := a.translateInlineAgent(ctx, agent, &tState{})
		if err != nil {
			return nil, err
		}
		dep, err := a.resolveInlineDeployment(agent, mdd)
		if err != nil {
			return nil, err
		}
		return a.buildManifest(ctx, agent, dep, cfg, card)

	case v1alpha2.AgentType_BYO:

		dep, err := a.resolveByoDeployment(agent)
		if err != nil {
			return nil, err
		}
		// TODO: Resolve this from the actual pod
		agentCard := &server.AgentCard{
			Name:        strings.ReplaceAll(agent.Name, "-", "_"),
			Description: agent.Spec.Description,
			URL:         fmt.Sprintf("http://%s.%s:8080", agent.Name, agent.Namespace),
			Capabilities: server.AgentCapabilities{
				Streaming:              ptr.To(true),
				PushNotifications:      ptr.To(false),
				StateTransitionHistory: ptr.To(true),
			},
			// Can't be null for Python, so set to empty list
			Skills:             []server.AgentSkill{},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text"},
		}
		return a.buildManifest(ctx, agent, dep, nil, agentCard)

	default:
		return nil, fmt.Errorf("unknown agent type: %s", agent.Spec.Type)
	}
}

func (a *adkApiTranslator) buildManifest(
	ctx context.Context,
	agent *v1alpha2.Agent,
	dep *resolvedDeployment,
	cfg *adk.AgentConfig, // nil for BYO
	card *server.AgentCard, // nil for BYO
) (*AgentOutputs, error) {
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

	// Service Account
	outputs.Manifest = append(outputs.Manifest, &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: objMeta,
	})

	// Base env for both types
	sharedEnv := make([]corev1.EnvVar, 0, 8)
	sharedEnv = append(sharedEnv, collectOtelEnvFromProcess()...)
	sharedEnv = append(sharedEnv,
		corev1.EnvVar{
			Name: "KAGENT_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		corev1.EnvVar{
			Name: "KAGENT_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.serviceAccountName"},
			},
		},
		corev1.EnvVar{
			Name:  "KAGENT_URL",
			Value: fmt.Sprintf("http://kagent-controller.%s:8083", utils.GetResourceNamespace()),
		},
	)

	// Optional config/card for Inline
	var configHash uint64
	var configVol []corev1.Volume
	var configMounts []corev1.VolumeMount
	if cfg != nil && card != nil {
		bCfg, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		bCard, err := json.Marshal(card)
		if err != nil {
			return nil, err
		}
		configHash = computeConfigHash(bCfg, bCard)

		outputs.Manifest = append(outputs.Manifest, &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: objMeta,
			Data: map[string]string{
				"config.json":     string(bCfg),
				"agent-card.json": string(bCard),
			},
		})

		configVol = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: agent.Name},
				},
			},
		}}
		configMounts = []corev1.VolumeMount{{Name: "config", MountPath: "/config"}}
	}

	// Build Deployment
	volumes := append(configVol, dep.Volumes...)
	volumeMounts := append(configMounts, dep.VolumeMounts...)

	// Token volume
	volumes = append(volumes, corev1.Volume{
		Name: "kagent-token",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          "kagent",
							ExpirationSeconds: ptr.To(int64(3600)),
							Path:              "kagent-token",
						},
					},
				},
			},
		},
	})
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "kagent-token",
		MountPath: "/var/run/secrets/tokens",
	})
	env := append(dep.Env, sharedEnv...)

	podTemplateLabels := maps.Clone(podLabels)
	if dep.Labels != nil {
		maps.Copy(podTemplateLabels, dep.Labels)
	}
	if configHash != 0 {
		if podTemplateLabels == nil {
			podTemplateLabels = map[string]string{}
		}
		podTemplateLabels["kagent.dev/config-hash"] = fmt.Sprintf("%d", configHash)
	}

	var cmd []string
	if len(dep.Cmd) != 0 {
		cmd = []string{dep.Cmd}
	}

	deployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: objMeta,
		Spec: appsv1.DeploymentSpec{
			Replicas: dep.Replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			Selector: &metav1.LabelSelector{MatchLabels: podLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podTemplateLabels, Annotations: dep.Annotations},
				Spec: corev1.PodSpec{
					ServiceAccountName: agent.Name,
					ImagePullSecrets:   dep.ImagePullSecrets,
					Containers: []corev1.Container{{
						Name:            "kagent",
						Image:           dep.Image,
						ImagePullPolicy: dep.ImagePullPolicy,
						Command:         cmd,
						Args:            dep.Args,
						Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: dep.Port}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("384Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2000m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						Env: env,
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
							},
							InitialDelaySeconds: 15,
							TimeoutSeconds:      15,
							PeriodSeconds:       15,
						},
						VolumeMounts: volumeMounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
	outputs.Manifest = append(outputs.Manifest, deployment)

	// Service
	outputs.Manifest = append(outputs.Manifest, &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: objMeta,
		Spec: corev1.ServiceSpec{
			Selector: podLabels,
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       dep.Port,
				TargetPort: intstr.FromInt(int(dep.Port)),
			}},
			Type: corev1.ServiceTypeClusterIP,
		},
	})

	// Owner refs
	for _, obj := range outputs.Manifest {
		if err := controllerutil.SetControllerReference(agent, obj, a.kube.Scheme()); err != nil {
			return nil, err
		}
	}

	// Inline-only return values
	outputs.Config = cfg
	if card != nil {
		outputs.AgentCard = *card
	}
	return outputs, nil
}

func (a *adkApiTranslator) translateInlineAgent(ctx context.Context, agent *v1alpha2.Agent, state *tState) (*adk.AgentConfig, *server.AgentCard, *modelDeploymentData, error) {

	model, mdd, err := a.translateModel(ctx, agent.Namespace, agent.Spec.Declarative.ModelConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := &adk.AgentConfig{
		Description: agent.Spec.Description,
		Instruction: agent.Spec.Declarative.SystemMessage,
		Model:       model,
	}
	agentCard := &server.AgentCard{
		Name:        strings.ReplaceAll(agent.Name, "-", "_"),
		Description: agent.Spec.Description,
		URL:         fmt.Sprintf("http://%s.%s:8080", agent.Name, agent.Namespace),
		Capabilities: server.AgentCapabilities{
			Streaming:              ptr.To(true),
			PushNotifications:      ptr.To(false),
			StateTransitionHistory: ptr.To(true),
		},
		// Can't be null for Python, so set to empty list
		Skills:             []server.AgentSkill{},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}

	if agent.Spec.Declarative.A2AConfig != nil {
		agentCard.Skills = slices.Collect(utils.Map(slices.Values(agent.Spec.Declarative.A2AConfig.Skills), func(skill v1alpha2.AgentSkill) server.AgentSkill {
			return server.AgentSkill(skill)
		}))
	}

	toolsByServer := make(map[v1alpha2.TypedLocalReference][]string)
	for _, tool := range agent.Spec.Declarative.Tools {
		// Skip tools that are not applicable to the model provider
		switch {
		case tool.McpServer != nil:
			toolsByServer[tool.McpServer.TypedLocalReference] = append(toolsByServer[tool.McpServer.TypedLocalReference], tool.McpServer.ToolNames...)
		case tool.Agent != nil:

			agentRef := types.NamespacedName{
				Namespace: agent.Namespace,
				Name:      tool.Agent.Name,
			}

			if agentRef.Namespace == agent.Namespace && agentRef.Name == agent.Name {
				return nil, nil, nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			if state.isVisited(agentRef.String()) {
				return nil, nil, nil, fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, agentRef.String())
			}

			if state.depth > MAX_DEPTH {
				return nil, nil, nil, fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, agentRef.String())
			}

			// Translate a nested tool
			toolAgent := &v1alpha2.Agent{}
			err := a.kube.Get(ctx, agentRef, toolAgent)
			if err != nil {
				return nil, nil, nil, err
			}

			var toolAgentCfg *adk.AgentConfig
			switch toolAgent.Spec.Type {
			case v1alpha2.AgentType_Declarative:
				toolAgentCfg, _, _, err = a.translateInlineAgent(ctx, toolAgent, state.with(agent))
				if err != nil {
					return nil, nil, nil, err
				}
				cfg.Agents = append(cfg.Agents, *toolAgentCfg)
			case v1alpha2.AgentType_BYO:
				port := int32(8080)
				url := fmt.Sprintf("http://%s.%s:%d", toolAgent.Name, toolAgent.Namespace, port)
				cfg.RemoteAgents = append(cfg.RemoteAgents, adk.RemoteAgentConfig{
					Name:        utils.ConvertToPythonIdentifier(utils.GetObjectRef(toolAgent)),
					Url:         url,
					Description: toolAgent.Spec.Description,
				})
			default:
				return nil, nil, nil, fmt.Errorf("unknown agent type: %s", toolAgent.Spec.Type)
			}

		default:
			return nil, nil, nil, fmt.Errorf("tool must have a provider or tool server")
		}
	}
	for server, tools := range toolsByServer {
		err := a.translateMCPServerTarget(ctx, cfg, server, tools, agent.Namespace)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return cfg, agentCard, mdd, nil
}

const (
	googleCredsVolumeName = "google-creds"
)

func (a *adkApiTranslator) translateModel(ctx context.Context, namespace, modelConfig string) (adk.Model, *modelDeploymentData, error) {
	model := &v1alpha2.ModelConfig{}
	err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: modelConfig}, model)
	if err != nil {
		return nil, nil, err
	}

	modelDeploymentData := &modelDeploymentData{}
	switch model.Spec.Provider {
	case v1alpha2.ModelProviderOpenAI:
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: "OPENAI_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecret,
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
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name:  "OPENAI_ORGANIZATION",
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, modelDeploymentData, nil
	case v1alpha2.ModelProviderAnthropic:
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: "ANTHROPIC_API_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecret,
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
		return anthropic, modelDeploymentData, nil
	case v1alpha2.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, fmt.Errorf("AzureOpenAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name: "AZURE_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: model.Spec.APIKeySecret,
					},
					Key: model.Spec.APIKeySecretKey,
				},
			},
		})
		if model.Spec.AzureOpenAI.AzureADToken != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "AZURE_AD_TOKEN",
				Value: model.Spec.AzureOpenAI.AzureADToken,
			})
		}
		if model.Spec.AzureOpenAI.APIVersion != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "AZURE_API_VERSION",
				Value: model.Spec.AzureOpenAI.APIVersion,
			})
		}
		if model.Spec.AzureOpenAI.Endpoint != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "AZURE_API_BASE",
				Value: model.Spec.AzureOpenAI.Endpoint,
			})
		}
		azureOpenAI := &adk.AzureOpenAI{
			BaseModel: adk.BaseModel{
				Model: model.Spec.AzureOpenAI.DeploymentName,
			},
		}
		return azureOpenAI, modelDeploymentData, nil
	case v1alpha2.ModelProviderGeminiVertexAI:
		if model.Spec.GeminiVertexAI == nil {
			return nil, nil, fmt.Errorf("GeminiVertexAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_PROJECT",
			Value: model.Spec.GeminiVertexAI.ProjectID,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_LOCATION",
			Value: model.Spec.GeminiVertexAI.Location,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "GOOGLE_GENAI_USE_VERTEXAI",
			Value: "true",
		})
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "GOOGLE_APPLICATION_CREDENTIALS",
				Value: "/creds/" + model.Spec.APIKeySecretKey,
			})
			modelDeploymentData.Volumes = append(modelDeploymentData.Volumes, corev1.Volume{
				Name: googleCredsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: model.Spec.APIKeySecret,
					},
				},
			})
			modelDeploymentData.VolumeMounts = append(modelDeploymentData.VolumeMounts, corev1.VolumeMount{
				Name:      googleCredsVolumeName,
				MountPath: "/creds",
			})
		}
		gemini := &adk.GeminiVertexAI{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return gemini, modelDeploymentData, nil
	case v1alpha2.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil {
			return nil, nil, fmt.Errorf("AnthropicVertexAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_PROJECT",
			Value: model.Spec.AnthropicVertexAI.ProjectID,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "GOOGLE_CLOUD_LOCATION",
			Value: model.Spec.AnthropicVertexAI.Location,
		})
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "GOOGLE_APPLICATION_CREDENTIALS",
				Value: "/creds/" + model.Spec.APIKeySecretKey,
			})
			modelDeploymentData.Volumes = append(modelDeploymentData.Volumes, corev1.Volume{
				Name: googleCredsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: model.Spec.APIKeySecret,
					},
				},
			})
			modelDeploymentData.VolumeMounts = append(modelDeploymentData.VolumeMounts, corev1.VolumeMount{
				Name:      googleCredsVolumeName,
				MountPath: "/creds",
			})
		}
		anthropic := &adk.GeminiAnthropic{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return anthropic, modelDeploymentData, nil
	case v1alpha2.ModelProviderOllama:
		if model.Spec.Ollama == nil {
			return nil, nil, fmt.Errorf("ollama model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "OLLAMA_API_BASE",
			Value: model.Spec.Ollama.Host,
		})
		ollama := &adk.Ollama{
			BaseModel: adk.BaseModel{
				Model: model.Spec.Model,
			},
		}
		return ollama, modelDeploymentData, nil
	case v1alpha2.ModelProviderGemini:
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name: "GOOGLE_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: model.Spec.APIKeySecret,
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
		return gemini, modelDeploymentData, nil
	}
	return nil, nil, fmt.Errorf("unknown model provider: %s", model.Spec.Provider)
}

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, tool *v1alpha2.RemoteMCPServerSpec, namespace string) (*adk.StreamableHTTPConnectionParams, error) {
	headers, err := tool.ResolveHeaders(ctx, a.kube, namespace)
	if err != nil {
		return nil, err
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

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, tool *v1alpha2.RemoteMCPServerSpec, namespace string) (*adk.SseConnectionParams, error) {
	headers, err := tool.ResolveHeaders(ctx, a.kube, namespace)
	if err != nil {
		return nil, err
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

func (a *adkApiTranslator) translateMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, toolServerRef v1alpha2.TypedLocalReference, toolNames []string, agentNamespace string) error {
	gvk := toolServerRef.GroupKind()

	switch gvk {
	case schema.GroupKind{
		Group: "",
		Kind:  "",
	}:
		fallthrough // default to MCP server
	case schema.GroupKind{
		Group: "",
		Kind:  "MCPServer",
	}:
		fallthrough // default to MCP server
	case schema.GroupKind{
		Group: "kagent.dev",
		Kind:  "MCPServer",
	}:
		mcpServer := &v1alpha1.MCPServer{}
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServerRef.Name}, mcpServer)
		if err != nil {
			return err
		}
		spec, err := ConvertMCPServerToRemoteMCPServer(mcpServer)
		if err != nil {
			return err
		}
		return a.translateRemoteMCPServerTarget(ctx, agent, spec, toolNames, agentNamespace)
	case schema.GroupKind{
		Group: "",
		Kind:  "RemoteMCPServer",
	}:
		fallthrough // default to remote MCP server
	case schema.GroupKind{
		Group: "kagent.dev",
		Kind:  "RemoteMCPServer",
	}:
		remoteMcpServer := &v1alpha2.RemoteMCPServer{}
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServerRef.Name}, remoteMcpServer)
		if err != nil {
			return err
		}
		return a.translateRemoteMCPServerTarget(ctx, agent, &remoteMcpServer.Spec, toolNames, agentNamespace)
	case schema.GroupKind{
		Group: "",
		Kind:  "Service",
	}:
		fallthrough // default to service
	case schema.GroupKind{
		Group: "core",
		Kind:  "Service",
	}:
		svc := &corev1.Service{}
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServerRef.Name}, svc)
		if err != nil {
			return err
		}
		spec, err := ConvertServiceToRemoteMCPServer(svc)
		if err != nil {
			return err
		}
		return a.translateRemoteMCPServerTarget(ctx, agent, spec, toolNames, agentNamespace)

	default:
		return fmt.Errorf("unknown tool server type: %s", gvk)
	}
}

func ConvertServiceToRemoteMCPServer(svc *corev1.Service) (*v1alpha2.RemoteMCPServerSpec, error) {
	// Check wellknown annotations
	port := int64(0)
	protocol := string(MCPServiceProtocolDefault)
	path := MCPServicePathDefault
	if svc.Annotations != nil {
		if portStr, ok := svc.Annotations[MCPServicePortAnnotation]; ok {
			var err error
			port, err = strconv.ParseInt(portStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("port in annotation %s is not a valid integer: %v", MCPServicePortAnnotation, err)
			}
		}
		if protocolStr, ok := svc.Annotations[MCPServiceProtocolAnnotation]; ok {
			if protocolStr != string(v1alpha2.RemoteMCPServerProtocolSse) && protocolStr != string(v1alpha2.RemoteMCPServerProtocolStreamableHttp) {
				// default to streamable http
				protocol = string(v1alpha2.RemoteMCPServerProtocolStreamableHttp)
			} else {
				protocol = protocolStr
			}
		}
		if pathStr, ok := svc.Annotations[MCPServicePathAnnotation]; ok {
			path = pathStr
		}
	}
	if port == 0 {
		if len(svc.Spec.Ports) == 1 {
			port = int64(svc.Spec.Ports[0].Port)
		} else {
			// Look through ports to find AppProtcol = mcp
			for _, svcPort := range svc.Spec.Ports {
				if svcPort.AppProtocol != nil && strings.ToLower(*svcPort.AppProtocol) == "mcp" {
					port = int64(svcPort.Port)
					break
				}
			}
		}
	}
	if port == 0 {
		return nil, fmt.Errorf("no port found for service %s with protocol %s", svc.Name, protocol)
	}
	return &v1alpha2.RemoteMCPServerSpec{
		URL:      fmt.Sprintf("http://%s.%s:%d%s", svc.Name, svc.Namespace, port, path),
		Protocol: v1alpha2.RemoteMCPServerProtocol(protocol),
	}, nil
}

func ConvertMCPServerToRemoteMCPServer(mcpServer *v1alpha1.MCPServer) (*v1alpha2.RemoteMCPServerSpec, error) {
	if mcpServer.Spec.Deployment.Port == 0 {
		return nil, fmt.Errorf("cannot determine port for MCP server %s", mcpServer.Name)
	}

	return &v1alpha2.RemoteMCPServerSpec{
		URL:      fmt.Sprintf("http://%s.%s:%d/mcp", mcpServer.Name, mcpServer.Namespace, mcpServer.Spec.Deployment.Port),
		Protocol: v1alpha2.RemoteMCPServerProtocolStreamableHttp,
	}, nil
}
func (a *adkApiTranslator) translateRemoteMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, remoteMcpServer *v1alpha2.RemoteMCPServerSpec, toolNames []string, agentNamespace string) error {
	switch remoteMcpServer.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		tool, err := a.translateSseHttpTool(ctx, remoteMcpServer, agentNamespace)
		if err != nil {
			return err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	default:
		tool, err := a.translateStreamableHttpTool(ctx, remoteMcpServer, agentNamespace)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	}
	return nil
}

// Helper functions

func computeConfigHash(config, card []byte) uint64 {
	hasher := sha256.New()
	hasher.Write(config)
	hasher.Write(card)
	hash := hasher.Sum(nil)
	return binary.BigEndian.Uint64(hash[:8])
}

func collectOtelEnvFromProcess() []corev1.EnvVar {
	envVars := slices.Collect(utils.Map(
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
	))

	// Sort by environment variable name
	slices.SortFunc(envVars, func(a, b corev1.EnvVar) int {
		return strings.Compare(a.Name, b.Name)
	})

	return envVars
}

// Internal to translator - Data added to the deployment spec for an inline agent
// Mostly used for model auth and config.
type modelDeploymentData struct {
	EnvVars      []corev1.EnvVar
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

// Internal to translator – a unified deployment spec for any agent.
type resolvedDeployment struct {
	// Required concrete runtime properties
	Image           string
	Cmd             string // empty → no explicit command
	Args            []string
	Port            int32 // container port and Service port
	ImagePullPolicy corev1.PullPolicy

	// SharedDeploymentSpec merged
	Replicas         *int32
	ImagePullSecrets []corev1.LocalObjectReference
	Volumes          []corev1.Volume
	VolumeMounts     []corev1.VolumeMount
	Labels           map[string]string
	Annotations      map[string]string
	Env              []corev1.EnvVar
}

func (a *adkApiTranslator) resolveInlineDeployment(agent *v1alpha2.Agent, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	// Defaults
	port := int32(8080)
	cmd := "kagent-adk"
	args := []string{
		"static",
		"--host",
		"0.0.0.0",
		"--port",
		fmt.Sprintf("%d", port),
		"--filepath",
		"/config",
	}

	// Start with spec deployment spec
	spec := v1alpha2.DeclarativeDeploymentSpec{}
	if agent.Spec.Declarative.Deployment != nil {
		spec = *agent.Spec.Declarative.Deployment
	}
	registry := DefaultImageConfig.Registry
	if spec.ImageRegistry != "" {
		registry = spec.ImageRegistry
	}
	repository := DefaultImageConfig.Repository
	image := fmt.Sprintf("%s/%s:%s", registry, repository, DefaultImageConfig.Tag)

	imagePullPolicy := corev1.PullPolicy(DefaultImageConfig.PullPolicy)
	if spec.ImagePullPolicy != "" {
		imagePullPolicy = corev1.PullPolicy(spec.ImagePullPolicy)
	}

	dep := &resolvedDeployment{
		Image:            image,
		Cmd:              cmd,
		Args:             args,
		Port:             port,
		ImagePullPolicy:  imagePullPolicy,
		Replicas:         spec.Replicas,
		ImagePullSecrets: slices.Clone(spec.ImagePullSecrets),
		Volumes:          append(slices.Clone(spec.Volumes), mdd.Volumes...),
		VolumeMounts:     append(slices.Clone(spec.VolumeMounts), mdd.VolumeMounts...),
		Labels:           maps.Clone(spec.Labels),
		Annotations:      maps.Clone(spec.Annotations),
		Env:              append(slices.Clone(spec.Env), mdd.EnvVars...),
	}

	// Set default replicas if not specified
	if dep.Replicas == nil {
		dep.Replicas = ptr.To(int32(1))
	}

	return dep, nil
}

func (a *adkApiTranslator) resolveByoDeployment(agent *v1alpha2.Agent) (*resolvedDeployment, error) {
	spec := agent.Spec.BYO.Deployment
	if spec == nil {
		return nil, fmt.Errorf("BYO deployment spec is required")
	}

	// Defaults
	port := int32(8080)

	image := spec.Image
	if image == "" {
		// This should never happen as it's required by the API
		return nil, fmt.Errorf("image is required for BYO deployment")
	}

	cmd := ""
	if spec.Cmd != nil && *spec.Cmd != "" {
		cmd = *spec.Cmd
	}

	var args []string
	if len(spec.Args) != 0 {
		args = spec.Args
	}

	imagePullPolicy := corev1.PullIfNotPresent
	if spec.ImagePullPolicy != "" {
		imagePullPolicy = spec.ImagePullPolicy
	}

	replicas := spec.Replicas
	if replicas == nil {
		replicas = ptr.To(int32(1))
	}

	dep := &resolvedDeployment{
		Image:            image,
		Cmd:              cmd,
		Args:             args,
		Port:             port,
		ImagePullPolicy:  imagePullPolicy,
		Replicas:         replicas,
		ImagePullSecrets: slices.Clone(spec.ImagePullSecrets),
		Volumes:          slices.Clone(spec.Volumes),
		VolumeMounts:     slices.Clone(spec.VolumeMounts),
		Labels:           maps.Clone(spec.Labels),
		Annotations:      maps.Clone(spec.Annotations),
		Env:              slices.Clone(spec.Env),
	}

	return dep, nil
}
