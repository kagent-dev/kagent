package translator

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// CredentialPlaceholder satisfies SDKs that require a configured API key.
// The gateway overwrites the corresponding header with the current Secret.
const CredentialPlaceholder = "kagent-credential-injected"

// CompileCredentials replaces secret-backed environment values with inert SDK
// placeholders and compiles their destination-scoped gateway bindings. Models
// outside the agent tree (such as memory embeddings) are supplied separately.
// The third result contains only contributed bindings that need MCP placeholders.
func CompileCredentials(input *HarnessInput, extraModels []*ResolvedModelConfig, environment []corev1.EnvVar, binder egressext.CredentialBinder) ([]corev1.EnvVar, []egress.Credential, []egress.Credential, []string, error) {
	var bindings []egress.Credential
	var tokenPassthroughOrigins []string
	boundModels := map[string]bool{}
	boundMCP := map[string]bool{}
	bind := func(rawURL, header, prefix, namespace, name, key string) error {
		origin, err := credentialOrigin(rawURL)
		if err != nil {
			return NewValidationError("invalid credential destination: %v", err)
		}
		uri := "ate-secret://k8s.io/default/" + namespace + "/" + name + "/" + key
		bindings = append(bindings, egress.Credential{Origin: origin, Header: header, Prefix: prefix, URI: uri})
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
		for _, child := range agent.Shared {
			if err := visit(child.Agent); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(input.Root); err != nil {
		return nil, nil, nil, nil, err
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
			return nil, nil, nil, nil, err
		}
		boundModels[name+"\x00"+model.Spec.APIKeySecret+"\x00"+key] = true
	}
	bindings, err := egress.CanonicalCredentials(bindings)
	if err != nil {
		return nil, nil, nil, nil, NewValidationError("%v", err)
	}
	additions, err := contributeCredentials(input, models, bindings, binder)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	bindings, err = egress.CanonicalCredentials(append(bindings, additions...))
	if err != nil {
		return nil, nil, nil, nil, NewValidationError("%v", err)
	}
	for _, resolved := range models {
		if !resolved.Config.Spec.APIKeyPassthrough {
			continue
		}
		_, endpoint, _, _ := modelCredentialTarget(resolved)
		origin, err := credentialOrigin(endpoint)
		if err != nil {
			return nil, nil, nil, nil, NewValidationError("invalid passthrough credential destination: %v", err)
		}
		tokenPassthroughOrigins = append(tokenPassthroughOrigins, origin)
	}
	var destinations []string
	for _, binding := range bindings {
		destinations = append(destinations, binding.Origin)
	}
	if _, err := egress.MergeAccess(egress.Access{Destinations: destinations, Credentials: bindings}, egress.Access{}, tokenPassthroughOrigins); err != nil {
		return nil, nil, nil, nil, NewValidationError("%v", err)
	}
	result, err := applyModelCredentialPlaceholders(models, environment, bindings, additions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for i, variable := range result {
		if variable.ValueFrom == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		isMCP := strings.HasPrefix(variable.Name, "KAGENT_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CODEX_MCP_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CLAUDE_MCP_CREDENTIAL_")
		if ref == nil || (!boundModels[variable.Name+"\x00"+ref.Name+"\x00"+ref.Key] && (!isMCP || !boundMCP[ref.Name+"\x00"+ref.Key])) {
			return nil, nil, nil, nil, NewValidationError("environment credential %q cannot use gateway header injection; local signing and arbitrary secret environment variables are unsupported", variable.Name)
		}
		result[i].Value, result[i].ValueFrom = CredentialPlaceholder, nil
	}
	return result, bindings, additions, tokenPassthroughOrigins, nil
}

// Check requirements against the original environment, before Secret references
// or another model introduce placeholders for the same SDK variable.
func applyModelCredentialPlaceholders(models []*ResolvedModelConfig, environment []corev1.EnvVar, bindings, additions []egress.Credential) ([]corev1.EnvVar, error) {
	result := append([]corev1.EnvVar(nil), environment...)
	for _, resolved := range models {
		model := resolved.Config
		if model.Spec.APIKeySecret != "" || model.Spec.APIKeyPassthrough {
			continue
		}
		name, endpoint, header, prefix := modelCredentialTarget(resolved)
		if name == "" {
			continue
		}
		origin, err := credentialOrigin(endpoint)
		if err != nil {
			continue
		}
		matches := func(binding egress.Credential) bool {
			return binding.Origin == origin && binding.Header == header && binding.Prefix == prefix
		}
		bound := slices.ContainsFunc(bindings, matches)
		i := slices.IndexFunc(result, func(variable corev1.EnvVar) bool { return variable.Name == name })
		required := slices.ContainsFunc(environment, func(variable corev1.EnvVar) bool {
			return variable.Name == name && variable.Value == CredentialPlaceholder
		})
		if required && !bound {
			return nil, NewValidationError("model %q requires a gateway credential for %s header %s", model.Name, origin, header)
		}
		contributed := slices.ContainsFunc(additions, matches)
		if contributed && i < 0 {
			result = append(result, corev1.EnvVar{Name: name, Value: CredentialPlaceholder})
		}
	}
	return result, nil
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
	case v1alpha3.ModelProviderMistral:
		name, endpoint, header, prefix = env.MistralAPIKey.Name(), "https://api.mistral.ai", "authorization", "Bearer "
		if spec.Mistral != nil && spec.Mistral.BaseURL != nil && *spec.Mistral.BaseURL != "" {
			endpoint = *spec.Mistral.BaseURL
		}
	case v1alpha3.ModelProviderBedrock:
		name, header, prefix = env.AWSBearerTokenBedrock.Name(), "authorization", "Bearer "
		if spec.Bedrock != nil {
			endpoint = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", spec.Bedrock.Region)
		}
	case v1alpha3.ModelProviderOllama:
		// Ollama Cloud is the only keyed Ollama endpoint, so a binding is
		// declared exactly when the model reaches it. That condition lives in
		// models.OllamaReachesCloud, which the compiler's Secret reference and
		// the egress list also use: a cloud model picked from the catalog, an
		// explicit host, and a missing key were each judged differently in each
		// place, so a valid configuration could compile with an env var that had
		// no binding to match, or with an egress list that omitted the host it
		// was about to call.
		//
		// Without a binding this leaves the name empty, and the env var is
		// treated as a plain secret reference, which is correct — no request
		// leaves for api.ollama.com.
		if spec.Ollama == nil {
			break
		}
		hasCredential := spec.APIKeySecret != "" || spec.APIKeyPassthrough
		if !models.OllamaReachesCloud(spec.Model, spec.Ollama.Host, hasCredential) {
			break
		}
		name, endpoint, header, prefix = env.OllamaAPIKey.Name(), "https://api.ollama.com", "authorization", "Bearer "
	case v1alpha3.ModelProviderFoundry:
		name, endpoint, header = env.FoundryAPIKey.Name(), resolved.FoundryEndpoint, "api-key"
		if spec.Foundry != nil && spec.Foundry.APIFormat == v1alpha3.FoundryAPIFormatAnthropic {
			header = "x-api-key"
		}
	}
	return name, endpoint, header, prefix
}
