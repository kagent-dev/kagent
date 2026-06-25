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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/skillsinit"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/core/pkg/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/translator"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
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
	Digest     string `json:"digest,omitempty"` // OCI manifest digest (sha256:...), set at link time
	PullPolicy string `json:"pullPolicy,omitempty"`
	PullSecret string `json:"pullSecret,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// Image returns the fully qualified image reference (registry/repository:tag).
func (c ImageConfig) Image() string {
	return fmt.Sprintf("%s/%s:%s", c.Registry, c.Repository, c.Tag)
}

// PinnedImage returns registry/repository@sha256:... when Digest is set, otherwise Image().
func (c ImageConfig) PinnedImage() string {
	if digest := normalizeImageDigest(c.Digest); digest != "" {
		return fmt.Sprintf("%s/%s@%s", c.Registry, c.Repository, digest)
	}
	return c.Image()
}

func normalizeImageDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return ""
	}
	if strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return "sha256:" + strings.TrimPrefix(digest, "sha256:")
}

var DefaultImageConfig = ImageConfig{
	Registry:   "cr.kagent.dev",
	Tag:        version.Get().Version,
	PullPolicy: string(corev1.PullIfNotPresent),
	PullSecret: "",
	Repository: "kagent-dev/kagent/app",
}

// PythonADKImageDigest, GoADKImageDigest, and GoADKFullImageDigest are set at
// controller link time from the pushed runtime image manifest digests.
var PythonADKImageDigest string
var GoADKImageDigest string
var GoADKFullImageDigest string

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
	return NewAdkApiTranslatorWithWatchedNamespaces(kube, nil, defaultModelConfig, plugins, globalProxyURL, sandboxBackend, false)
}

func NewAdkApiTranslatorWithWatchedNamespaces(kube client.Client, watchedNamespaces []string, defaultModelConfig types.NamespacedName, plugins []TranslatorPlugin, globalProxyURL string, sandboxBackend sandboxbackend.Backend, mcpEgressPlaintext bool) AdkApiTranslator {
	return &adkApiTranslator{
		kube:               kube,
		watchedNamespaces:  watchedNamespaces,
		defaultModelConfig: defaultModelConfig,
		plugins:            plugins,
		globalProxyURL:     globalProxyURL,
		sandboxBackend:     sandboxBackend,
		mcpEgressPlaintext: mcpEgressPlaintext,
	}
}

type adkApiTranslator struct {
	kube               client.Client
	watchedNamespaces  []string
	defaultModelConfig types.NamespacedName
	plugins            []TranslatorPlugin
	globalProxyURL     string
	sandboxBackend     sandboxbackend.Backend
	// mcpEgressPlaintext, when true, makes the tool translation emit an
	// external RemoteMCPServer's URL in plaintext form (egress.RewriteURL)
	// instead of verbatim. The controller's tool-discovery dial calls the same
	// egress.RewriteURL on the same RMS, so the agent and the controller probe
	// the same endpoint when the egress feature is on.
	mcpEgressPlaintext bool
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
	tlsCAVolumePrefix     = "tls-ca-"
	tlsCAMountRoot        = "/etc/ssl/certs/custom"
	maxDNS1123LabelLen    = 63
	gdchCredsVolumeName   = "gdch-creds"
	gdchCredsMountPath    = "/gdch-creds"
)

// dns1123LabelRE matches RFC 1123 labels (lowercase alphanumeric + dashes,
// must start and end with alphanumeric). K8s volume names require this
// grammar — but K8s Secret names follow the looser DNS_SUBDOMAIN grammar
// (dots allowed, up to 253 chars), so a literal Secret name like
// `corp.ca` or cert-manager-style `mcp.example.com-tls` would fail volume
// name validation if embedded verbatim. tlsCAPaths hashes the name when
// it would violate this regex (or the length limit) for that reason.
var dns1123LabelRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// tlsCAPaths returns deterministic volume name, mount path, and cert file
// path for the given Secret reference. Per-Secret naming lets multiple TLS
// sources (chat ModelConfig + embedding ModelConfig + RemoteMCPServers) on
// the same agent pod coexist without colliding when their
// modelDeploymentData entries get merged (mergeDeploymentData dedupes by
// (Name, MountPath) for VolumeMounts and by Name for Volumes).
func tlsCAPaths(secretName, key string) (volumeName, mountPath, certPath string) {
	candidate := tlsCAVolumePrefix + secretName
	id := secretName
	if len(candidate) > maxDNS1123LabelLen || !dns1123LabelRE.MatchString(candidate) {
		h := sha256.Sum256([]byte(secretName))
		id = hex.EncodeToString(h[:])[:8]
	}
	volumeName = tlsCAVolumePrefix + id
	mountPath = path.Join(tlsCAMountRoot, id)
	certPath = path.Join(mountPath, key)
	return
}

// deriveTLSFields turns a v1alpha2.TLSConfig into the three pointer fields
// that every TLS-aware adk wire type carries (BaseModel,
// StreamableHTTPConnectionParams, SseConnectionParams). Returns nils for
// nil or all-zero configs so the caller can assign-through to all three
// fields in a single statement. Emitting explicit `false` booleans on
// an empty struct would flip the Python runtime out of its no-op
// short-circuit and silently swap google-adk's default httpx client for
// kagent's, which has the same SSL behavior but different
// timeout/redirect defaults.
func deriveTLSFields(tlsConfig *v1alpha2.TLSConfig) (*bool, *string, *bool) {
	if tlsConfig.IsEmpty() {
		return nil, nil, nil
	}
	insecureSkipVerify := &tlsConfig.DisableVerify
	disableSystemCAs := &tlsConfig.DisableSystemCAs
	var caCertPath *string
	if tlsConfig.CACertSecretRef != "" && tlsConfig.CACertSecretKey != "" {
		_, _, p := tlsCAPaths(tlsConfig.CACertSecretRef, tlsConfig.CACertSecretKey)
		caCertPath = &p
	}
	return insecureSkipVerify, caCertPath, disableSystemCAs
}

// populateTLSFields writes the derived TLS fields onto an adk.BaseModel.
// Used by every model-provider branch in translateBaseModel — each provider
// embeds BaseModel, so this single call replaces the explicit three-field
// assignment at each site. The MCP-connection params (StreamableHTTPConnectionParams,
// SseConnectionParams) carry the same three fields but do not embed BaseModel;
// those callers assign through deriveTLSFields directly.
func populateTLSFields(baseModel *adk.BaseModel, tlsConfig *v1alpha2.TLSConfig) {
	baseModel.TLSInsecureSkipVerify, baseModel.TLSCACertPath, baseModel.TLSDisableSystemCAs = deriveTLSFields(tlsConfig)
}

// addTLSConfiguration mounts a CA Secret as a per-Secret read-only volume on
// modelDeploymentData. Safe to call multiple times for the same agent with
// the same OR different TLSConfigs:
//   - different Secrets produce different volume names + paths and accumulate.
//   - the same Secret referenced from multiple sources (e.g. several RMSs
//     pointing at one shared corp-CA bundle) is idempotent — we skip the
//     append if a volume with the same name is already present, because the
//     RemoteMCPServer path (translateRemoteMCPServerTarget) appends directly to the
//     already-merged modelDeploymentData rather than through
//     mergeDeploymentData.
//
// Spec validation (Secret exists, named key present) is the reconciler's
// job for both ModelConfig and RemoteMCPServer — see the TLS branches of
// ReconcileKagentModelConfig and ReconcileKagentRemoteMCPServer. The
// translator trusts that the Status has already surfaced any
// misconfiguration; mounting an absent key here would crash the agent at
// startup, but the operator gets the early signal on the resource's own
// Accepted condition.
func addTLSConfiguration(modelDeploymentData *modelDeploymentData, tlsConfig *v1alpha2.TLSConfig) {
	if tlsConfig == nil {
		return
	}

	// A CA bundle requires both the Secret name and the key within it; with
	// either missing there is no file to mount (system-trust or disableVerify
	// paths set neither and fall through as a no-op).
	if tlsConfig.CACertSecretRef != "" && tlsConfig.CACertSecretKey != "" {
		volumeName, mountPath, _ := tlsCAPaths(tlsConfig.CACertSecretRef, tlsConfig.CACertSecretKey)

		for _, v := range modelDeploymentData.Volumes {
			if v.Name == volumeName {
				return
			}
		}

		modelDeploymentData.Volumes = append(modelDeploymentData.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  tlsConfig.CACertSecretRef,
					DefaultMode: new(int32(0444)), // Read-only for all users
				},
			},
		})

		modelDeploymentData.VolumeMounts = append(modelDeploymentData.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
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
					DefaultMode: new(int32(0444)), // Read-only for all users
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
			spec := model.Spec.Anthropic
			anthropic.BaseUrl = spec.BaseURL
			anthropic.Temperature = utils.ParseStringToFloat64(spec.Temperature)
			anthropic.TopP = utils.ParseStringToFloat64(spec.TopP)
			if spec.MaxTokens > 0 {
				anthropic.MaxTokens = &spec.MaxTokens
			}
			if spec.TopK > 0 {
				anthropic.TopK = &spec.TopK
			}
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
			Temperature: utils.ParseStringToFloat64(model.Spec.AzureOpenAI.Temperature),
			TopP:        utils.ParseStringToFloat64(model.Spec.AzureOpenAI.TopP),
			MaxTokens:   model.Spec.AzureOpenAI.MaxTokens,
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
		var additionalFields map[string]any
		if model.Spec.Bedrock.AdditionalModelRequestFields != nil {
			if err := json.Unmarshal(model.Spec.Bedrock.AdditionalModelRequestFields.Raw, &additionalFields); err != nil {
				return nil, nil, nil, fmt.Errorf("failed to unmarshal bedrock additionalModelRequestFields: %w", err)
			}
		}
		bedrock := &adk.Bedrock{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
			Region:                       model.Spec.Bedrock.Region,
			AdditionalModelRequestFields: additionalFields,
			PromptCaching:                model.Spec.Bedrock.PromptCaching,
			CacheTTL:                     model.Spec.Bedrock.CacheTTL,
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

func (a *adkApiTranslator) translateStreamableHttpTool(ctx context.Context, server *v1alpha2.RemoteMCPServer, agentHeaders map[string]string, proxyURL string, egressRewrite bool) (*adk.StreamableHTTPConnectionParams, error) {
	headers, err := server.ResolveHeaders(ctx, a.kube)
	if err != nil {
		return nil, err
	}
	// Agent headers override tool headers
	maps.Copy(headers, agentHeaders)

	// Proxy and egress are mutually exclusive (decided by the caller): a proxy
	// URL routes through the Gateway API; an egress rewrite downgrades the URL
	// to plaintext so traffic egresses to a TLS-originating proxy.
	targetURL := server.Spec.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(targetURL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	} else if egressRewrite {
		targetURL = egress.RewriteURL(server)
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
	params.TLSInsecureSkipVerify, params.TLSCACertPath, params.TLSDisableSystemCAs = deriveTLSFields(server.Spec.TLS)

	return params, nil
}

func (a *adkApiTranslator) translateSseHttpTool(ctx context.Context, server *v1alpha2.RemoteMCPServer, agentHeaders map[string]string, proxyURL string, egressRewrite bool) (*adk.SseConnectionParams, error) {
	headers, err := server.ResolveHeaders(ctx, a.kube)
	if err != nil {
		return nil, err
	}
	// Agent headers override tool headers
	maps.Copy(headers, agentHeaders)

	// Proxy and egress are mutually exclusive (decided by the caller): a proxy
	// URL routes through the Gateway API; an egress rewrite downgrades the URL
	// to plaintext so traffic egresses to a TLS-originating proxy.
	targetURL := server.Spec.URL
	if proxyURL != "" {
		targetURL, headers, err = applyProxyURL(targetURL, proxyURL, headers)
		if err != nil {
			return nil, err
		}
	} else if egressRewrite {
		targetURL = egress.RewriteURL(server)
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
	params.TLSInsecureSkipVerify, params.TLSCACertPath, params.TLSDisableSystemCAs = deriveTLSFields(server.Spec.TLS)
	return params, nil
}

func (a *adkApiTranslator) translateMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, mdd *modelDeploymentData, agentNamespace string, toolServer *v1alpha2.McpServerTool, agentHeaders map[string]string, proxyURL string) ([]byte, error) {
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
			return nil, err
		}

		remoteMcpServer, err := ConvertMCPServerToRemoteMCPServer(mcpServer)
		if err != nil {
			return nil, err
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, mdd, remoteMcpServer, toolServer, agentHeaders, proxyURL, false)

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
			return nil, err
		}

		// RemoteMCPServer uses user-supplied URLs. An internal k8s URL routes
		// through the gateway proxy; otherwise, when the egress gate is on, the
		// external upstream is rewritten to its plaintext form so traffic
		// egresses to a TLS-originating proxy. The two are mutually exclusive:
		// proxy wins for internal URLs.
		proxyURL := ""
		egressRewrite := false
		if a.globalProxyURL != "" && a.isInternalK8sURL(ctx, remoteMcpServer.Spec.URL, agentNamespace) {
			proxyURL = a.globalProxyURL
		} else if a.mcpEgressPlaintext {
			egressRewrite = true
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, mdd, remoteMcpServer, toolServer, agentHeaders, proxyURL, egressRewrite)
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
			return nil, err
		}

		remoteMcpServer, err := ConvertServiceToRemoteMCPServer(svc)
		if err != nil {
			return nil, err
		}

		return a.translateRemoteMCPServerTarget(ctx, agent, mdd, remoteMcpServer, toolServer, agentHeaders, proxyURL, false)
	default:
		return nil, fmt.Errorf("unknown tool server type: %s", gvk)
	}
}

func (a *adkApiTranslator) translateRemoteMCPServerTarget(ctx context.Context, agent *adk.AgentConfig, mdd *modelDeploymentData, remoteMcpServer *v1alpha2.RemoteMCPServer, mcpServerTool *v1alpha2.McpServerTool, agentHeaders map[string]string, proxyURL string, egressRewrite bool) ([]byte, error) {
	switch remoteMcpServer.Spec.Protocol {
	case v1alpha2.RemoteMCPServerProtocolSse:
		tool, err := a.translateSseHttpTool(ctx, remoteMcpServer, agentHeaders, proxyURL, egressRewrite)
		if err != nil {
			return nil, err
		}
		agent.SseTools = append(agent.SseTools, adk.SseMcpServerConfig{
			Params:          *tool,
			Tools:           mcpServerTool.ToolNames,
			AllowedHeaders:  mcpServerTool.AllowedHeaders,
			RequireApproval: mcpServerTool.RequireApproval,
		})
	default:
		tool, err := a.translateStreamableHttpTool(ctx, remoteMcpServer, agentHeaders, proxyURL, egressRewrite)
		if err != nil {
			return nil, err
		}
		agent.HttpTools = append(agent.HttpTools, adk.HttpMcpServerConfig{
			Params:          *tool,
			Tools:           mcpServerTool.ToolNames,
			AllowedHeaders:  mcpServerTool.AllowedHeaders,
			RequireApproval: mcpServerTool.RequireApproval,
		})
	}
	// Mount the CA Secret on the agent pod when the RemoteMCPServer pins a TLS bundle.
	// Converters that synthesize RMSs from in-cluster MCPServer/Service
	// references don't set Spec.TLS, so this is a no-op for those. Returns
	// the controller-resolved TLS Secret hash so callers can mix it into
	// the agent's config hash — that's the signal that drives a rollout
	// when the CA Secret rotates in place (same Secret name, new PEM).
	addTLSConfiguration(mdd, remoteMcpServer.Spec.TLS)
	return remoteMCPServerSecretHashBytes(remoteMcpServer), nil
}

// remoteMCPServerSecretHashBytes returns the hex-decoded bytes of the
// RMS's Status.SecretHash so the agent translator can fold them into the
// agent's config hash. Returns nil (no contribution, no error) when the
// status hash is empty or malformed — the controller is responsible for
// keeping Status.SecretHash in sync, and a transient missing/garbage
// value should not block agent translation.
func remoteMCPServerSecretHashBytes(remoteMcpServer *v1alpha2.RemoteMCPServer) []byte {
	if remoteMcpServer == nil || remoteMcpServer.Status.SecretHash == "" {
		return nil
	}
	decoded, err := hex.DecodeString(remoteMcpServer.Status.SecretHash)
	if err != nil {
		return nil
	}
	return decoded
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

func computeConfigHash(agentCfg, agentCard, secretData, skillsInitCfg []byte) uint64 {
	hasher := sha256.New()
	hasher.Write(agentCfg)
	hasher.Write(agentCard)
	hasher.Write(secretData)
	hasher.Write(skillsInitCfg)
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
		// Dedupe by (Name, MountPath). MountPath-only dedupe would silently
		// drop a mount from a different Volume that happened to choose the
		// same path; matching on both lets kubelet surface the conflict
		// loudly instead.
		found := false
		for _, m := range dst.VolumeMounts {
			if m.Name == sm.Name && m.MountPath == sm.MountPath {
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
	var envVars []corev1.EnvVar
	for _, envVar := range os.Environ() {
		if !strings.HasPrefix(envVar, "OTEL_") {
			continue
		}
		name, value, found := strings.Cut(envVar, "=")
		if !found {
			continue
		}
		envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
	}
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

	// validHostPattern and validPortPattern are input-hygiene patterns for SSH
	// host/port values. They used to be a shell-injection boundary when these
	// values were interpolated into the rendered shell script; the
	// skills-init container is now driven by a structured JSON config so
	// values reach ssh-keyscan as argv entries and shell metacharacters are
	// inert. We keep the patterns to reject obvious garbage early.
	validHostPattern = regexp.MustCompile(`^[A-Za-z0-9.\-]+$`)
	validPortPattern = regexp.MustCompile(`^[0-9]+$`)
)

func gitSSHHost(rawURL string) (skillsinit.SSHHost, bool) {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		switch parsed.Scheme {
		case "ssh", "git+ssh":
			host := parsed.Hostname()
			if host == "" || !validHostPattern.MatchString(host) {
				return skillsinit.SSHHost{}, false
			}
			port := parsed.Port()
			if port == "22" {
				port = "" // 22 is the SSH default; omit to avoid redundant -p flag
			}
			if port != "" && !validPortPattern.MatchString(port) {
				return skillsinit.SSHHost{}, false
			}
			return skillsinit.SSHHost{
				Host: host,
				Port: port,
			}, true
		case "http", "https":
			return skillsinit.SSHHost{}, false
		}
	}

	if strings.Contains(rawURL, "://") {
		return skillsinit.SSHHost{}, false
	}

	matches := scpLikeGitURLRegex.FindStringSubmatch(rawURL)
	if len(matches) != 2 {
		return skillsinit.SSHHost{}, false
	}
	host := matches[1]
	if !validHostPattern.MatchString(host) {
		return skillsinit.SSHHost{}, false
	}

	return skillsinit.SSHHost{Host: host}, true
}

// validSkillNamePattern restricts skill directory names to a safe alphabet.
// The name becomes the final path segment under /skills/, so anything beyond
// [a-zA-Z0-9._-] (notably "/" and "..") could escape the skills volume.
var validSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validateSkillName rejects names that would escape /skills/ or look like
// dotfiles that hide the skill.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("skill name %q is not a valid directory name", name)
	}
	if !validSkillNamePattern.MatchString(name) {
		return fmt.Errorf("skill name %q must match %s", name, validSkillNamePattern)
	}
	return nil
}

// validateSubPath rejects subPath values that are absolute or contain ".." traversal segments.
func validateSubPath(p string) error {
	if p == "" {
		return nil
	}
	// filepath.IsLocal rejects absolute paths, ".." segments, and anything
	// else that can't be a local relative path — exactly the threat model here.
	if !filepath.IsLocal(p) {
		return fmt.Errorf("skill subPath must be a relative path without '..' segments, got %q", p)
	}
	return nil
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

// prepareSkillsInitConfig converts CRD values into the JSON config consumed by
// the skills-init binary. It validates subPaths and detects duplicate skill
// directory names. User-controlled strings (URL, ref, name, OCI image) flow
// through this struct as data only — the binary passes them to git/library
// calls as argv vectors, never as shell input.
func prepareSkillsInitConfig(
	gitRefs []v1alpha2.GitRepo,
	authSecretRef *corev1.LocalObjectReference,
	ociRefs []string,
	insecureOCI bool,
	imagePullSecrets []string,
) (skillsinit.Config, error) {
	cfg := skillsinit.Config{
		InsecureOCI:      insecureOCI,
		ImagePullSecrets: imagePullSecrets,
	}

	if authSecretRef != nil {
		cfg.AuthMountPath = skillsinit.AuthMountPath
	}

	seen := make(map[string]bool)
	seenSSHHosts := make(map[string]bool)

	for _, ref := range gitRefs {
		subPath := strings.TrimSuffix(ref.Path, "/")
		if err := validateSubPath(subPath); err != nil {
			return skillsinit.Config{}, err
		}

		gitRef := ref.Ref
		if gitRef == "" {
			gitRef = "main"
		}
		ref.Ref = gitRef

		name := gitSkillName(ref)
		if err := validateSkillName(name); err != nil {
			return skillsinit.Config{}, fmt.Errorf("git skill %q: %w", ref.URL, err)
		}
		if seen[name] {
			return skillsinit.Config{}, fmt.Errorf("duplicate skill directory name %q", name)
		}
		seen[name] = true

		// SSH host collection runs per-ref inside the loop, not once at the
		// top level, because the host comes from the per-ref URL.
		if authSecretRef != nil {
			if sshHost, ok := gitSSHHost(ref.URL); ok {
				key := sshHost.Host + ":" + sshHost.Port
				if !seenSSHHosts[key] {
					seenSSHHosts[key] = true
					cfg.SSHHosts = append(cfg.SSHHosts, sshHost)
				}
			}
		}

		cfg.GitRefs = append(cfg.GitRefs, skillsinit.GitRef{
			URL:     ref.URL,
			Ref:     gitRef,
			Dest:    skillsinit.SkillsDir + "/" + name,
			Full:    isCommitSHA(gitRef),
			SubPath: subPath,
		})
	}

	for _, imageRef := range ociRefs {
		name := ociSkillName(imageRef)
		if err := validateSkillName(name); err != nil {
			return skillsinit.Config{}, fmt.Errorf("oci skill %q: %w", imageRef, err)
		}
		if seen[name] {
			return skillsinit.Config{}, fmt.Errorf("duplicate skill directory name %q", name)
		}
		seen[name] = true

		cfg.OCIRefs = append(cfg.OCIRefs, skillsinit.OCIRef{
			Image: imageRef,
			Dest:  skillsinit.SkillsDir + "/" + name,
		})
	}

	slices.SortFunc(cfg.SSHHosts, func(a, b skillsinit.SSHHost) int {
		if cmp := strings.Compare(a.Host, b.Host); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Port, b.Port)
	})

	return cfg, nil
}

// SkillsInitConfigMapSuffix is appended to the Agent name to form the
// ConfigMap that carries the skills-init container's JSON config.
const SkillsInitConfigMapSuffix = "-skills-init"

// SkillsInitConfigMapName returns the name of the skills-init ConfigMap for
// the given Agent.
func SkillsInitConfigMapName(agentName string) string {
	return agentName + SkillsInitConfigMapSuffix
}

// validateSkillsInitConfigMapName enforces the K8s DNS-1123 subdomain rules
// on the derived ConfigMap name. Agent names are already constrained by the
// CRD, but the suffix can push borderline names over the 253-char limit, so
// we fail fast here with a clear message rather than letting the apiserver
// reject the eventual write.
func validateSkillsInitConfigMapName(name string) error {
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf("derived skills-init ConfigMap name %q is invalid: %s", name, strings.Join(errs, "; "))
	}
	return nil
}

// buildSkillsInitContainer assembles the init container, its volumes, and the
// ConfigMap holding its JSON configuration. The container runs a kagent-owned
// Go binary that consumes the ConfigMap; no shell is involved, so
// user-controlled CRD fields cannot inject commands.
//
// If authSecretRef is non-nil a Secret is mounted at AuthMountPath.
// If imagePullSecrets is non-empty, each kubernetes.io/dockerconfigjson secret
// is mounted under DockerSecretsDir/<name>; the binary merges them into a
// single config.json and sets DOCKER_CONFIG for the OCI client library.
func buildSkillsInitContainer(
	agentName, agentNamespace string,
	gitRefs []v1alpha2.GitRepo,
	authSecretRef *corev1.LocalObjectReference,
	ociRefs []string,
	insecureOCI bool,
	securityContext *corev1.SecurityContext,
	envVars []corev1.EnvVar,
	resources corev1.ResourceRequirements,
	imagePullSecrets []corev1.LocalObjectReference,
) (containers []corev1.Container, volumes []corev1.Volume, configMap *corev1.ConfigMap, err error) {
	pullSecretNames := make([]string, len(imagePullSecrets))
	for i, s := range imagePullSecrets {
		pullSecretNames[i] = s.Name
	}

	cfg, err := prepareSkillsInitConfig(gitRefs, authSecretRef, ociRefs, insecureOCI, pullSecretNames)
	if err != nil {
		return nil, nil, nil, err
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal skills-init config: %w", err)
	}

	cmName := SkillsInitConfigMapName(agentName)
	if err := validateSkillsInitConfigMapName(cmName); err != nil {
		return nil, nil, nil, err
	}
	configMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: agentNamespace,
		},
		Data: map[string]string{
			skillsinit.ConfigMapKey: string(cfgJSON),
		},
	}

	initSecCtx := securityContext
	if initSecCtx != nil {
		initSecCtx = initSecCtx.DeepCopy()
	}

	const configVolumeName = "skills-init-config"
	volumes = append(volumes, corev1.Volume{
		Name: configVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: cmName},
			},
		},
	})
	volumeMounts := []corev1.VolumeMount{
		{Name: "kagent-skills", MountPath: skillsinit.SkillsDir},
		{Name: configVolumeName, MountPath: skillsinit.ConfigMountPath, ReadOnly: true},
	}

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
			MountPath: skillsinit.AuthMountPath,
			ReadOnly:  true,
		})
	}

	for _, secret := range imagePullSecrets {
		volName := "pull-secret-" + secret.Name
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: skillsinit.DockerSecretsDir + "/" + secret.Name,
			ReadOnly:  true,
		})
	}

	// Command is intentionally omitted: the skills-init image's ENTRYPOINT
	// is the single source of truth for the binary path.
	skillsInitContainer := corev1.Container{
		Name:            "skills-init",
		Image:           DefaultSkillsInitImageConfig.Image(),
		VolumeMounts:    volumeMounts,
		SecurityContext: initSecCtx,
		Env:             envVars,
		Resources:       resources,
	}

	containers = append(containers, skillsInitContainer)
	return containers, volumes, configMap, nil
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
