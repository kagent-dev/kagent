package agent

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/adk"
	"github.com/kagent-dev/kagent/go/internal/controller/translator/labels"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/translator"
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

	ProxyHostHeader = "x-kagent-host"
)

type ImageConfig struct {
	Registry   string `json:"registry,omitempty"`
	Tag        string `json:"tag,omitempty"`
	PullPolicy string `json:"pullPolicy,omitempty"`
	PullSecret string `json:"pullSecret,omitempty"`
	Repository string `json:"repository,omitempty"`
}

var DefaultImageConfig = ImageConfig{
	Registry:   "cr.kagent.dev",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	PullSecret: "",
	Repository: "kagent-dev/kagent/app",
}

// TODO(ilackarms): migrate this whole package to pkg/translator
type AgentOutputs = translator.AgentOutputs

type AdkApiTranslator interface {
	TranslateAgent(
		ctx context.Context,
		agent *v1alpha2.Agent,
	) (*AgentOutputs, error)
	GetOwnedResourceTypes() []client.Object
}

type TranslatorPlugin = translator.TranslatorPlugin

func NewAdkApiTranslator(kube client.Client, defaultModelConfig types.NamespacedName, plugins []TranslatorPlugin, globalProxyURL string) AdkApiTranslator {
	return &adkApiTranslator{
		kube:               kube,
		defaultModelConfig: defaultModelConfig,
		plugins:            plugins,
		globalProxyURL:     globalProxyURL,
	}
}

type adkApiTranslator struct {
	kube               client.Client
	defaultModelConfig types.NamespacedName
	plugins            []TranslatorPlugin
	globalProxyURL     string
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
	err := a.validateAgent(ctx, agent, &tState{})
	if err != nil {
		return nil, err
	}

	var cfg *adk.AgentConfig
	var dep *resolvedDeployment
	var secretHashBytes []byte

	switch agent.Spec.Type {
	case v1alpha2.AgentType_Declarative:
		var mdd *modelDeploymentData
		cfg, mdd, secretHashBytes, err = a.translateInlineAgent(ctx, agent)
		if err != nil {
			return nil, err
		}
		dep, err = a.resolveInlineDeployment(agent, mdd)
		if err != nil {
			return nil, err
		}

	case v1alpha2.AgentType_BYO:

		dep, err = a.resolveByoDeployment(agent)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unknown agent type: %s", agent.Spec.Type)
	}

	card := GetA2AAgentCard(agent)

	return a.buildManifest(ctx, agent, dep, cfg, card, secretHashBytes)
}

// GetOwnedResourceTypes returns all the resource types that may be created for an agent.
// Even though this method returns an array of client.Object, these are (empty)
// example structs rather than actual resources.
func (r *adkApiTranslator) GetOwnedResourceTypes() []client.Object {
	ownedResources := []client.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.Service{},
		&corev1.ServiceAccount{},
	}

	for _, plugin := range r.plugins {
		ownedResources = append(ownedResources, plugin.GetOwnedResourceTypes()...)
	}

	return ownedResources
}

func (a *adkApiTranslator) validateAgent(ctx context.Context, agent *v1alpha2.Agent, state *tState) error {
	agentRef := utils.GetObjectRef(agent)

	if state.isVisited(agentRef) {
		return fmt.Errorf("cycle detected in agent tool chain: %s -> %s", agentRef, agentRef)
	}

	if state.depth > MAX_DEPTH {
		return fmt.Errorf("recursion limit reached in agent tool chain: %s -> %s", agentRef, agentRef)
	}

	if agent.Spec.Type != v1alpha2.AgentType_Declarative {
		// We only need to validate loops in declarative agents
		return nil
	}

	for _, tool := range agent.Spec.Declarative.Tools {
		if tool.Type != v1alpha2.ToolProviderType_Agent {
			continue
		}

		if tool.Agent == nil {
			return fmt.Errorf("tool must have an agent reference")
		}

		agentRef := types.NamespacedName{
			Namespace: agent.Namespace,
			Name:      tool.Agent.Name,
		}

		if agentRef.Namespace == agent.Namespace && agentRef.Name == agent.Name {
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

	return nil
}

func (a *adkApiTranslator) buildManifest(
	ctx context.Context,
	agent *v1alpha2.Agent,
	dep *resolvedDeployment,
	cfg *adk.AgentConfig, // nil for BYO
	card *server.AgentCard, // nil for BYO
	modelConfigSecretHashBytes []byte, // nil for BYO
) (*AgentOutputs, error) {
	outputs := &AgentOutputs{}

	// Optional config/card for Inline
	var cfgHash uint64
	var secretVol []corev1.Volume
	var secretMounts []corev1.VolumeMount
	var cfgJson string
	var agentCard string
	if cfg != nil && card != nil {
		bCfg, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		bCard, err := json.Marshal(card)
		if err != nil {
			return nil, err
		}
		// Include secret hash bytes in config hash to trigger redeployment on secret changes
		secretData := modelConfigSecretHashBytes
		if secretData == nil {
			secretData = []byte{}
		}
		cfgHash = computeConfigHash(bCfg, bCard, secretData)

		cfgJson = string(bCfg)
		agentCard = string(bCard)

		secretVol = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: agent.Name,
				},
			},
		}}
		secretMounts = []corev1.VolumeMount{{Name: "config", MountPath: "/config"}}
	}

	selectorLabels := map[string]string{
		"app":    labels.ManagedByKagent,
		"kagent": agent.Name,
	}
	podLabels := func() map[string]string {
		l := maps.Clone(selectorLabels)
		if dep.Labels != nil {
			maps.Copy(l, dep.Labels)
		}
		return l
	}

	objMeta := func() metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:        agent.Name,
			Namespace:   agent.Namespace,
			Annotations: agent.Annotations,
			Labels:      podLabels(),
		}
	}

	// Secret
	outputs.Manifest = append(outputs.Manifest, &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: objMeta(),
		StringData: map[string]string{
			"config.json":     cfgJson,
			"agent-card.json": agentCard,
		},
	})

	// Service Account
	outputs.Manifest = append(outputs.Manifest, &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: objMeta(),
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
			Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace()),
		},
	)

	var skills []string
	if agent.Spec.Skills != nil && len(agent.Spec.Skills.Refs) != 0 {
		skills = agent.Spec.Skills.Refs
	}

	// Build Deployment
	volumes := append(secretVol, dep.Volumes...)
	volumeMounts := append(secretMounts, dep.VolumeMounts...)
	needSandbox := cfg != nil && cfg.ExecuteCode

	var initContainers []corev1.Container

	if len(skills) > 0 {
		skillsEnv := corev1.EnvVar{
			Name:  "KAGENT_SKILLS_FOLDER",
			Value: "/skills",
		}
		needSandbox = true
		insecure := agent.Spec.Skills.InsecureSkipVerify
		command := []string{"kagent-adk", "pull-skills"}
		if insecure {
			command = append(command, "--insecure")
		}
		initContainerSecurityContext := dep.SecurityContext
		if initContainerSecurityContext != nil {
			initContainerSecurityContext = initContainerSecurityContext.DeepCopy()
		}
		initContainers = append(initContainers, corev1.Container{
			Name:    "skills-init",
			Image:   dep.Image,
			Command: command,
			Args:    skills,
			VolumeMounts: []corev1.VolumeMount{
				{Name: "kagent-skills", MountPath: "/skills"},
			},
			Env:             []corev1.EnvVar{skillsEnv},
			SecurityContext: initContainerSecurityContext,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "kagent-skills",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "kagent-skills",
			MountPath: "/skills",
			ReadOnly:  true,
		})
		sharedEnv = append(sharedEnv, skillsEnv)
	}

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

	var cmd []string
	if len(dep.Cmd) != 0 {
		cmd = []string{dep.Cmd}
	}

	podTemplateAnnotations := dep.Annotations
	if podTemplateAnnotations == nil {
		podTemplateAnnotations = map[string]string{}
	}
	// Add hash annotations to pod template to force rollout on agent config or model config secret changes
	podTemplateAnnotations["kagent.dev/config-hash"] = fmt.Sprintf("%d", cfgHash)

	// Merge container security context: start with user-provided, then apply sandbox requirements
	var securityContext *corev1.SecurityContext
	if dep.SecurityContext != nil {
		// Deep copy the user-provided security context
		securityContext = dep.SecurityContext.DeepCopy()
		// If sandbox is needed, ensure Privileged is set (may override user setting)
		if needSandbox {
			securityContext.Privileged = ptr.To(true)
		}
	} else if needSandbox {
		// Only create security context if sandbox is needed
		securityContext = &corev1.SecurityContext{
			Privileged: ptr.To(true),
		}
	}
	// If neither user-provided securityContext nor sandbox is needed, securityContext remains nil

	deployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: objMeta(),
		Spec: appsv1.DeploymentSpec{
			Replicas: dep.Replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels(), Annotations: podTemplateAnnotations},
				Spec: corev1.PodSpec{
					ServiceAccountName: agent.Name,
					ImagePullSecrets:   dep.ImagePullSecrets,
					SecurityContext:    dep.PodSecurityContext,
					InitContainers:     initContainers,
					Containers: []corev1.Container{{
						Name:            "kagent",
						Image:           dep.Image,
						ImagePullPolicy: dep.ImagePullPolicy,
						Command:         cmd,
						Args:            dep.Args,
						Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: dep.Port}},
						Resources:       dep.Resources,
						Env:             env,
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
							},
							InitialDelaySeconds: 15,
							TimeoutSeconds:      15,
							PeriodSeconds:       15,
						},
						SecurityContext: securityContext,
						VolumeMounts:    volumeMounts,
					}},
					Volumes:      volumes,
					Tolerations:  dep.Tolerations,
					Affinity:     dep.Affinity,
					NodeSelector: dep.NodeSelector,
				},
			},
		},
	}
	outputs.Manifest = append(outputs.Manifest, deployment)

	// Service
	outputs.Manifest = append(outputs.Manifest, &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: objMeta(),
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
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

	return outputs, a.runPlugins(ctx, agent, outputs)
}

func (a *adkApiTranslator) translateInlineAgent(ctx context.Context, agent *v1alpha2.Agent) (*adk.AgentConfig, *modelDeploymentData, []byte, error) {
	model, mdd, secretHashBytes, err := a.translateModel(ctx, agent.Namespace, agent.Spec.Declarative.ModelConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	systemMessage, err := a.resolveSystemMessage(ctx, agent)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg := &adk.AgentConfig{
		Description: agent.Spec.Description,
		Instruction: systemMessage,
		Model:       model,
		ExecuteCode: false && ptr.Deref(agent.Spec.Declarative.ExecuteCodeBlocks, false), //ignored due to this issue https://github.com/google/adk-python/issues/3921.
		Stream:      agent.Spec.Declarative.Stream,
	}

	for _, tool := range agent.Spec.Declarative.Tools {
		// Skip tools that are not applicable to the model provider
		switch {
		case tool.McpServer != nil:
			// Use proxy for MCP server/tool communication
			err := a.translateMCPServerTarget(ctx, cfg, agent.Namespace, tool.McpServer, tool.HeadersFrom, a.globalProxyURL)
			if err != nil {
				return nil, nil, nil, err
			}
		case tool.Agent != nil:
			agentRef := types.NamespacedName{
				Namespace: agent.Namespace,
				Name:      tool.Agent.Name,
			}

			if agentRef.Namespace == agent.Namespace && agentRef.Name == agent.Name {
				return nil, nil, nil, fmt.Errorf("agent tool cannot be used to reference itself, %s", agentRef)
			}

			// Translate a nested tool
			toolAgent := &v1alpha2.Agent{}
			err := a.kube.Get(ctx, agentRef, toolAgent)
			if err != nil {
				return nil, nil, nil, err
			}

			switch toolAgent.Spec.Type {
			case v1alpha2.AgentType_BYO, v1alpha2.AgentType_Declarative:
				originalURL := fmt.Sprintf("http://%s.%s:8080", toolAgent.Name, toolAgent.Namespace)
				headers, err := tool.ResolveHeaders(ctx, a.kube, agent.Namespace)
				if err != nil {
					return nil, nil, nil, err
				}

				// If proxy is configured, use proxy URL and set header for Gateway API routing
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

	return cfg, mdd, secretHashBytes, nil
}

func (a *adkApiTranslator) resolveSystemMessage(ctx context.Context, agent *v1alpha2.Agent) (string, error) {
	if agent.Spec.Declarative.SystemMessageFrom != nil {
		return agent.Spec.Declarative.SystemMessageFrom.Resolve(ctx, a.kube, agent.Namespace)
	}
	if agent.Spec.Declarative.SystemMessage != "" {
		return agent.Spec.Declarative.SystemMessage, nil
	}
	return "", fmt.Errorf("at least one system message source (SystemMessage or SystemMessageFrom) must be specified")
}

const (
	googleCredsVolumeName = "google-creds"
	tlsCACertVolumeName   = "tls-ca-cert"
	tlsCACertMountPath    = "/etc/ssl/certs/custom"
)

// populateTLSFields populates TLS configuration fields in the BaseModel
// from the ModelConfig TLS spec.
func populateTLSFields(baseModel *adk.BaseModel, tlsConfig *v1alpha2.TLSConfig) {
	if tlsConfig == nil {
		return
	}

	// Set TLS configuration fields in BaseModel
	baseModel.TLSDisableVerify = &tlsConfig.DisableVerify
	baseModel.TLSDisableSystemCAs = &tlsConfig.DisableSystemCAs

	// Set CA cert path if Secret and key are both specified
	if tlsConfig.CACertSecretRef != "" && tlsConfig.CACertSecretKey != "" {
		certPath := fmt.Sprintf("%s/%s", tlsCACertMountPath, tlsConfig.CACertSecretKey)
		baseModel.TLSCACertPath = &certPath
	}
}

// addTLSConfiguration adds TLS certificate volume mounts to modelDeploymentData
// when TLS configuration is present in the ModelConfig.
// Note: TLS configuration fields are now included in agent config JSON via BaseModel,
// so this function only handles volume mounting.
func addTLSConfiguration(modelDeploymentData *modelDeploymentData, tlsConfig *v1alpha2.TLSConfig) {
	if tlsConfig == nil {
		return
	}

	// Add Secret volume mount if both CA certificate Secret and key are specified
	if tlsConfig.CACertSecretRef != "" && tlsConfig.CACertSecretKey != "" {
		// Add volume from Secret
		modelDeploymentData.Volumes = append(modelDeploymentData.Volumes, corev1.Volume{
			Name: tlsCACertVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  tlsConfig.CACertSecretRef,
					DefaultMode: ptr.To(int32(0444)), // Read-only for all users
				},
			},
		})

		// Add volume mount
		modelDeploymentData.VolumeMounts = append(modelDeploymentData.VolumeMounts, corev1.VolumeMount{
			Name:      tlsCACertVolumeName,
			MountPath: tlsCACertMountPath,
			ReadOnly:  true,
		})
	}
}

func (a *adkApiTranslator) translateModel(ctx context.Context, namespace, modelConfig string) (adk.Model, *modelDeploymentData, []byte, error) {
	model := &v1alpha2.ModelConfig{}
	err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: modelConfig}, model)
	if err != nil {
		return nil, nil, nil, err
	}

	// Decode hex-encoded secret hash to bytes
	var secretHashBytes []byte
	if model.Status.SecretHash != "" {
		decoded, err := hex.DecodeString(model.Status.SecretHash)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode secret hash: %w", err)
		}
		secretHashBytes = decoded
	}

	modelDeploymentData := &modelDeploymentData{}

	// Add TLS configuration if present
	addTLSConfiguration(modelDeploymentData, model.Spec.TLS)

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
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&openai.BaseModel, model.Spec.TLS)

		if model.Spec.OpenAI != nil {
			openai.BaseUrl = model.Spec.OpenAI.BaseURL
			openai.Temperature = utils.ParseStringToFloat64(model.Spec.OpenAI.Temperature)
			openai.TopP = utils.ParseStringToFloat64(model.Spec.OpenAI.TopP)
			openai.FrequencyPenalty = utils.ParseStringToFloat64(model.Spec.OpenAI.FrequencyPenalty)
			openai.PresencePenalty = utils.ParseStringToFloat64(model.Spec.OpenAI.PresencePenalty)

			if model.Spec.OpenAI.MaxTokens > 0 {
				openai.MaxTokens = &model.Spec.OpenAI.MaxTokens
			}
			if model.Spec.OpenAI.Seed != nil {
				openai.Seed = model.Spec.OpenAI.Seed
			}
			if model.Spec.OpenAI.N != nil {
				openai.N = model.Spec.OpenAI.N
			}
			if model.Spec.OpenAI.Timeout != nil {
				openai.Timeout = model.Spec.OpenAI.Timeout
			}
			if model.Spec.OpenAI.ReasoningEffort != nil {
				effort := string(*model.Spec.OpenAI.ReasoningEffort)
				openai.ReasoningEffort = &effort
			}

			if model.Spec.OpenAI.Organization != "" {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name:  "OPENAI_ORGANIZATION",
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, modelDeploymentData, secretHashBytes, nil
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
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&anthropic.BaseModel, model.Spec.TLS)

		if model.Spec.Anthropic != nil {
			anthropic.BaseUrl = model.Spec.Anthropic.BaseURL
		}
		return anthropic, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, nil, fmt.Errorf("AzureOpenAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name: "AZURE_OPENAI_API_KEY",
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
				Name:  "OPENAI_API_VERSION",
				Value: model.Spec.AzureOpenAI.APIVersion,
			})
		}
		if model.Spec.AzureOpenAI.Endpoint != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  "AZURE_OPENAI_ENDPOINT",
				Value: model.Spec.AzureOpenAI.Endpoint,
			})
		}
		azureOpenAI := &adk.AzureOpenAI{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.AzureOpenAI.DeploymentName,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&azureOpenAI.BaseModel, model.Spec.TLS)

		return azureOpenAI, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderGeminiVertexAI:
		if model.Spec.GeminiVertexAI == nil {
			return nil, nil, nil, fmt.Errorf("GeminiVertexAI model config is required")
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
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&gemini.BaseModel, model.Spec.TLS)

		return gemini, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil {
			return nil, nil, nil, fmt.Errorf("AnthropicVertexAI model config is required")
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
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&anthropic.BaseModel, model.Spec.TLS)

		return anthropic, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderOllama:
		if model.Spec.Ollama == nil {
			return nil, nil, nil, fmt.Errorf("ollama model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "OLLAMA_API_BASE",
			Value: model.Spec.Ollama.Host,
		})
		ollama := &adk.Ollama{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&ollama.BaseModel, model.Spec.TLS)

		return ollama, modelDeploymentData, secretHashBytes, nil
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
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&gemini.BaseModel, model.Spec.TLS)

		return gemini, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderBedrock:
		if model.Spec.Bedrock == nil {
			return nil, nil, nil, fmt.Errorf("bedrock model config is required")
		}

		// Set AWS region (always required)
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  "AWS_REGION",
			Value: model.Spec.Bedrock.Region,
		})

		// If AWS_BEARER_TOKEN_BEDROCK key exists: use bearer token auth
		// Otherwise, use IAM credentials
		if model.Spec.APIKeySecret != "" {
			secret := &corev1.Secret{}
			if err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: model.Spec.APIKeySecret}, secret); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to get Bedrock credentials secret: %w", err)
			}

			if _, hasBearerToken := secret.Data["AWS_BEARER_TOKEN_BEDROCK"]; hasBearerToken {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: "AWS_BEARER_TOKEN_BEDROCK",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: "AWS_BEARER_TOKEN_BEDROCK",
						},
					},
				})
			} else {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: "AWS_ACCESS_KEY_ID",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: "AWS_ACCESS_KEY_ID",
						},
					},
				})
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: "AWS_SECRET_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: "AWS_SECRET_ACCESS_KEY",
						},
					},
				})
				// AWS_SESSION_TOKEN is optional, only needed for temporary/SSO credentials
				if _, hasSessionToken := secret.Data["AWS_SESSION_TOKEN"]; hasSessionToken {
					modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
						Name: "AWS_SESSION_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: model.Spec.APIKeySecret,
								},
								Key: "AWS_SESSION_TOKEN",
							},
						},
					})
				}
			}
		}
		bedrock := &adk.Bedrock{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
			Region: model.Spec.Bedrock.Region,
		}

		// Populate TLS fields in BaseModel
		populateTLSFields(&bedrock.BaseModel, model.Spec.TLS)

		return bedrock, modelDeploymentData, secretHashBytes, nil
	}

	return nil, nil, nil, fmt.Errorf("unknown model provider: %s", model.Spec.Provider)
}

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, tool *v1alpha2.RemoteMCPServerSpec, namespace string, proxyURL string) (*adk.StreamableHTTPConnectionParams, error) {
	headers, err := tool.ResolveHeaders(ctx, a.kube, namespace)
	if err != nil {
		return nil, err
	}

	// If proxy is configured, use proxy URL and set header for Gateway API routing
	targetURL := tool.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(tool.URL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	}

	params := &adk.StreamableHTTPConnectionParams{
		Url:     targetURL,
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

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, tool *v1alpha2.RemoteMCPServerSpec, namespace string, proxyURL string) (*adk.SseConnectionParams, error) {
	headers, err := tool.ResolveHeaders(ctx, a.kube, namespace)
	if err != nil {
		return nil, err
	}

	// If proxy is configured, use proxy URL and set header for Gateway API routing
	targetURL := tool.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(tool.URL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	}

	params := &adk.SseConnectionParams{
		Url:     targetURL,
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

func (a *adkApiTranslator) translateMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, agentNamespace string, toolServer *v1alpha2.McpServerTool, toolHeaders []v1alpha2.ValueRef, proxyURL string) error {
	gvk := toolServer.GroupKind()

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
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServer.Name}, mcpServer)
		if err != nil {
			return err
		}

		spec, err := ConvertMCPServerToRemoteMCPServer(mcpServer)
		if err != nil {
			return err
		}

		spec.HeadersFrom = append(spec.HeadersFrom, toolHeaders...)

		return a.translateRemoteMCPServerTarget(ctx, agent, agentNamespace, spec, toolServer.ToolNames, proxyURL)
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
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServer.Name}, remoteMcpServer)
		if err != nil {
			return err
		}

		remoteMcpServer.Spec.HeadersFrom = append(remoteMcpServer.Spec.HeadersFrom, toolHeaders...)

		// RemoteMCPServer uses user-supplied URLs, but if the URL points to an internal k8s service,
		// apply proxy to route through the gateway
		proxyURL := ""
		if a.globalProxyURL != "" && a.isInternalK8sURL(ctx, remoteMcpServer.Spec.URL, agentNamespace) {
			proxyURL = a.globalProxyURL
		}
		return a.translateRemoteMCPServerTarget(ctx, agent, agentNamespace, &remoteMcpServer.Spec, toolServer.ToolNames, proxyURL)
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
		err := a.kube.Get(ctx, types.NamespacedName{Namespace: agentNamespace, Name: toolServer.Name}, svc)
		if err != nil {
			return err
		}

		spec, err := ConvertServiceToRemoteMCPServer(svc)
		if err != nil {
			return err
		}

		spec.HeadersFrom = append(spec.HeadersFrom, toolHeaders...)

		return a.translateRemoteMCPServerTarget(ctx, agent, agentNamespace, spec, toolServer.ToolNames, proxyURL)

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
			// Look through ports to find AppProtocol = mcp
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

func (a *adkApiTranslator) translateRemoteMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, agentNamespace string, remoteMcpServer *v1alpha2.RemoteMCPServerSpec, toolNames []string, proxyURL string) error {
	switch remoteMcpServer.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		tool, err := a.translateSseHttpTool(ctx, remoteMcpServer, agentNamespace, proxyURL)
		if err != nil {
			return err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params: *tool,
			Tools:  toolNames,
		})
	default:
		tool, err := a.translateStreamableHttpTool(ctx, remoteMcpServer, agentNamespace, proxyURL)
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

// isInternalK8sURL checks if a URL points to an internal Kubernetes service.
// Internal k8s URLs follow the pattern: http://{name}.{namespace}:{port} or
// http://{name}.{namespace}.svc.cluster.local:{port}
// This method checks if the namespace exists in the cluster to determine if it's internal.
func (a *adkApiTranslator) isInternalK8sURL(ctx context.Context, urlStr, namespace string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return false
	}

	// Check if it ends with .svc.cluster.local (definitely internal)
	if strings.HasSuffix(hostname, ".svc.cluster.local") {
		return true
	}

	// Extract namespace from hostname pattern: {name}.{namespace}
	// Examples: test-mcp-server.kagent -> namespace is "kagent"
	parts := strings.Split(hostname, ".")
	if len(parts) == 2 {
		potentialNamespace := parts[1]

		// Check if this namespace exists in the cluster
		ns := &corev1.Namespace{}
		err := a.kube.Get(ctx, types.NamespacedName{Name: potentialNamespace}, ns)
		if err == nil {
			// Namespace exists, so this is an internal k8s URL
			return true
		}
		// If namespace doesn't exist, it's likely a TLD or external domain
	}

	return false
}

func applyProxyURL(originalURL, proxyURL string, headers map[string]string) (targetURL string, updatedHeaders map[string]string, err error) {
	// Parse original URL to extract path and hostname
	originalURLParsed, err := url.Parse(originalURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse original URL %q: %w", originalURL, err)
	}
	proxyURLParsed, err := url.Parse(proxyURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse proxy URL %q: %w", proxyURL, err)
	}

	// Use proxy URL with original path
	targetURL = fmt.Sprintf("%s://%s%s", proxyURLParsed.Scheme, proxyURLParsed.Host, originalURLParsed.Path)

	// Set header to original hostname (without port) for Gateway API routing
	updatedHeaders = headers
	if updatedHeaders == nil {
		updatedHeaders = make(map[string]string)
	}
	updatedHeaders[ProxyHostHeader] = originalURLParsed.Hostname()

	return targetURL, updatedHeaders, nil
}

func computeConfigHash(agentCfg, agentCard, secretData []byte) uint64 {
	hasher := sha256.New()
	hasher.Write(agentCfg)
	hasher.Write(agentCard)
	hasher.Write(secretData)
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
	Replicas           *int32
	ImagePullSecrets   []corev1.LocalObjectReference
	Volumes            []corev1.Volume
	VolumeMounts       []corev1.VolumeMount
	Labels             map[string]string
	Annotations        map[string]string
	Env                []corev1.EnvVar
	Resources          corev1.ResourceRequirements
	Tolerations        []corev1.Toleration
	Affinity           *corev1.Affinity
	NodeSelector       map[string]string
	SecurityContext    *corev1.SecurityContext
	PodSecurityContext *corev1.PodSecurityContext
}

// getDefaultResources sets default resource requirements if not specified
func getDefaultResources(spec *corev1.ResourceRequirements) corev1.ResourceRequirements {
	if spec == nil {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("384Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
	}
	return *spec
}

func getDefaultLabels(agentName string, incoming map[string]string) map[string]string {
	defaultLabels := map[string]string{
		labels.AppManagedBy: labels.ManagedByKagent,
		labels.AppPartOf:    labels.ManagedByKagent,
		labels.AppName:      agentName,
	}
	maps.Copy(defaultLabels, incoming)
	return defaultLabels
}

func (a *adkApiTranslator) resolveInlineDeployment(agent *v1alpha2.Agent, mdd *modelDeploymentData) (*resolvedDeployment, error) {
	// Defaults
	port := int32(8080)
	args := []string{
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

	if DefaultImageConfig.PullSecret != "" {
		// Only append if not already present
		alreadyPresent := a.checkPullSecretAlreadyPresent(spec)
		if !alreadyPresent {
			spec.ImagePullSecrets = append(spec.ImagePullSecrets, corev1.LocalObjectReference{Name: DefaultImageConfig.PullSecret})
		}
	}

	dep := &resolvedDeployment{
		Image:              image,
		Args:               args,
		Port:               port,
		ImagePullPolicy:    imagePullPolicy,
		Replicas:           spec.Replicas,
		ImagePullSecrets:   slices.Clone(spec.ImagePullSecrets),
		Volumes:            append(slices.Clone(spec.Volumes), mdd.Volumes...),
		VolumeMounts:       append(slices.Clone(spec.VolumeMounts), mdd.VolumeMounts...),
		Labels:             getDefaultLabels(agent.Name, spec.Labels),
		Annotations:        maps.Clone(spec.Annotations),
		Env:                append(slices.Clone(spec.Env), mdd.EnvVars...),
		Resources:          getDefaultResources(spec.Resources), // Set default resources if not specified
		Tolerations:        slices.Clone(spec.Tolerations),
		Affinity:           spec.Affinity,
		NodeSelector:       maps.Clone(spec.NodeSelector),
		SecurityContext:    spec.SecurityContext,
		PodSecurityContext: spec.PodSecurityContext,
	}

	return dep, nil
}

func (a *adkApiTranslator) checkPullSecretAlreadyPresent(spec v1alpha2.DeclarativeDeploymentSpec) bool {
	alreadyPresent := false
	for _, secret := range spec.ImagePullSecrets {
		if secret.Name == DefaultImageConfig.PullSecret {
			alreadyPresent = true
			break
		}
	}
	return alreadyPresent
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

	imagePullPolicy := corev1.PullPolicy(DefaultImageConfig.PullPolicy)
	if spec.ImagePullPolicy != "" {
		imagePullPolicy = corev1.PullPolicy(spec.ImagePullPolicy)
	}

	replicas := spec.Replicas
	if replicas == nil {
		replicas = ptr.To(int32(1))
	}

	dep := &resolvedDeployment{
		Image:              image,
		Cmd:                cmd,
		Args:               args,
		Port:               port,
		ImagePullPolicy:    imagePullPolicy,
		Replicas:           replicas,
		ImagePullSecrets:   slices.Clone(spec.ImagePullSecrets),
		Volumes:            slices.Clone(spec.Volumes),
		VolumeMounts:       slices.Clone(spec.VolumeMounts),
		Labels:             getDefaultLabels(agent.Name, spec.Labels),
		Annotations:        maps.Clone(spec.Annotations),
		Env:                slices.Clone(spec.Env),
		Resources:          getDefaultResources(spec.Resources), // Set default resources if not specified
		Tolerations:        slices.Clone(spec.Tolerations),
		Affinity:           spec.Affinity,
		NodeSelector:       maps.Clone(spec.NodeSelector),
		SecurityContext:    spec.SecurityContext,
		PodSecurityContext: spec.PodSecurityContext,
	}

	return dep, nil
}

func (a *adkApiTranslator) runPlugins(ctx context.Context, agent *v1alpha2.Agent, outputs *AgentOutputs) error {
	var errs error
	for _, plugin := range a.plugins {
		if err := plugin.ProcessAgent(ctx, agent, outputs); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
