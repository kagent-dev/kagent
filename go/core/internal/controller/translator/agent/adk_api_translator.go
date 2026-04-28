package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
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

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/translator"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// DefaultServiceAccountName is the global default ServiceAccount name for agent pods.
// When set, agent pods that don't specify an explicit serviceAccountName will use this
// instead of auto-creating a per-agent ServiceAccount.
var DefaultServiceAccountName string

// DefaultAgentPodLabels is a set of labels applied to all agent pod templates.
// Per-agent labels from the Agent CRD spec take precedence over these defaults.
var DefaultAgentPodLabels map[string]string

// DefaultAgentBindHost is the host address agent pods bind to.
// Defaults to "0.0.0.0" (IPv4 only). Set to "::" for dual-stack (IPv4+IPv6) support.
var DefaultAgentBindHost = "0.0.0.0"

// TODO(ilackarms): migrate this whole package to pkg/translator
type AgentOutputs = translator.AgentOutputs

type AdkApiTranslator interface {
	CompileAgent(
		ctx context.Context,
		agent v1alpha2.AgentObject,
	) (*AgentManifestInputs, error)
	BuildManifest(
		ctx context.Context,
		agent v1alpha2.AgentObject,
		inputs *AgentManifestInputs,
	) (*AgentOutputs, error)
	GetOwnedResourceTypes() []client.Object
}

// probeConfig holds readiness probe timing configuration
type probeConfig struct {
	InitialDelaySeconds int32
	TimeoutSeconds      int32
	PeriodSeconds       int32
}

// getRuntimeProbeConfig returns readiness probe configuration for a runtime
func getRuntimeProbeConfig(runtime v1alpha2.DeclarativeRuntime) probeConfig {
	switch runtime {
	case v1alpha2.DeclarativeRuntime_Go:
		return probeConfig{
			InitialDelaySeconds: 1,
			TimeoutSeconds:      5,
			PeriodSeconds:       1,
		}
	case v1alpha2.DeclarativeRuntime_Python:
		return probeConfig{
			InitialDelaySeconds: 15,
			TimeoutSeconds:      15,
			PeriodSeconds:       15,
		}
	default:
		// Default to Python timing (conservative)
		return probeConfig{
			InitialDelaySeconds: 15,
			TimeoutSeconds:      15,
			PeriodSeconds:       15,
		}
	}
}

type TranslatorPlugin = translator.TranslatorPlugin

func NewAdkApiTranslator(kube client.Client, defaultModelConfig types.NamespacedName, plugins []TranslatorPlugin, globalProxyURL string, sandboxBackend sandboxbackend.Backend) AdkApiTranslator {
	return NewAdkApiTranslatorWithWatchedNamespaces(kube, nil, defaultModelConfig, plugins, globalProxyURL, sandboxBackend)
}

func NewAdkApiTranslatorWithWatchedNamespaces(kube client.Client, watchedNamespaces []string, defaultModelConfig types.NamespacedName, plugins []TranslatorPlugin, globalProxyURL string, sandboxBackend sandboxbackend.Backend) AdkApiTranslator {
	return &adkApiTranslator{
		kube:               kube,
		watchedNamespaces:  watchedNamespaces,
		defaultModelConfig: defaultModelConfig,
		plugins:            plugins,
		globalProxyURL:     globalProxyURL,
		sandboxBackend:     sandboxBackend,
	}
}

type adkApiTranslator struct {
	kube               client.Client
	watchedNamespaces  []string
	defaultModelConfig types.NamespacedName
	plugins            []TranslatorPlugin
	globalProxyURL     string
	sandboxBackend     sandboxbackend.Backend
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

	// Startup watch/index setup already skips NoMatch resources, so return sandbox-owned
	// types whenever sandbox support is configured rather than probing the API too early.
	if r.sandboxBackend != nil {
		ownedResources = append(ownedResources, r.sandboxBackend.GetOwnedResourceTypes()...)
	}

	return ownedResources
}

const (
	googleCredsVolumeName = "google-creds"
	tlsCACertVolumeName   = "tls-ca-cert"
	tlsCACertMountPath    = "/etc/ssl/certs/custom"
	gdchCredsVolumeName   = "gdch-creds"
	gdchCredsMountPath    = "/gdch-creds"
)

// populateTLSFields populates TLS configuration fields in the BaseModel
// from the ModelConfig TLS spec.
func populateTLSFields(baseModel *adk.BaseModel, tlsConfig *v1alpha2.TLSConfig) {
	if tlsConfig == nil {
		return
	}

	// Set TLS configuration fields in BaseModel
	baseModel.TLSInsecureSkipVerify = &tlsConfig.DisableVerify
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
					DefaultMode: new(int32(0444)), // Read-only for all users
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

// addTokenExchangeConfiguration adds token exchange configuration to the OpenAI
// model and mounts the service account secret (referenced by the top-level
// apiKeySecret / apiKeySecretKey fields) as a file for google.auth to read.
// Token exchange is only supported for OpenAI-compatible endpoints (e.g., GDCH).
func addTokenExchangeConfiguration(openai *adk.OpenAI, mdd *modelDeploymentData, spec *v1alpha2.ModelConfigSpec) {
	if spec.OpenAI == nil || spec.OpenAI.TokenExchange == nil {
		return
	}
	tokenExchange := spec.OpenAI.TokenExchange
	switch tokenExchange.Type {
	case v1alpha2.TokenExchangeTypeGDCH:
		cfg := tokenExchange.GDCHServiceAccount
		if cfg == nil {
			return
		}
		saPath := fmt.Sprintf("%s/%s", gdchCredsMountPath, spec.APIKeySecretKey)
		openai.TokenExchange = &adk.TokenExchangeConfig{
			Type: string(tokenExchange.Type),
			GDCHServiceAccount: &adk.GDCHTokenExchangeConfig{
				ServiceAccountPath: saPath,
				Audience:           cfg.Audience,
			},
		}
		mdd.Volumes = append(mdd.Volumes, corev1.Volume{
			Name: gdchCredsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.APIKeySecret,
					DefaultMode: new(int32(0444)),
				},
			},
		})
		mdd.VolumeMounts = append(mdd.VolumeMounts, corev1.VolumeMount{
			Name:      gdchCredsVolumeName,
			MountPath: gdchCredsMountPath,
			ReadOnly:  true,
		})
	}
}

// translateEmbeddingConfig resolves the embedding ModelConfig and returns the
// EmbeddingConfig for the Python config JSON, the deployment data for the
// embedding model, and the raw secret hash bytes (caller decides whether to
// include them). The caller should use mergeDeploymentData to combine the
// returned deployment data with the existing deployment data.
func (a *adkApiTranslator) translateEmbeddingConfig(ctx context.Context, namespace, modelConfigName string) (*adk.EmbeddingConfig, *modelDeploymentData, []byte, error) {
	embModel, embMdd, embHash, err := a.translateModel(ctx, namespace, modelConfigName)
	if err != nil {
		return nil, nil, nil, err
	}

	return adk.ModelToEmbeddingConfig(embModel), embMdd, embHash, nil
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
		usingTokenExchange := model.Spec.OpenAI != nil && model.Spec.OpenAI.TokenExchange != nil
		if !model.Spec.APIKeyPassthrough && !usingTokenExchange && model.Spec.APIKeySecret != "" {
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
		// Populate TokenExchange fields (OpenAI-specific)
		addTokenExchangeConfiguration(openai, modelDeploymentData, &model.Spec)
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
	case v1alpha2.ModelProviderSAPAICore:
		if model.Spec.SAPAICore == nil {
			return nil, nil, nil, fmt.Errorf("sapAICore model config is required")
		}

		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			secret := &corev1.Secret{}
			if err := a.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: model.Spec.APIKeySecret}, secret); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to get SAP AI Core credentials secret: %w", err)
			}

			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.SAPAICoreClientID.Name(),
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecret,
						},
						Key: "client_id",
					},
				},
			})
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.SAPAICoreClientSecret.Name(),
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: model.Spec.APIKeySecret,
						},
						Key: "client_secret",
					},
				},
			})
		}

		sapAICore := &adk.SAPAICore{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
			BaseUrl:       model.Spec.SAPAICore.BaseURL,
			ResourceGroup: model.Spec.SAPAICore.ResourceGroup,
			AuthUrl:       model.Spec.SAPAICore.AuthURL,
		}

		populateTLSFields(&sapAICore.BaseModel, model.Spec.TLS)
		sapAICore.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return sapAICore, modelDeploymentData, secretHashBytes, nil
	default:
		return nil, nil, nil, fmt.Errorf("unsupported model provider: %s", model.Spec.Provider)
	}
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
		params.Timeout = new(server.Spec.Timeout.Seconds())
	}
	if server.Spec.SseReadTimeout != nil {
		params.SseReadTimeout = new(server.Spec.SseReadTimeout.Seconds())
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
		params.Timeout = new(server.Spec.Timeout.Seconds())
	}
	if server.Spec.SseReadTimeout != nil {
		params.SseReadTimeout = new(server.Spec.SseReadTimeout.Seconds())
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
			Params:          *tool,
			Tools:           mcpServerTool.ToolNames,
			AllowedHeaders:  mcpServerTool.AllowedHeaders,
			RequireApproval: mcpServerTool.RequireApproval,
		})
	default:
		tool, err := a.translateStreamableHttpTool(ctx, remoteMcpServer, agentHeaders, proxyURL)
		if err != nil {
			return err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params:          *tool,
			Tools:           mcpServerTool.ToolNames,
			AllowedHeaders:  mcpServerTool.AllowedHeaders,
			RequireApproval: mcpServerTool.RequireApproval,
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
		// Controller is using namespaced RBAC, so check if the namespace is watched
		if (apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err)) && len(a.watchedNamespaces) > 0 {
			return slices.Contains(a.watchedNamespaces, potentialNamespace)
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

// mergeDeploymentData adds env vars, volumes, and volume mounts from src into dst,
// skipping any that already exist in dst (by name for env/volumes, by mount path for mounts).
func mergeDeploymentData(dst, src *modelDeploymentData) {
	for _, se := range src.EnvVars {
		found := false
		for _, e := range dst.EnvVars {
			if e.Name == se.Name {
				found = true
				break
			}
		}
		if !found {
			dst.EnvVars = append(dst.EnvVars, se)
		}
	}
	for _, sv := range src.Volumes {
		found := false
		for _, v := range dst.Volumes {
			if v.Name == sv.Name {
				found = true
				break
			}
		}
		if !found {
			dst.Volumes = append(dst.Volumes, sv)
		}
	}
	for _, sm := range src.VolumeMounts {
		found := false
		for _, m := range dst.VolumeMounts {
			if m.MountPath == sm.MountPath {
				found = true
				break
			}
		}
		if !found {
			dst.VolumeMounts = append(dst.VolumeMounts, sm)
		}
	}
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
// If Name is set, it is used. Otherwise, if Path (in-repo directory) is set, the
// last path segment of Path is used. If Path is empty, the last path segment of
// the repo URL (with any .git suffix stripped) is used.
// Query parameters and fragments are stripped before extracting the base name from the URL.
func gitSkillName(ref v1alpha2.GitRepo) string {
	if n := strings.TrimSpace(ref.Name); n != "" {
		return n
	}
	if p := strings.Trim(strings.TrimSpace(ref.Path), "/"); p != "" {
		return path.Base(p)
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

var (
	scpLikeGitURLRegex = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):.+$`)

	// validHostPattern and validPortPattern are the security boundary that prevents
	// shell injection when host/port values are interpolated into the ssh-keyscan
	// commands in skills-init.sh.tmpl. Do NOT relax these patterns without auditing
	// every template site that references .Host or .Port.
	validHostPattern = regexp.MustCompile(`^[A-Za-z0-9.\-]+$`)
	validPortPattern = regexp.MustCompile(`^[0-9]+$`)
)

func gitSSHHost(rawURL string) (sshHostData, bool) {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		switch parsed.Scheme {
		case "ssh", "git+ssh":
			host := parsed.Hostname()
			if host == "" || !validHostPattern.MatchString(host) {
				return sshHostData{}, false
			}
			port := parsed.Port()
			if port == "22" {
				port = "" // 22 is the SSH default; omit to avoid redundant -p flag
			}
			if port != "" && !validPortPattern.MatchString(port) {
				return sshHostData{}, false
			}
			return sshHostData{
				Host: host,
				Port: port,
			}, true
		case "http", "https":
			return sshHostData{}, false
		}
	}

	if strings.Contains(rawURL, "://") {
		return sshHostData{}, false
	}

	matches := scpLikeGitURLRegex.FindStringSubmatch(rawURL)
	if len(matches) != 2 {
		return sshHostData{}, false
	}
	host := matches[1]
	if !validHostPattern.MatchString(host) {
		return sshHostData{}, false
	}

	return sshHostData{Host: host}, true
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
	AuthMountPath string        // "/git-auth" or "" (for git auth)
	GitRefs       []gitRefData  // git repos to clone
	OCIRefs       []ociRefData  // OCI images to pull
	InsecureOCI   bool          // --insecure flag for krane
	SSHHosts      []sshHostData // extra hosts to add to known_hosts via ssh-keyscan
}

// sshHostData holds the host and optional port for an SSH known_hosts entry.
type sshHostData struct {
	Host string // hostname or IP
	Port string // port number, empty means default (22)
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
	seenSSHHosts := make(map[string]bool)

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

		// SSH host collection is separate from the AuthMountPath block above
		// because it runs per-ref inside the loop, not once at the top level.
		if authSecretRef != nil {
			if sshHost, ok := gitSSHHost(ref.URL); ok {
				key := sshHost.Host + ":" + sshHost.Port
				if !seenSSHHosts[key] {
					seenSSHHosts[key] = true
					data.SSHHosts = append(data.SSHHosts, sshHost)
				}
			}
		}

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

	slices.SortFunc(data.SSHHosts, func(a, b sshHostData) int {
		if cmp := strings.Compare(a.Host, b.Host); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Port, b.Port)
	})

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
	env []corev1.EnvVar,
	resources corev1.ResourceRequirements,
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
		Env:             env,
		Resources:       resources,
	}

	return container, volumes, nil
}

func (a *adkApiTranslator) runPlugins(ctx context.Context, agent v1alpha2.AgentObject, outputs *AgentOutputs) error {
	var errs error
	for _, plugin := range a.plugins {
		if err := plugin.ProcessAgent(ctx, agent, outputs); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// allowPrivilegeEscalationExplicitlyFalse reports whether the security context
// has AllowPrivilegeEscalation explicitly set to false (PSS Restricted profile).
// This is used to detect when adding Privileged:true would create an invalid
// securityContext that Kubernetes refuses to admit.
func allowPrivilegeEscalationExplicitlyFalse(sc *corev1.SecurityContext) bool {
	return sc != nil && sc.AllowPrivilegeEscalation != nil && !*sc.AllowPrivilegeEscalation
}
