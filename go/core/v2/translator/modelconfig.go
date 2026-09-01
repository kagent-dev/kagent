package translator

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
)

// BedrockAuthentication identifies the credential shape selected from a
// resolved Bedrock credentials Secret without retaining its values.
type BedrockAuthentication string

const (
	BedrockAuthenticationNone   BedrockAuthentication = ""
	BedrockAuthenticationBearer BedrockAuthentication = "bearer"
	BedrockAuthenticationIAM    BedrockAuthentication = "iam"
)

// ModelConfigReference records an object consulted while resolving a ModelConfig.
// It intentionally contains object identity only; secret values remain in Kubernetes.
type ModelConfigReference struct {
	NamespacedName types.NamespacedName
	Kind           string
	Key            string
}

// ModelConfigFailure describes either an invalid ModelConfig or an unavailable
// Kubernetes input it references.
type ModelConfigFailure struct {
	Reason  string
	Message string
}

// ResolvedModelConfig is the harness-neutral ModelConfig input. Harness adapters
// render it into their runtime-specific model and credential configuration.
type ResolvedModelConfig struct {
	Config                *v1alpha3.ModelConfig
	FoundryEndpoint       string
	BedrockAuthentication BedrockAuthentication
	BedrockSessionToken   bool
	References            []ModelConfigReference
	SemanticFailures      []ModelConfigFailure
	ReferenceFailures     []ModelConfigFailure
}

func (r ResolvedModelConfig) ResourceName() string {
	if r.Config == nil {
		return ""
	}
	return r.Config.Namespace + "/" + r.Config.Name
}

func (r ResolvedModelConfig) Equals(other ResolvedModelConfig) bool {
	return apiequality.Semantic.DeepEqual(r, other)
}

// Usable reports whether every intrinsic configuration requirement and every
// referenced Kubernetes input was resolved.
func (r *ResolvedModelConfig) Usable() bool {
	return r != nil && r.Config != nil && len(r.SemanticFailures) == 0 && len(r.ReferenceFailures) == 0
}

// Failure returns the first diagnostic suitable for reporting at a compilation
// boundary. Reconciliation should inspect both failure lists to report status.
func (r *ResolvedModelConfig) Failure() *ModelConfigFailure {
	if r == nil || r.Config == nil {
		return &ModelConfigFailure{Reason: "ModelConfigMissing", Message: "model config is required"}
	}
	if len(r.SemanticFailures) > 0 {
		return &r.SemanticFailures[0]
	}
	if len(r.ReferenceFailures) > 0 {
		return &r.ReferenceFailures[0]
	}
	return nil
}

// ResolveModelConfig validates and resolves ModelConfig data shared by every
// harness. It does not expose secret values or produce runtime-specific inputs.
func ResolveModelConfig(ctx krt.HandlerContext, collections Collections, config *v1alpha3.ModelConfig) (*ResolvedModelConfig, error) {
	if config == nil {
		return &ResolvedModelConfig{SemanticFailures: []ModelConfigFailure{{Reason: "ModelConfigMissing", Message: "model config is required"}}}, nil
	}
	resolved := &ResolvedModelConfig{Config: config.DeepCopy()}
	addSemanticFailure := func(reason, message string) {
		resolved.SemanticFailures = append(resolved.SemanticFailures, ModelConfigFailure{Reason: reason, Message: message})
	}
	addReferenceFailure := func(reason, message string) {
		resolved.ReferenceFailures = append(resolved.ReferenceFailures, ModelConfigFailure{Reason: reason, Message: message})
	}
	requireSecret := func(name, notFoundReason, keyNotFoundReason string, keys ...string) *corev1.Secret {
		key := types.NamespacedName{Namespace: config.Namespace, Name: name}
		fetched := krt.FetchOne(ctx, collections.Secrets, krt.FilterObjectName(key))
		if fetched == nil {
			addReferenceFailure(notFoundReason, fmt.Sprintf("secret %s not found", name))
			return nil
		}
		secret := *fetched
		for _, secretKey := range keys {
			if _, ok := secret.Data[secretKey]; !ok {
				addReferenceFailure(keyNotFoundReason, fmt.Sprintf("secret %s does not contain key %q", name, secretKey))
			}
		}
		for _, secretKey := range keys {
			resolved.References = append(resolved.References, ModelConfigReference{NamespacedName: key, Kind: "Secret", Key: secretKey})
		}
		if len(keys) == 0 {
			resolved.References = append(resolved.References, ModelConfigReference{NamespacedName: key, Kind: "Secret"})
		}
		return secret
	}
	requireAPIKey := func() {
		if config.Spec.APIKeySecret == "" {
			return
		}
		requireSecret(config.Spec.APIKeySecret, "APIKeySecretNotFound", "APIKeySecretKeyNotFound", config.Spec.APIKeySecretKey)
	}
	if tls := config.Spec.TLS; tls != nil && tls.CACertSecretRef != "" {
		requireSecret(tls.CACertSecretRef, "TLSSecretNotFound", "TLSSecretKeyNotFound", tls.CACertSecretKey)
	}

	switch config.Spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		usingTokenExchange := config.Spec.OpenAI != nil && config.Spec.OpenAI.TokenExchange != nil
		if !config.Spec.APIKeyPassthrough && (usingTokenExchange || config.Spec.APIKeySecret != "") {
			requireAPIKey()
		}
	case v1alpha3.ModelProviderAnthropic, v1alpha3.ModelProviderAzureOpenAI, v1alpha3.ModelProviderFoundry:
		if !config.Spec.APIKeyPassthrough && config.Spec.APIKeySecret != "" {
			requireAPIKey()
		}
	case v1alpha3.ModelProviderGemini:
		requireAPIKey()
	case v1alpha3.ModelProviderGeminiVertexAI, v1alpha3.ModelProviderAnthropicVertexAI:
		if config.Spec.APIKeySecret != "" {
			requireAPIKey()
		}
	}

	switch config.Spec.Provider {
	case v1alpha3.ModelProviderAzureOpenAI:
		if config.Spec.AzureOpenAI == nil {
			addSemanticFailure("InvalidProviderConfig", "AzureOpenAI model config is required")
		}
	case v1alpha3.ModelProviderGeminiVertexAI:
		if config.Spec.GeminiVertexAI == nil {
			addSemanticFailure("InvalidProviderConfig", "GeminiVertexAI model config is required")
		}
	case v1alpha3.ModelProviderAnthropicVertexAI:
		if config.Spec.AnthropicVertexAI == nil {
			addSemanticFailure("InvalidProviderConfig", "AnthropicVertexAI model config is required")
		}
	case v1alpha3.ModelProviderOllama:
		if config.Spec.Ollama == nil {
			addSemanticFailure("InvalidProviderConfig", "ollama model config is required")
		}
	case v1alpha3.ModelProviderBedrock:
		if config.Spec.Bedrock == nil {
			addSemanticFailure("InvalidProviderConfig", "bedrock model config is required")
			break
		}
		if !config.Spec.APIKeyPassthrough && config.Spec.APIKeySecret != "" {
			key := types.NamespacedName{Namespace: config.Namespace, Name: config.Spec.APIKeySecret}
			fetched := krt.FetchOne(ctx, collections.Secrets, krt.FilterObjectName(key))
			if fetched == nil {
				addReferenceFailure("APIKeySecretNotFound", fmt.Sprintf("secret %s not found", config.Spec.APIKeySecret))
				break
			}
			secret := *fetched
			if _, ok := secret.Data[env.AWSBearerTokenBedrock.Name()]; ok {
				resolved.BedrockAuthentication = BedrockAuthenticationBearer
				resolved.References = append(resolved.References, ModelConfigReference{NamespacedName: key, Kind: "Secret", Key: env.AWSBearerTokenBedrock.Name()})
			} else {
				resolved.BedrockAuthentication = BedrockAuthenticationIAM
				requireSecret(config.Spec.APIKeySecret, "APIKeySecretNotFound", "BedrockCredentialKeyNotFound", env.AWSAccessKeyID.Name(), env.AWSSecretAccessKey.Name())
				_, resolved.BedrockSessionToken = secret.Data[env.AWSSessionToken.Name()]
				if resolved.BedrockSessionToken {
					resolved.References = append(resolved.References, ModelConfigReference{NamespacedName: key, Kind: "Secret", Key: env.AWSSessionToken.Name()})
				}
			}
		}
	case v1alpha3.ModelProviderSAPAICore:
		if config.Spec.SAPAICore == nil {
			addSemanticFailure("InvalidProviderConfig", "sapAICore model config is required")
		}
		if !config.Spec.APIKeyPassthrough && config.Spec.APIKeySecret != "" {
			requireSecret(config.Spec.APIKeySecret, "APIKeySecretNotFound", "SAPAICoreCredentialKeyNotFound", "client_id", "client_secret")
		}
	case v1alpha3.ModelProviderFoundry:
		if config.Spec.Foundry == nil {
			addSemanticFailure("InvalidProviderConfig", "foundry model config is required")
			break
		}
		resolved.FoundryEndpoint = config.Spec.Foundry.Endpoint
		if resolved.FoundryEndpoint == "" && config.Spec.Foundry.EndpointFrom != nil {
			ref := config.Spec.Foundry.EndpointFrom
			key := types.NamespacedName{Namespace: config.Namespace, Name: ref.Name}
			resolved.References = append(resolved.References, ModelConfigReference{NamespacedName: key, Kind: "ConfigMap", Key: ref.Key})
			fetched := krt.FetchOne(ctx, collections.ConfigMaps, krt.FilterObjectName(key))
			if fetched == nil {
				addReferenceFailure("EndpointConfigMapNotFound", fmt.Sprintf("config map %s not found", ref.Name))
			} else {
				configMap := *fetched
				value, ok := configMap.Data[ref.Key]
				if !ok {
					addReferenceFailure("EndpointConfigMapKeyNotFound", fmt.Sprintf("config map %s does not contain key %q", ref.Name, ref.Key))
				} else {
					resolved.FoundryEndpoint = value
				}
			}
		}
		if resolved.FoundryEndpoint == "" {
			if config.Spec.Foundry.EndpointFrom == nil {
				addSemanticFailure("InvalidProviderConfig", "foundry endpoint could not be resolved: set foundry.endpoint or a foundry.endpointFrom whose ConfigMap key exists")
			}
		}
	case v1alpha3.ModelProviderOpenAI, v1alpha3.ModelProviderAnthropic, v1alpha3.ModelProviderGemini:
	default:
		addSemanticFailure("UnsupportedProvider", fmt.Sprintf("unsupported model provider: %s", config.Spec.Provider))
	}
	return resolved, nil
}
