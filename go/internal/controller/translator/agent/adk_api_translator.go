package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/controller/translator/labels"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"github.com/kagent-dev/kagent/go/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/adk"
	"github.com/kagent-dev/kagent/go/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/translator"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

	// DefaultMCPServerTimeout is the fallback connection timeout applied when
	// an MCPServer CRD resource does not have an explicit Timeout set (e.g.
	// objects created before the field was introduced). This value mirrors
	// the kubebuilder default on MCPServerSpec.Timeout in the kmcp CRD.
	DefaultMCPServerTimeout = 30 * time.Second
)

// ValidationError indicates a configuration error that requires user action to fix.
// These errors should not trigger exponential backoff retries.
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string {
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// NewValidationError creates a new ValidationError
func NewValidationError(format string, args ...any) error {
	return &ValidationError{Err: fmt.Errorf(format, args...)}
}

type ImageConfig struct {
	Registry   string `json:"registry,omitempty"`
	Tag        string `json:"tag,omitempty"`
	PullPolicy string `json:"pullPolicy,omitempty"`
	PullSecret string `json:"pullSecret,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// Image returns the fully qualified image reference (registry/repository:tag).
func (c ImageConfig) Image() string {
	return fmt.Sprintf("%s/%s:%s", c.Registry, c.Repository, c.Tag)
}

var DefaultImageConfig = ImageConfig{
	Registry:   "cr.kagent.dev",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	PullSecret: "",
	Repository: "kagent-dev/kagent/app",
}

// DefaultSkillsInitImageConfig is the image config for the skills-init container
// that clones skill repositories from Git and pulls OCI skill images.
var DefaultSkillsInitImageConfig = ImageConfig{
	Registry:   "cr.kagent.dev",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	Repository: "kagent-dev/kagent/skills-init",
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
		switch tool.Type {
		case v1alpha2.ToolProviderType_Agent:
			if tool.Agent == nil {
				return fmt.Errorf("tool must have an agent reference")
			}

			agentRef := tool.Agent.NamespacedName(agent.Namespace)

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

	// Service Account - only created if using the default name
	if *dep.ServiceAccountName == agent.Name {
		sa := &corev1.ServiceAccount{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ServiceAccount",
			},
			ObjectMeta: objMeta(),
		}
		if dep.ServiceAccountConfig != nil {
			if dep.ServiceAccountConfig.Labels != nil {
				if sa.Labels == nil {
					sa.Labels = make(map[string]string)
				}
				maps.Copy(sa.Labels, dep.ServiceAccountConfig.Labels)
			}
			if dep.ServiceAccountConfig.Annotations != nil {
				if sa.Annotations == nil {
					sa.Annotations = make(map[string]string)
				}
				maps.Copy(sa.Annotations, dep.ServiceAccountConfig.Annotations)
			}
		}
		outputs.Manifest = append(outputs.Manifest, sa)
	}

	// Base env for both types
	sharedEnv := make([]corev1.EnvVar, 0, 8)
	sharedEnv = append(sharedEnv, collectOtelEnvFromProcess()...)
	sharedEnv = append(sharedEnv,
		corev1.EnvVar{
			Name: env.KagentNamespace.Name(),
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		corev1.EnvVar{
			Name:  env.KagentName.Name(),
			Value: agent.Name,
		},
		corev1.EnvVar{
			Name:  env.KagentURL.Name(),
			Value: fmt.Sprintf("http://%s.%s:8083", utils.GetControllerName(), utils.GetResourceNamespace()),
		},
	)

	var skills []string
	var gitRefs []v1alpha2.GitRepo
	var gitAuthSecretRef *corev1.LocalObjectReference
	if agent.Spec.Skills != nil {
		skills = agent.Spec.Skills.Refs
		gitRefs = agent.Spec.Skills.GitRefs
		gitAuthSecretRef = agent.Spec.Skills.GitAuthSecretRef
	}
	hasSkills := len(skills) > 0 || len(gitRefs) > 0

	// Build Deployment
	volumes := append(secretVol, dep.Volumes...)
	volumeMounts := append(secretMounts, dep.VolumeMounts...)
	needSandbox := cfg != nil && cfg.ExecuteCode

	var initContainers []corev1.Container

	// Add shared skills volume and env var when any skills (OCI or git) are present
	if hasSkills {
		skillsEnv := corev1.EnvVar{
			Name:  env.KagentSkillsFolder.Name(),
			Value: "/skills",
		}
		needSandbox = true
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

		insecure := agent.Spec.Skills != nil && agent.Spec.Skills.InsecureSkipVerify
		container, skillsVolumes, err := buildSkillsInitContainer(gitRefs, gitAuthSecretRef, skills, insecure, dep.SecurityContext)
		if err != nil {
			return nil, fmt.Errorf("failed to build skills init container: %w", err)
		}
		initContainers = append(initContainers, container)
		volumes = append(volumes, skillsVolumes...)
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
					ServiceAccountName: *dep.ServiceAccountName,
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
		headers, err := tool.ResolveHeaders(ctx, a.kube, agent.Namespace)
		if err != nil {
			return nil, nil, nil, err
		}

		// Skip tools that are not applicable to the model provider
		switch {
		case tool.McpServer != nil:
			// Use proxy for MCP server/tool communication
			err := a.translateMCPServerTarget(ctx, cfg, agent.Namespace, tool.McpServer, headers, a.globalProxyURL)
			if err != nil {
				return nil, nil, nil, err
			}
		case tool.Agent != nil:
			agentRef := tool.Agent.NamespacedName(agent.Namespace)

			if agentRef.Namespace == agent.Namespace && agentRef.Name == agent.Name {
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
		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.OpenAIAPIKey.Name(),
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
		openai.APIKeyPassthrough = model.Spec.APIKeyPassthrough

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
					Name:  env.OpenAIOrganization.Name(),
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderAnthropic:
		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.AnthropicAPIKey.Name(),
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
		anthropic.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		if model.Spec.Anthropic != nil {
			anthropic.BaseUrl = model.Spec.Anthropic.BaseURL
		}
		return anthropic, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, nil, fmt.Errorf("AzureOpenAI model config is required")
		}
		if !model.Spec.APIKeyPassthrough {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.AzureOpenAIAPIKey.Name(),
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
		if model.Spec.AzureOpenAI.AzureADToken != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.AzureADToken.Name(),
				Value: model.Spec.AzureOpenAI.AzureADToken,
			})
		}
		if model.Spec.AzureOpenAI.APIVersion != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.OpenAIAPIVersion.Name(),
				Value: model.Spec.AzureOpenAI.APIVersion,
			})
		}
		if model.Spec.AzureOpenAI.Endpoint != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.AzureOpenAIEndpoint.Name(),
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
		azureOpenAI.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return azureOpenAI, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderGeminiVertexAI:
		if model.Spec.GeminiVertexAI == nil {
			return nil, nil, nil, fmt.Errorf("GeminiVertexAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudProject.Name(),
			Value: model.Spec.GeminiVertexAI.ProjectID,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudLocation.Name(),
			Value: model.Spec.GeminiVertexAI.Location,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleGenAIUseVertexAI.Name(),
			Value: "true",
		})
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.GoogleApplicationCredentials.Name(),
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
		gemini.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return gemini, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil {
			return nil, nil, nil, fmt.Errorf("AnthropicVertexAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudProject.Name(),
			Value: model.Spec.AnthropicVertexAI.ProjectID,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudLocation.Name(),
			Value: model.Spec.AnthropicVertexAI.Location,
		})
		if model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.GoogleApplicationCredentials.Name(),
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
		anthropic.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return anthropic, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderOllama:
		if model.Spec.Ollama == nil {
			return nil, nil, nil, fmt.Errorf("ollama model config is required")
		}
		host := model.Spec.Ollama.Host
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = "http://" + host
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.OllamaAPIBase.Name(),
			Value: host,
		})
		ollama := &adk.Ollama{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
			Options: model.Spec.Ollama.Options,
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&ollama.BaseModel, model.Spec.TLS)
		ollama.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return ollama, modelDeploymentData, secretHashBytes, nil
	case v1alpha2.ModelProviderGemini:
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name: env.GoogleAPIKey.Name(),
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
			Name:  env.AWSRegion.Name(),
			Value: model.Spec.Bedrock.Region,
		})

		// If AWS_BEARER_TOKEN_BEDROCK key exists: use bearer token auth
		// Otherwise, use IAM credentials
		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			secret := &corev1.Secret{}
			if err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: model.Spec.APIKeySecret}, secret); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to get Bedrock credentials secret: %w", err)
			}

			if _, hasBearerToken := secret.Data[env.AWSBearerTokenBedrock.Name()]; hasBearerToken {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: env.AWSBearerTokenBedrock.Name(),
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: env.AWSBearerTokenBedrock.Name(),
						},
					},
				})
			} else {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: env.AWSAccessKeyID.Name(),
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: env.AWSAccessKeyID.Name(),
						},
					},
				})
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name: env.AWSSecretAccessKey.Name(),
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: model.Spec.APIKeySecret,
							},
							Key: env.AWSSecretAccessKey.Name(),
						},
					},
				})
				// AWS_SESSION_TOKEN is optional, only needed for temporary/SSO credentials
				if _, hasSessionToken := secret.Data[env.AWSSessionToken.Name()]; hasSessionToken {
					modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
						Name: env.AWSSessionToken.Name(),
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: model.Spec.APIKeySecret,
								},
								Key: env.AWSSessionToken.Name(),
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
		bedrock.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return bedrock, modelDeploymentData, secretHashBytes, nil
	}

	return nil, nil, nil, fmt.Errorf("unknown model provider: %s", model.Spec.Provider)
}

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, server *v1alpha2.RemoteMCPServer, agentHeaders map[string]string, proxyURL string) (*adk.StreamableHTTPConnectionParams, error) {
	headers, err := server.ResolveHeaders(ctx, a.kube)
	if err != nil {
		return nil, err
	}
	// Agent headers override tool headers
	maps.Copy(headers, agentHeaders)

	// If proxy is configured, use proxy URL and set header for Gateway API routing
	targetURL := server.Spec.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(targetURL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	}

	params := &adk.StreamableHTTPConnectionParams{
		Url:     targetURL,
		Headers: headers,
	}
	if server.Spec.Timeout != nil {
		params.Timeout = ptr.To(server.Spec.Timeout.Seconds())
	}
	if server.Spec.SseReadTimeout != nil {
		params.SseReadTimeout = ptr.To(server.Spec.SseReadTimeout.Seconds())
	}
	if server.Spec.TerminateOnClose != nil {
		params.TerminateOnClose = server.Spec.TerminateOnClose
	}

	return params, nil
}

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, server *v1alpha2.RemoteMCPServer, agentHeaders map[string]string, proxyURL string) (*adk.SseConnectionParams, error) {
	headers, err := server.ResolveHeaders(ctx, a.kube)
	if err != nil {
		return nil, err
	}
	// Agent headers override tool headers
	maps.Copy(headers, agentHeaders)

	// If proxy is configured, use proxy URL and set header for Gateway API routing
	targetURL := server.Spec.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(targetURL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	}

	params := &adk.SseConnectionParams{
		Url:     targetURL,
		Headers: headers,
	}
	if server.Spec.Timeout != nil {
		params.Timeout = ptr.To(server.Spec.Timeout.Seconds())
	}
	if server.Spec.SseReadTimeout != nil {
		params.SseReadTimeout = ptr.To(server.Spec.SseReadTimeout.Seconds())
	}
	return params, nil
}

func (a *adkApiTranslator) translateMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, agentNamespace string, toolServer *v1alpha2.McpServerTool, agentHeaders map[string]string, proxyURL string) error {
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
		mcpServerRef := toolServer.NamespacedName(agentNamespace)

		err := a.kube.Get(ctx, mcpServerRef, mcpServer)
		if err != nil {
			return err
		}

		remoteMcpServer, err := ConvertMCPServerToRemoteMCPServer(mcpServer)
		if err != nil {
			return err
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, remoteMcpServer, toolServer, agentHeaders, proxyURL)

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
		remoteMcpServerRef := toolServer.NamespacedName(agentNamespace)

		err := a.kube.Get(ctx, remoteMcpServerRef, remoteMcpServer)
		if err != nil {
			return err
		}

		// RemoteMCPServer uses user-supplied URLs, but if the URL points to an internal k8s service,
		// apply proxy to route through the gateway
		proxyURL := ""
		if a.globalProxyURL != "" && a.isInternalK8sURL(ctx, remoteMcpServer.Spec.URL, agentNamespace) {
			proxyURL = a.globalProxyURL
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, remoteMcpServer, toolServer, agentHeaders, proxyURL)
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
		svcRef := toolServer.NamespacedName(agentNamespace)

		err := a.kube.Get(ctx, svcRef, svc)
		if err != nil {
			return err
		}

		remoteMcpServer, err := ConvertServiceToRemoteMCPServer(svc)
		if err != nil {
			return err
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, remoteMcpServer, toolServer, agentHeaders, proxyURL)
	default:
		return fmt.Errorf("unknown tool server type: %s", gvk)
	}
}

func (a *adkApiTranslator) translateRemoteMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, remoteMcpServer *v1alpha2.RemoteMCPServer, mcpServerTool *v1alpha2.McpServerTool, agentHeaders map[string]string, proxyURL string) error {
	switch remoteMcpServer.Spec.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		tool, err := a.translateSseHttpTool(ctx, remoteMcpServer, agentHeaders, proxyURL)
		if err != nil {
			return err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params:         *tool,
			Tools:          mcpServerTool.ToolNames,
			AllowedHeaders: mcpServerTool.AllowedHeaders,
		})
	default:
		tool, err := a.translateStreamableHttpTool(ctx, remoteMcpServer, agentHeaders, proxyURL)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params:         *tool,
			Tools:          mcpServerTool.ToolNames,
			AllowedHeaders: mcpServerTool.AllowedHeaders,
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

// isCommitSHA returns true if ref looks like a full 40-character hex commit SHA.
var commitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func isCommitSHA(ref string) bool {
	return commitSHARegex.MatchString(ref)
}

// gitSkillName returns the directory name for a git skill ref.
// If Name is set, it is used; otherwise the last path segment of the repo URL
// (with any .git suffix stripped) is used.
// Query parameters and fragments are stripped before extracting the base name.
func gitSkillName(ref v1alpha2.GitRepo) string {
	if ref.Name != "" {
		return ref.Name
	}
	// Parse the URL to strip query params and fragments
	u := ref.URL
	if parsed, err := url.Parse(u); err == nil {
		u = parsed.Path
		// If the path is empty (e.g. just a host), fall back to the raw URL
		if u == "" {
			u = ref.URL
		}
	}
	u = strings.TrimSuffix(u, ".git")
	return path.Base(u)
}

// validateSubPath rejects subPath values that are absolute or contain ".." traversal segments.
func validateSubPath(p string) error {
	if p == "" {
		return nil
	}
	if path.IsAbs(p) {
		return fmt.Errorf("skill subPath must be relative, got %q", p)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return fmt.Errorf("skill subPath must not contain '..', got %q", p)
	}
	return nil
}

// skillsInitData holds the template data for the unified skills-init script.
type skillsInitData struct {
	AuthMountPath string       // "/git-auth" or "" (for git auth)
	GitRefs       []gitRefData // git repos to clone
	OCIRefs       []ociRefData // OCI images to pull
	InsecureOCI   bool         // --insecure flag for krane
}

// gitRefData holds pre-computed fields for each git skill ref, used by the script template.
type gitRefData struct {
	URL      string
	Ref      string
	Dest     string // e.g. /skills/my-skill
	IsCommit bool   // true if Ref is a 40-char hex SHA
	SubPath  string // Path with trailing slash stripped
}

// ociRefData holds pre-computed fields for each OCI skill ref, used by the script template.
type ociRefData struct {
	Image string // full image ref e.g. ghcr.io/org/skill:v1
	Dest  string // /skills/<name>
}

//go:embed skills-init.sh.tmpl
var skillsInitScriptTmpl string

// skillsScriptTemplate is the shell script template for fetching skills from Git and OCI.
var skillsScriptTemplate = template.Must(template.New("skills-init").Parse(skillsInitScriptTmpl))

// buildSkillsScript renders the unified skills-init shell script.
func buildSkillsScript(data skillsInitData) (string, error) {
	var buf bytes.Buffer
	if err := skillsScriptTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render skills init script: %w", err)
	}
	return buf.String(), nil
}

// ociSkillName extracts a skill directory name from an OCI image reference.
// It takes the last path component of the repo (stripped of tag/digest).
func ociSkillName(imageRef string) string {
	ref := imageRef
	// Strip digest
	if i := strings.LastIndex(ref, "@"); i != -1 {
		ref = ref[:i]
	}
	// Strip tag (colon after the last slash is a tag, not a port)
	if i := strings.LastIndex(ref, ":"); i != -1 {
		if j := strings.LastIndex(ref, "/"); i > j {
			ref = ref[:i]
		}
	}
	return path.Base(ref)
}

// prepareSkillsInitData converts CRD values to the template-ready data struct.
// It validates subPaths and detects duplicate skill directory names.
func prepareSkillsInitData(
	gitRefs []v1alpha2.GitRepo,
	authSecretRef *corev1.LocalObjectReference,
	ociRefs []string,
	insecureOCI bool,
) (skillsInitData, error) {
	data := skillsInitData{
		InsecureOCI: insecureOCI,
	}

	if authSecretRef != nil {
		data.AuthMountPath = "/git-auth"
	}

	seen := make(map[string]bool)

	for _, ref := range gitRefs {
		subPath := strings.TrimSuffix(ref.Path, "/")
		if err := validateSubPath(subPath); err != nil {
			return skillsInitData{}, err
		}

		gitRef := ref.Ref
		if gitRef == "" {
			gitRef = "main"
		}
		ref.Ref = gitRef

		name := gitSkillName(ref)
		if seen[name] {
			return skillsInitData{}, fmt.Errorf("duplicate skill directory name %q", name)
		}
		seen[name] = true

		data.GitRefs = append(data.GitRefs, gitRefData{
			URL:      ref.URL,
			Ref:      gitRef,
			Dest:     "/skills/" + name,
			IsCommit: isCommitSHA(gitRef),
			SubPath:  subPath,
		})
	}

	for _, imageRef := range ociRefs {
		name := ociSkillName(imageRef)
		if seen[name] {
			return skillsInitData{}, fmt.Errorf("duplicate skill directory name %q", name)
		}
		seen[name] = true

		data.OCIRefs = append(data.OCIRefs, ociRefData{
			Image: imageRef,
			Dest:  "/skills/" + name,
		})
	}

	return data, nil
}

// buildSkillsInitContainer creates the unified init container and associated volumes
// for fetching skills from both Git repositories and OCI registries.
// If authSecretRef is non-nil a single Secret volume is created and mounted at /git-auth.
func buildSkillsInitContainer(
	gitRefs []v1alpha2.GitRepo,
	authSecretRef *corev1.LocalObjectReference,
	ociRefs []string,
	insecureOCI bool,
	securityContext *corev1.SecurityContext,
) (container corev1.Container, volumes []corev1.Volume, err error) {
	data, err := prepareSkillsInitData(gitRefs, authSecretRef, ociRefs, insecureOCI)
	if err != nil {
		return corev1.Container{}, nil, err
	}
	script, err := buildSkillsScript(data)
	if err != nil {
		return corev1.Container{}, nil, err
	}

	initSecCtx := securityContext
	if initSecCtx != nil {
		initSecCtx = initSecCtx.DeepCopy()
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "kagent-skills", MountPath: "/skills"},
	}

	// Mount single auth secret if provided
	if authSecretRef != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "git-auth",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: authSecretRef.Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "git-auth",
			MountPath: "/git-auth",
			ReadOnly:  true,
		})
	}

	container = corev1.Container{
		Name:            "skills-init",
		Image:           DefaultSkillsInitImageConfig.Image(),
		Command:         []string{"/bin/sh", "-c", script},
		VolumeMounts:    volumeMounts,
		SecurityContext: initSecCtx,
	}

	return container, volumes, nil
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
