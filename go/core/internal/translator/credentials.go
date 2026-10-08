package translator

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
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
	var bindings []egress.Credential
	boundModels := map[string]bool{}
	boundMCP := map[string]bool{}
	boundArtifacts := map[string]bool{}
	var callerOwnedMCP []*v1alpha3.RemoteMCPServer
	bind := func(rawURL, header, prefix, namespace, name, key string) error {
		u, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return NewValidationError("credential destination must be an absolute HTTP(S) URL without user information or fragment")
		}
		uri := "ate-secret://k8s.io/default/" + namespace + "/" + name + "/" + key
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
			if !slices.ContainsFunc(tool.Server.Spec.HeadersFrom, func(ref v1alpha3.ValueRef) bool {
				return strings.EqualFold(ref.Name, "authorization")
			}) {
				callerOwnedMCP = append(callerOwnedMCP, tool.Server)
			}
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
	// With KAGENT_PROPAGATE_TOKEN a server that configures no Authorization
	// receives the caller's token. The gateway replaces a header the request
	// carries, so an Authorization binding on the same host would overwrite it.
	if propagatesCallerToken(environment) {
		for _, server := range callerOwnedMCP {
			u, err := url.Parse(strings.TrimSpace(server.Spec.URL))
			if err != nil {
				return nil, nil, NewValidationError("invalid MCP server URL for %q", server.Name)
			}
			hostname := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
			for _, binding := range bindings {
				if binding.Hostname == hostname && binding.Header == "authorization" {
					return nil, nil, NewValidationError("MCP server %q: destination %q cannot combine caller-token propagation with a gateway Authorization credential", server.Name, hostname)
				}
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

func propagatesCallerToken(environment []corev1.EnvVar) bool {
	propagates := false
	for _, variable := range environment {
		if variable.Name == env.KagentPropagateToken.Name() {
			propagates = strings.EqualFold(strings.TrimSpace(variable.Value), "true")
		}
	}
	return propagates
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
