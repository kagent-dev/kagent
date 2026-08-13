package translator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/kagent-dev/kagent/go/api/adk"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// modelDeploymentData collects the Kubernetes inputs required by a provider.
// Volumes are retained even though the current Substrate ActorTemplate path
// rejects them, so the compiler can report incompatibility instead of silently
// dropping credentials or custom CAs.
type modelDeploymentData struct {
	EnvVars      []corev1.EnvVar
	Volumes      []corev1.Volume
	VolumeMounts []corev1.VolumeMount
}

// modelRuntime is the provider-neutral result consumed by the rest of the v2
// compiler. data remains attached so MCP TLS requirements can be accumulated
// before the final Substrate compatibility check.
type modelRuntime struct {
	Model                 adk.Model
	Environment           []corev1.EnvVar
	SecretHash            []byte
	HasUnsupportedVolumes bool
	data                  *modelDeploymentData
}

// resolveModel collapses provider-specific translation output into the subset
// needed to compile a runtime revision.
func (c *Compiler) resolveModel(ctx context.Context, config *v1alpha3.ModelConfig) (*modelRuntime, error) {
	model, data, secretHash, err := c.translateModel(ctx, config)
	if err != nil {
		return nil, err
	}
	return &modelRuntime{
		Model: model, Environment: data.EnvVars, SecretHash: secretHash,
		HasUnsupportedVolumes: len(data.Volumes) > 0 || len(data.VolumeMounts) > 0,
		data:                  data,
	}, nil
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
// sources (the ModelConfig and RemoteMCPServers) in the same revision coexist
// without colliding. Repeated references to the same Secret reuse the same
// deterministic name and path.
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

// deriveTLSFields turns a v1alpha3.TLSConfig into the three pointer fields
// that every TLS-aware adk wire type carries (BaseModel,
// StreamableHTTPConnectionParams, SseConnectionParams). Returns nils for
// nil or all-zero configs so the caller can assign-through to all three
// fields in a single statement. Emitting explicit `false` booleans on
// an empty struct would flip the Python runtime out of its no-op
// short-circuit and silently swap google-adk's default httpx client for
// kagent's, which has the same SSL behavior but different
// timeout/redirect defaults.
func deriveTLSFields(tlsConfig *v1alpha3.TLSConfig) (*bool, *string, *bool) {
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
// Each provider embeds BaseModel, so this keeps the three TLS fields consistent
// across translateModel branches. MCP connection params carry the same fields
// but do not embed BaseModel, so those callers use deriveTLSFields directly.
func populateTLSFields(baseModel *adk.BaseModel, tlsConfig *v1alpha3.TLSConfig) {
	baseModel.TLSInsecureSkipVerify, baseModel.TLSCACertPath, baseModel.TLSDisableSystemCAs = deriveTLSFields(tlsConfig)
}

// addTLSConfiguration mounts a CA Secret as a per-Secret read-only volume on
// modelDeploymentData. Safe to call multiple times for the same agent with
// the same OR different TLSConfigs:
//   - different Secrets produce different volume names + paths and accumulate.
//   - the same Secret referenced from multiple sources is idempotent because a
//     volume with the same deterministic name is appended only once.
//
// ModelConfig and RemoteMCPServer reconciliation validate that the Secret and
// key exist. Translation preserves the requested mount and lets the public
// resources surface configuration errors through their Accepted conditions.
func addTLSConfiguration(modelDeploymentData *modelDeploymentData, tlsConfig *v1alpha3.TLSConfig) {
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
func addTokenExchangeConfiguration(openai *adk.OpenAI, mdd *modelDeploymentData, spec *v1alpha3.ModelConfigSpec) {
	if spec.OpenAI == nil || spec.OpenAI.TokenExchange == nil {
		return
	}
	tokenExchange := spec.OpenAI.TokenExchange
	switch tokenExchange.Type {
	case v1alpha3.TokenExchangeTypeGDCH:
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

// resolveFoundryEndpoint returns the Foundry endpoint, preferring the inline
// value and otherwise resolving it from the referenced ConfigMap (endpointFrom),
// which lets Azure Service Operator own the account endpoint.
func (c *Compiler) resolveFoundryEndpoint(ctx context.Context, namespace string, cfg *v1alpha3.FoundryConfig) (string, error) {
	if cfg.Endpoint != "" {
		return cfg.Endpoint, nil
	}
	if cfg.EndpointFrom == nil {
		return "", nil
	}
	ref := cfg.EndpointFrom
	cm := &corev1.ConfigMap{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, cm); err != nil {
		return "", fmt.Errorf("failed to get Foundry endpoint config map %s: %w", ref.Name, err)
	}
	value, ok := cm.Data[ref.Key]
	if !ok {
		if ref.Optional != nil && *ref.Optional {
			return "", nil
		}
		return "", fmt.Errorf("the Foundry endpoint config map %s does not contain key %q", ref.Name, ref.Key)
	}
	return value, nil
}

// translateModel owns the v2 ModelConfig-to-ADK mapping. The provider branches
// are intentionally local rather than calling the legacy translator: v2 can
// now evolve and eventually replace that code without a compatibility layer.
// It returns the ADK wire model, its Kubernetes runtime requirements, and the
// controller-computed credential hash used in revision identity.
func (c *Compiler) translateModel(ctx context.Context, model *v1alpha3.ModelConfig) (adk.Model, *modelDeploymentData, []byte, error) {
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
	case v1alpha3.ModelProviderOpenAI:
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
			if model.Spec.OpenAI.MaxCompletionTokens > 0 {
				openai.MaxCompletionTokens = &model.Spec.OpenAI.MaxCompletionTokens
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
			if model.Spec.OpenAI.APIFormat != nil && *model.Spec.OpenAI.APIFormat != "" {
				openai.APIFormat = string(*model.Spec.OpenAI.APIFormat)
			}

			if model.Spec.OpenAI.Organization != "" {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name:  env.OpenAIOrganization.Name(),
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, modelDeploymentData, secretHashBytes, nil
	case v1alpha3.ModelProviderAnthropic:
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
	case v1alpha3.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, nil, fmt.Errorf("AzureOpenAI model config is required")
		}
		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
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
			Endpoint:    model.Spec.AzureOpenAI.Endpoint,
			Deployment:  model.Spec.AzureOpenAI.DeploymentName,
			APIVersion:  model.Spec.AzureOpenAI.APIVersion,
			Temperature: utils.ParseStringToFloat64(model.Spec.AzureOpenAI.Temperature),
			TopP:        utils.ParseStringToFloat64(model.Spec.AzureOpenAI.TopP),
			MaxTokens:   model.Spec.AzureOpenAI.MaxTokens,
		}
		// Populate TLS fields in BaseModel
		populateTLSFields(&azureOpenAI.BaseModel, model.Spec.TLS)
		azureOpenAI.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return azureOpenAI, modelDeploymentData, secretHashBytes, nil
	case v1alpha3.ModelProviderGeminiVertexAI:
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

		if model.Spec.GeminiVertexAI.MaxOutputTokens > 0 {
			gemini.MaxOutputTokens = &model.Spec.GeminiVertexAI.MaxOutputTokens
		}

		return gemini, modelDeploymentData, secretHashBytes, nil
	case v1alpha3.ModelProviderAnthropicVertexAI:
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
	case v1alpha3.ModelProviderOllama:
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
	case v1alpha3.ModelProviderGemini:
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
		if model.Spec.Gemini != nil && model.Spec.Gemini.MaxOutputTokens > 0 {
			gemini.MaxOutputTokens = &model.Spec.Gemini.MaxOutputTokens
		}
		return gemini, modelDeploymentData, secretHashBytes, nil
	case v1alpha3.ModelProviderBedrock:
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
			if err := c.kube.Get(ctx, types.NamespacedName{Namespace: model.Namespace, Name: model.Spec.APIKeySecret}, secret); err != nil {
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
			ReadTimeout:                  model.Spec.Bedrock.ReadTimeout,
			ConnectTimeout:               model.Spec.Bedrock.ConnectTimeout,
		}
		if model.Spec.Bedrock.Guardrail != nil {
			bedrock.Guardrail = &adk.BedrockGuardrail{
				Identifier: model.Spec.Bedrock.Guardrail.Identifier,
				Version:    model.Spec.Bedrock.Guardrail.Version,
				Trace:      model.Spec.Bedrock.Guardrail.Trace,
			}
		}

		// Populate TLS fields in BaseModel
		populateTLSFields(&bedrock.BaseModel, model.Spec.TLS)
		bedrock.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return bedrock, modelDeploymentData, secretHashBytes, nil
	case v1alpha3.ModelProviderSAPAICore:
		if model.Spec.SAPAICore == nil {
			return nil, nil, nil, fmt.Errorf("sapAICore model config is required")
		}

		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			secret := &corev1.Secret{}
			if err := c.kube.Get(ctx, types.NamespacedName{Namespace: model.Namespace, Name: model.Spec.APIKeySecret}, secret); err != nil {
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
	case v1alpha3.ModelProviderFoundry:
		if model.Spec.Foundry == nil {
			return nil, nil, nil, fmt.Errorf("foundry model config is required")
		}
		cfg := model.Spec.Foundry

		// Resolve the endpoint, which may come from an inline value or from a
		// ConfigMap written by Azure Service Operator (endpointFrom).
		endpoint, err := c.resolveFoundryEndpoint(ctx, model.Namespace, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		if endpoint == "" {
			return nil, nil, nil, fmt.Errorf("foundry endpoint could not be resolved: set foundry.endpoint or a foundry.endpointFrom whose ConfigMap key exists")
		}

		// Implicit auth: mount the API key only when a secret is provided and
		// passthrough is off; otherwise the runtime uses DefaultAzureCredential
		// (Workload Identity) or the passed-through caller token.
		if !model.Spec.APIKeyPassthrough && model.Spec.APIKeySecret != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name: env.FoundryAPIKey.Name(),
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

		// Endpoint is validated above; Deployment (required) and APIVersion
		// (defaulted) are guaranteed by the CRD — all three are always set.
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars,
			corev1.EnvVar{
				Name:  env.FoundryEndpoint.Name(),
				Value: endpoint,
			},
			corev1.EnvVar{
				Name:  env.FoundryDeployment.Name(),
				Value: cfg.Deployment,
			},
			corev1.EnvVar{
				Name:  env.FoundryAPIVersion.Name(),
				Value: cfg.APIVersion,
			},
		)

		foundry := &adk.Foundry{
			BaseModel: adk.BaseModel{
				Model:   model.Spec.Model,
				Headers: model.Spec.DefaultHeaders,
			},
			Endpoint:   endpoint,
			Deployment: cfg.Deployment,
			APIVersion: cfg.APIVersion,
		}
		populateTLSFields(&foundry.BaseModel, model.Spec.TLS)
		foundry.APIKeyPassthrough = model.Spec.APIKeyPassthrough

		return foundry, modelDeploymentData, secretHashBytes, nil
	default:
		return nil, nil, nil, fmt.Errorf("unsupported model provider: %s", model.Spec.Provider)
	}
}
