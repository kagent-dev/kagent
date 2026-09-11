package translator

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	corev1 "k8s.io/api/core/v1"
)

// CredentialPlaceholder satisfies SDKs that require a configured API key.
// The gateway overwrites the corresponding header with the current Secret.
const CredentialPlaceholder = "kagent-credential-injected"

// CompileCredentials replaces secret-backed environment values with inert SDK
// placeholders and compiles their destination-scoped gateway bindings. Models
// outside the agent tree (such as memory embeddings) are supplied separately.
func CompileCredentials(input *HarnessInput, extraModels []*ResolvedModelConfig, environment []corev1.EnvVar) ([]corev1.EnvVar, []egress.Credential, error) {
	for _, variable := range input.Harness.Spec.Env {
		if variable.CredentialRef != nil {
			return nil, nil, NewValidationError("Harness environment %q: arbitrary credentialRef values cannot be injected into HTTP headers; configure credentials on ModelConfig or RemoteMCPServer", variable.Name)
		}
	}
	var bindings []egress.Credential
	boundModels := map[string]bool{}
	boundMCP := map[string]bool{}
	boundArtifacts := map[string]bool{}
	bind := func(rawURL, header, prefix, namespace, name, key string) error {
		u, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return NewValidationError("credential destination must be an absolute HTTP(S) URL without user information or fragment")
		}
		uri := "ate-secret://kubernetes.io/" + namespace + "/" + name + "/" + key
		bindings = append(bindings, egress.Credential{Hostname: u.Hostname(), Header: header, Prefix: prefix, URI: uri})
		return nil
	}
	models := append([]*ResolvedModelConfig(nil), extraModels...)
	var visit func(*AgentInput) error
	visit = func(agent *AgentInput) error {
		if len(extraModels) == 0 && agent.ResolvedModelConfig != nil {
			models = append(models, agent.ResolvedModelConfig)
		}
		for _, tool := range agent.MCPTools {
			for _, ref := range tool.Server.Spec.HeadersFrom {
				if ref.ValueFrom != nil && ref.ValueFrom.Type == v1alpha3.SecretValueSource {
					boundMCP[ref.ValueFrom.Name+"\x00"+ref.ValueFrom.Key] = true
					if err := bind(tool.Server.Spec.URL, ref.Name, "", tool.Server.Namespace, ref.ValueFrom.Name, ref.ValueFrom.Key); err != nil {
						return err
					}
				}
			}
		}
		if agent.Template != nil {
			// Git speaks Basic authentication, so the Secret key holds the
			// base64 of "<username>:<token>", ready to follow the scheme.
			bindArtifact := func(source v1alpha3.ArtifactSource) error {
				if source.Git == nil || source.Git.CredentialRef == nil {
					return nil
				}
				ref := source.Git.CredentialRef
				boundArtifacts[ref.Name+"\x00"+ref.Key] = true
				return bind(source.Git.URL, "authorization", "Basic ", agent.Template.Namespace, ref.Name, ref.Key)
			}
			for _, skill := range agent.Template.Spec.Skills {
				if err := bindArtifact(skill.Source); err != nil {
					return err
				}
			}
			for _, plugin := range agent.Template.Spec.Plugins {
				if err := bindArtifact(plugin.Source); err != nil {
					return err
				}
			}
		}
		for _, child := range agent.Shared {
			if err := visit(child.Agent); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(input.Root); err != nil {
		return nil, nil, err
	}
	for _, resolved := range models {
		model := resolved.Config
		if model.Spec.APIKeyPassthrough || model.Spec.APIKeySecret == "" {
			continue
		}
		name, endpoint, header, prefix := modelCredentialTarget(resolved)
		if name == "" {
			continue
		}
		if model.Spec.OpenAI != nil && model.Spec.OpenAI.TokenExchange != nil {
			continue
		}
		key := model.Spec.APIKeySecretKey
		if model.Spec.Provider == v1alpha3.ModelProviderBedrock {
			key = env.AWSBearerTokenBedrock.Name()
			bearer := false
			for _, variable := range environment {
				if variable.Name == name && variable.ValueFrom != nil && variable.ValueFrom.SecretKeyRef != nil && variable.ValueFrom.SecretKeyRef.Name == model.Spec.APIKeySecret && variable.ValueFrom.SecretKeyRef.Key == key {
					bearer = true
				}
			}
			if !bearer {
				continue
			}
		}
		if err := bind(endpoint, header, prefix, model.Namespace, model.Spec.APIKeySecret, key); err != nil {
			return nil, nil, err
		}
		boundModels[name+"\x00"+model.Spec.APIKeySecret+"\x00"+key] = true
	}
	bindings, err := egress.CanonicalCredentials(bindings)
	if err != nil {
		return nil, nil, NewValidationError("%v", err)
	}
	for _, resolved := range models {
		if !resolved.Config.Spec.APIKeyPassthrough {
			continue
		}
		_, endpoint, _, _ := modelCredentialTarget(resolved)
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, nil, NewValidationError("invalid passthrough credential destination")
		}
		hostname := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		for _, binding := range bindings {
			if binding.Hostname == hostname {
				return nil, nil, NewValidationError("destination %q cannot combine caller-token passthrough with gateway credentials", hostname)
			}
		}
	}
	result := append([]corev1.EnvVar(nil), environment...)
	for i, variable := range result {
		if variable.ValueFrom == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		isMCP := strings.HasPrefix(variable.Name, "KAGENT_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CODEX_MCP_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CLAUDE_MCP_CREDENTIAL_")
		isArtifact := strings.HasPrefix(variable.Name, ArtifactCredentialEnvPrefix)
		if ref == nil || (!boundModels[variable.Name+"\x00"+ref.Name+"\x00"+ref.Key] && (!isMCP || !boundMCP[ref.Name+"\x00"+ref.Key]) && (!isArtifact || !boundArtifacts[ref.Name+"\x00"+ref.Key])) {
			return nil, nil, NewValidationError("environment credential %q cannot use gateway header injection; local signing and arbitrary secret environment variables are unsupported", variable.Name)
		}
		result[i].Value, result[i].ValueFrom = CredentialPlaceholder, nil
	}
	return result, bindings, nil
}

func modelCredentialTarget(resolved *ResolvedModelConfig) (name, endpoint, header, prefix string) {
	spec := resolved.Config.Spec
	switch spec.Provider {
	case v1alpha3.ModelProviderOpenAI:
		name, endpoint, header, prefix = env.OpenAIAPIKey.Name(), "https://api.openai.com", "authorization", "Bearer "
		if spec.OpenAI != nil && spec.OpenAI.BaseURL != "" {
			endpoint = spec.OpenAI.BaseURL
		}
	case v1alpha3.ModelProviderAnthropic:
		name, endpoint, header = env.AnthropicAPIKey.Name(), "https://api.anthropic.com", "x-api-key"
		if spec.Anthropic != nil && spec.Anthropic.BaseURL != "" {
			endpoint = spec.Anthropic.BaseURL
		}
	case v1alpha3.ModelProviderAzureOpenAI:
		name, header = env.AzureOpenAIAPIKey.Name(), "api-key"
		if spec.AzureOpenAI != nil {
			endpoint = spec.AzureOpenAI.Endpoint
		}
	case v1alpha3.ModelProviderGemini:
		name, endpoint, header = env.GoogleAPIKey.Name(), "https://generativelanguage.googleapis.com", "x-goog-api-key"
	case v1alpha3.ModelProviderBedrock:
		name, header, prefix = env.AWSBearerTokenBedrock.Name(), "authorization", "Bearer "
		if spec.Bedrock != nil {
			endpoint = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", spec.Bedrock.Region)
		}
	case v1alpha3.ModelProviderFoundry:
		name, endpoint, header = env.FoundryAPIKey.Name(), resolved.FoundryEndpoint, "api-key"
		if spec.Foundry != nil && spec.Foundry.APIFormat == v1alpha3.FoundryAPIFormatAnthropic {
			header = "x-api-key"
		}
	}
	return name, endpoint, header, prefix
}
