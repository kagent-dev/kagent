package adkconfig

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"

	runtimeconfig "github.com/kagent-dev/kagent/go/adk/pkg/config"
	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// modelDeploymentData holds provider-specific non-secret SDK settings.
type modelDeploymentData struct{ EnvVars []corev1.EnvVar }

// modelRuntime is the provider-neutral result consumed by the compiler.
type modelRuntime struct {
	Model       *configv1.ADKModelConfig
	Environment []corev1.EnvVar
}

// resolveModel collapses provider-specific translation output into the subset
// needed to compile a runtime revision.
func resolveModel(resolved *translator.ResolvedModelConfig) (*modelRuntime, error) {
	model, data, err := translateModel(resolved)
	if err != nil {
		return nil, err
	}
	headers, passthrough, environment, err := translator.ModelCredentialHeaders(resolved, model.Headers)
	if err != nil {
		return nil, err
	}
	model.Headers, model.PassthroughHeaders = headers, passthrough
	data.EnvVars = append(data.EnvVars, environment...)
	return &modelRuntime{
		Model: model, Environment: data.EnvVars,
	}, nil
}

// tlsInsecureSkipVerify leaves empty configs unset to preserve SDK client defaults.
func tlsInsecureSkipVerify(tlsConfig *v1alpha3.TLSConfig) *bool {
	if tlsConfig.IsEmpty() {
		return nil
	}
	return &tlsConfig.DisableVerify
}

func populateTLSFields(baseModel *configv1.ADKModelConfig, tlsConfig *v1alpha3.TLSConfig) {
	baseModel.TlsInsecureSkipVerify = tlsInsecureSkipVerify(tlsConfig)
}

// translateModel owns the ModelConfig-to-ADK mapping. The provider branches
// are intentionally local so runtime-specific requirements stay explicit.
// It returns the ADK wire model and its Kubernetes runtime requirements.
func translateModel(resolved *translator.ResolvedModelConfig) (*configv1.ADKModelConfig, *modelDeploymentData, error) {
	if resolved == nil || resolved.Config == nil {
		return nil, nil, fmt.Errorf("resolved model config is required")
	}
	model := resolved.Config
	modelDeploymentData := &modelDeploymentData{}

	switch model.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:

		openai := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Openai{Openai: &configv1.ADKOpenAI{}},
		}
		// Populate shared transport settings.
		populateTLSFields(openai, model.Spec.TLS)

		if model.Spec.OpenAI != nil {
			openai.GetOpenai().BaseUrl = model.Spec.OpenAI.BaseURL
			openai.GetOpenai().Temperature = utils.ParseStringToFloat64(model.Spec.OpenAI.Temperature)
			openai.GetOpenai().TopP = utils.ParseStringToFloat64(model.Spec.OpenAI.TopP)
			openai.GetOpenai().FrequencyPenalty = utils.ParseStringToFloat64(model.Spec.OpenAI.FrequencyPenalty)
			openai.GetOpenai().PresencePenalty = utils.ParseStringToFloat64(model.Spec.OpenAI.PresencePenalty)

			if model.Spec.OpenAI.MaxTokens > 0 {
				openai.GetOpenai().MaxTokens = runtimeInt(&model.Spec.OpenAI.MaxTokens)
			}
			if model.Spec.OpenAI.MaxCompletionTokens > 0 {
				openai.GetOpenai().MaxCompletionTokens = runtimeInt(&model.Spec.OpenAI.MaxCompletionTokens)
			}
			if model.Spec.OpenAI.Seed != nil {
				openai.GetOpenai().Seed = runtimeInt(model.Spec.OpenAI.Seed)
			}
			if model.Spec.OpenAI.N != nil {
				openai.GetOpenai().N = runtimeInt(model.Spec.OpenAI.N)
			}
			if model.Spec.OpenAI.Timeout != nil {
				openai.GetOpenai().Timeout = runtimeDuration(model.Spec.OpenAI.Timeout)
			}
			if model.Spec.OpenAI.ReasoningEffort != nil {
				effort := string(*model.Spec.OpenAI.ReasoningEffort)
				openai.GetOpenai().ReasoningEffort = &effort
			}
			if model.Spec.OpenAI.APIFormat != nil && *model.Spec.OpenAI.APIFormat != "" {
				openai.GetOpenai().ApiFormat = string(*model.Spec.OpenAI.APIFormat)
			}

			if model.Spec.OpenAI.Organization != "" {
				modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
					Name:  env.OpenAIOrganization.Name(),
					Value: model.Spec.OpenAI.Organization,
				})
			}
		}
		return openai, modelDeploymentData, nil
	case v1alpha3.ModelProviderAnthropic:

		anthropic := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Anthropic{Anthropic: &configv1.ADKAnthropic{}},
		}
		// Populate shared transport settings.
		populateTLSFields(anthropic, model.Spec.TLS)

		if model.Spec.Anthropic != nil {
			spec := model.Spec.Anthropic
			anthropic.GetAnthropic().BaseUrl = spec.BaseURL
			anthropic.GetAnthropic().Temperature = utils.ParseStringToFloat64(spec.Temperature)
			anthropic.GetAnthropic().TopP = utils.ParseStringToFloat64(spec.TopP)
			if spec.MaxTokens > 0 {
				anthropic.GetAnthropic().MaxTokens = runtimeInt(&spec.MaxTokens)
			}
			if spec.TopK > 0 {
				anthropic.GetAnthropic().TopK = runtimeInt(&spec.TopK)
			}
			anthropic.GetAnthropic().PromptCaching = spec.PromptCaching
			anthropic.GetAnthropic().CacheTtl = spec.CacheTTL
		}
		return anthropic, modelDeploymentData, nil
	case v1alpha3.ModelProviderAzureOpenAI:
		if model.Spec.AzureOpenAI == nil {
			return nil, nil, fmt.Errorf("AzureOpenAI model config is required")
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
		azureOpenAI := &configv1.ADKModelConfig{
			Model:   model.Spec.AzureOpenAI.DeploymentName,
			Headers: model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_AzureOpenai{AzureOpenai: &configv1.ADKAzureOpenAI{
				Endpoint:    model.Spec.AzureOpenAI.Endpoint,
				Deployment:  model.Spec.AzureOpenAI.DeploymentName,
				ApiVersion:  model.Spec.AzureOpenAI.APIVersion,
				Temperature: utils.ParseStringToFloat64(model.Spec.AzureOpenAI.Temperature),
				TopP:        utils.ParseStringToFloat64(model.Spec.AzureOpenAI.TopP),
				MaxTokens:   runtimeInt(model.Spec.AzureOpenAI.MaxTokens),
			}},
		}
		// Populate shared transport settings.
		populateTLSFields(azureOpenAI, model.Spec.TLS)

		if model.Spec.AzureOpenAI.APIFormat != nil && *model.Spec.AzureOpenAI.APIFormat != "" {
			azureOpenAI.GetAzureOpenai().ApiFormat = string(*model.Spec.AzureOpenAI.APIFormat)
		}

		return azureOpenAI, modelDeploymentData, nil
	case v1alpha3.ModelProviderGeminiVertexAI:
		if model.Spec.GeminiVertexAI == nil {
			return nil, nil, fmt.Errorf("GeminiVertexAI model config is required")
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

		gemini := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_GeminiVertexAi{GeminiVertexAi: &configv1.ADKGeminiVertexAI{}},
		}
		// Populate shared transport settings.
		populateTLSFields(gemini, model.Spec.TLS)

		if model.Spec.GeminiVertexAI.MaxOutputTokens > 0 {
			gemini.GetGeminiVertexAi().MaxOutputTokens = runtimeInt(&model.Spec.GeminiVertexAI.MaxOutputTokens)
		}

		return gemini, modelDeploymentData, nil
	case v1alpha3.ModelProviderAnthropicVertexAI:
		if model.Spec.AnthropicVertexAI == nil {
			return nil, nil, fmt.Errorf("AnthropicVertexAI model config is required")
		}
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudProject.Name(),
			Value: model.Spec.AnthropicVertexAI.ProjectID,
		})
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.GoogleCloudLocation.Name(),
			Value: model.Spec.AnthropicVertexAI.Location,
		})

		anthropic := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_GeminiAnthropic{GeminiAnthropic: &configv1.ADKGeminiAnthropic{}},
		}
		// Populate shared transport settings.
		populateTLSFields(anthropic, model.Spec.TLS)

		return anthropic, modelDeploymentData, nil
	case v1alpha3.ModelProviderOllama:
		if model.Spec.Ollama == nil {
			return nil, nil, fmt.Errorf("ollama model config is required")
		}
		// Only set a host when the operator gave one. An empty value keeps
		// OLLAMA_API_BASE unset so the runtime applies its own cloud/local
		// routing; writing a default here would look operator-chosen and pin
		// every cloud model to the local daemon.
		if model.Spec.Ollama.Host != "" {
			modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
				Name:  env.OllamaAPIBase.Name(),
				Value: withDefaultScheme(model.Spec.Ollama.Host),
			})
		}

		ollama := &configv1.ADKModelConfig{
			Model:   model.Spec.Model,
			Headers: model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Ollama{Ollama: &configv1.ADKOllama{
				Options: model.Spec.Ollama.Options,
			}},
		}
		// Populate shared transport settings.
		populateTLSFields(ollama, model.Spec.TLS)

		return ollama, modelDeploymentData, nil
	case v1alpha3.ModelProviderGemini:

		gemini := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Gemini{Gemini: &configv1.ADKGemini{}},
		}
		// Populate shared transport settings.
		populateTLSFields(gemini, model.Spec.TLS)
		if model.Spec.Gemini != nil && model.Spec.Gemini.MaxOutputTokens > 0 {
			gemini.GetGemini().MaxOutputTokens = runtimeInt(&model.Spec.Gemini.MaxOutputTokens)
		}
		return gemini, modelDeploymentData, nil
	case v1alpha3.ModelProviderBedrock:
		if model.Spec.Bedrock == nil {
			return nil, nil, fmt.Errorf("bedrock model config is required")
		}

		// Set AWS region (always required)
		modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, corev1.EnvVar{
			Name:  env.AWSRegion.Name(),
			Value: model.Spec.Bedrock.Region,
		})

		var additionalFields []byte
		if model.Spec.Bedrock.AdditionalModelRequestFields != nil {
			additionalFields = slices.Clone(model.Spec.Bedrock.AdditionalModelRequestFields.Raw)
		}
		bedrock := &configv1.ADKModelConfig{
			Model:   model.Spec.Model,
			Headers: model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Bedrock{Bedrock: &configv1.ADKBedrock{
				Region:                       model.Spec.Bedrock.Region,
				AdditionalModelRequestFields: additionalFields,
				PromptCaching:                model.Spec.Bedrock.PromptCaching,
				CacheTtl:                     model.Spec.Bedrock.CacheTTL,
				ReadTimeout:                  runtimeDuration(model.Spec.Bedrock.ReadTimeout),
				ConnectTimeout:               runtimeDuration(model.Spec.Bedrock.ConnectTimeout),
			}},
		}
		if model.Spec.Bedrock.Guardrail != nil {
			bedrock.GetBedrock().Guardrail = &configv1.ADKBedrockGuardrail{
				Identifier: model.Spec.Bedrock.Guardrail.Identifier,
				Version:    model.Spec.Bedrock.Guardrail.Version,
				Trace:      model.Spec.Bedrock.Guardrail.Trace,
			}
		}

		// Populate shared transport settings.
		populateTLSFields(bedrock, model.Spec.TLS)

		return bedrock, modelDeploymentData, nil
	case v1alpha3.ModelProviderSAPAICore:
		if model.Spec.SAPAICore == nil {
			return nil, nil, fmt.Errorf("sapAICore model config is required")
		}

		sapAICore := &configv1.ADKModelConfig{
			Model:   model.Spec.Model,
			Headers: model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_SapAiCore{SapAiCore: &configv1.ADKSAPAICore{
				BaseUrl:       model.Spec.SAPAICore.BaseURL,
				ResourceGroup: model.Spec.SAPAICore.ResourceGroup,
				AuthUrl:       model.Spec.SAPAICore.AuthURL,
			}},
		}

		populateTLSFields(sapAICore, model.Spec.TLS)

		return sapAICore, modelDeploymentData, nil
	case v1alpha3.ModelProviderFoundry:
		if model.Spec.Foundry == nil {
			return nil, nil, fmt.Errorf("foundry model config is required")
		}
		cfg := model.Spec.Foundry

		endpoint := resolved.FoundryEndpoint

		// The shared resolver supplies the endpoint; Deployment (required) and
		// APIVersion (defaulted) are guaranteed by the CRD.
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

		// Map the CRD API format (OpenAI default | Anthropic) to the ADK wire
		// surface. Anthropic-format Foundry models are served over the Claude
		// Messages API instead of the OpenAI-compatible surface.
		apiFormat := runtimeconfig.FoundryAPIFormatOpenAI
		if cfg.APIFormat == v1alpha3.FoundryAPIFormatAnthropic {
			apiFormat = runtimeconfig.FoundryAPIFormatAnthropic
		}

		foundry := &configv1.ADKModelConfig{
			Model:   model.Spec.Model,
			Headers: model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Foundry{Foundry: &configv1.ADKFoundry{
				Endpoint:   endpoint,
				Deployment: cfg.Deployment,
				ApiVersion: cfg.APIVersion,
				ApiFormat:  apiFormat,
			}},
		}
		populateTLSFields(foundry, model.Spec.TLS)

		return foundry, modelDeploymentData, nil
	case v1alpha3.ModelProviderMistral:

		mistral := &configv1.ADKModelConfig{
			Model:    model.Spec.Model,
			Headers:  model.Spec.DefaultHeaders,
			Provider: &configv1.ADKModelConfig_Mistral{Mistral: &configv1.ADKMistral{}},
		}
		populateTLSFields(mistral, model.Spec.TLS)

		if model.Spec.Mistral != nil {
			spec := model.Spec.Mistral
			if spec.BaseURL != nil {
				mistral.GetMistral().BaseUrl = *spec.BaseURL
			}
			if spec.Temperature != nil {
				mistral.GetMistral().Temperature = utils.ParseStringToFloat64(*spec.Temperature)
			}
			if spec.TopP != nil {
				mistral.GetMistral().TopP = utils.ParseStringToFloat64(*spec.TopP)
			}
			if spec.MaxTokens != nil {
				mistral.GetMistral().MaxTokens = runtimeInt(spec.MaxTokens)
			}
			if spec.Timeout != nil {
				mistral.GetMistral().Timeout = runtimeDuration(spec.Timeout)
			}
		}

		return mistral, modelDeploymentData, nil
	default:
		return nil, nil, fmt.Errorf("unsupported model provider: %s", model.Spec.Provider)
	}
}

func runtimeInt(value *int) *int64 {
	if value == nil {
		return nil
	}
	return new(int64(*value))
}

func runtimeDuration(seconds *int) *durationpb.Duration {
	if seconds == nil {
		return nil
	}
	// Validate the protobuf before converting seconds to a native duration.
	return &durationpb.Duration{Seconds: int64(*seconds)}
}
