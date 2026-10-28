package translator

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// CredentialPlaceholder satisfies SDKs that require a configured API key.
// The gateway overwrites the corresponding header with the configured credential.
const CredentialPlaceholder = "kagent-credential-injected"

const (
	mcpActorJWTHeader = "Authorization"
	mcpActorJWTPrefix = "Bearer "
)

// MCPActorJWTHeaders returns a copy with the placeholder for the MCP gateway
// credential, leaving the caller's headers unchanged.
func MCPActorJWTHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers)+1)
	maps.Copy(result, headers)
	result[mcpActorJWTHeader] = mcpActorJWTPrefix + CredentialPlaceholder
	return result
}

// passthroughClient sends the caller's token; an empty header means no target.
type passthroughClient struct {
	client, endpoint, header, prefix string
	// explicit lists the headers the client sets itself.
	explicit []string
}

// passthroughTargets records each client's token header. The origin must not
// serve clients that did not opt in.
func passthroughTargets(clients []passthroughClient, directEndpoints []string) ([]egresspolicy.GatewayTarget, error) {
	direct := map[string]bool{}
	if len(clients) != 0 {
		for _, endpoint := range directEndpoints {
			// IP literals fail to parse and cannot match a DNS-only target.
			if origin, err := canonicalOrigin(endpoint); err == nil {
				direct[origin.String()] = true
			}
		}
	}
	var targets []egresspolicy.GatewayTarget
	explicit := map[string]bool{}
	for _, client := range clients {
		if client.header == "" {
			return nil, NewValidationError("%s tokenPassthrough has no credential target", client.client)
		}
		origin, err := canonicalOrigin(client.endpoint)
		if err != nil {
			return nil, err
		}
		if direct[origin.String()] {
			return nil, NewValidationError("destination %q mixes tokenPassthrough with clients that do not use it", origin)
		}
		for _, header := range client.explicit {
			explicit[origin.String()+"\x00"+strings.ToLower(header)] = true
		}
		targets = append(targets, egresspolicy.GatewayTarget{Origin: origin.String(), Header: client.header, Prefix: client.prefix})
	}
	for _, target := range targets {
		if explicit[target.Origin+"\x00"+target.Header] {
			return nil, NewValidationError("destination %q has an explicit %q header that tokenPassthrough replaces", target.Origin, target.Header)
		}
	}
	targets, err := egress.CanonicalGatewayTargets(targets)
	if err != nil {
		return nil, NewValidationError("%v", err)
	}
	return targets, nil
}

func canonicalOrigin(endpoint string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, NewValidationError("invalid passthrough credential destination")
	}
	origin, err := egress.ParseOrigin(egress.Origin(u))
	if err != nil {
		return nil, NewValidationError("invalid passthrough credential destination: %v", err)
	}
	return origin, nil
}

// CompileCredentials replaces secret-backed environment values with inert SDK
// placeholders and compiles their destination-scoped gateway bindings. Models
// outside the agent tree (such as memory embeddings) are supplied separately.
// Compilation accepts resolved objects directly, so enforce credential
// exclusivity here as well as at Kubernetes admission.
func CompileCredentials(input *HarnessInput, extraModels []*ResolvedModelConfig, environment []corev1.EnvVar) ([]corev1.EnvVar, []egress.Credential, []egresspolicy.GatewayTarget, error) {
	var bindings []egress.Credential
	boundModels := map[string]bool{}
	boundMCP := map[string]bool{}
	var passthrough []passthroughClient
	var directEndpoints []string
	bind := func(rawURL, header, prefix, namespace, name, key string) error {
		u, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return NewValidationError("credential destination must be an absolute HTTP(S) URL without user information or fragment")
		}
		uri := "ate-secret://k8s.io/default/" + namespace + "/" + name + "/" + key
		bindings = append(bindings, egress.Credential{Origin: egress.Origin(u), Header: header, Prefix: prefix, URI: uri})
		return nil
	}
	models := append([]*ResolvedModelConfig(nil), extraModels...)
	var visit func(*AgentInput) error
	visit = func(agent *AgentInput) error {
		if len(extraModels) == 0 && agent.ResolvedModelConfig != nil {
			models = append(models, agent.ResolvedModelConfig)
		}
		for _, tool := range agent.MCPTools {
			if token := tool.Server.Spec.ActorJWT; token != nil {
				if tool.Server.Spec.TokenPassthrough != nil {
					return NewValidationError("actorJwt and tokenPassthrough are mutually exclusive")
				}
				for _, ref := range tool.Server.Spec.HeadersFrom {
					if strings.EqualFold(ref.Name, mcpActorJWTHeader) {
						return NewValidationError("headersFrom cannot set Authorization when actorJwt is enabled")
					}
				}
				origin, err := canonicalOrigin(tool.Server.Spec.URL)
				if err != nil {
					return err
				}
				bindings = append(bindings, egress.Credential{Origin: origin.String(), Header: strings.ToLower(mcpActorJWTHeader), Prefix: mcpActorJWTPrefix, ActorJWT: actorJWTSource(token)})
			}
			if ptr.Deref(tool.Server.Spec.TokenPassthrough, false) {
				var explicit []string
				for _, ref := range tool.Server.Spec.HeadersFrom {
					explicit = append(explicit, ref.Name)
				}
				passthrough = append(passthrough, passthroughClient{
					client: fmt.Sprintf("%s %q", tool.Binding.Server.Kind, tool.Server.Name), endpoint: tool.Server.Spec.URL,
					header: "authorization", prefix: "Bearer ", explicit: explicit,
				})
			} else {
				directEndpoints = append(directEndpoints, tool.Server.Spec.URL)
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
		for _, child := range agent.Shared {
			if err := visit(child.Agent); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(input.Root); err != nil {
		return nil, nil, nil, err
	}
	for _, resolved := range models {
		model := resolved.Config
		if model.Spec.ActorJWT != nil {
			if _, err := ActorJWTEnvironment(resolved); err != nil {
				return nil, nil, nil, err
			}
			_, endpoint, header, prefix := modelCredentialTarget(resolved)
			origin, err := canonicalOrigin(endpoint)
			if err != nil {
				return nil, nil, nil, err
			}
			bindings = append(bindings, egress.Credential{Origin: origin.String(), Header: header, Prefix: prefix, ActorJWT: actorJWTSource(model.Spec.ActorJWT)})
			continue
		}
		if ptr.Deref(model.Spec.TokenPassthrough, false) || model.Spec.APIKeySecret == "" {
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
			return nil, nil, nil, err
		}
		boundModels[name+"\x00"+model.Spec.APIKeySecret+"\x00"+key] = true
	}
	for _, resolved := range models {
		spec := resolved.Config.Spec
		if !ptr.Deref(spec.TokenPassthrough, false) {
			directEndpoints = append(directEndpoints, modelEndpoints(resolved, environment)...)
			continue
		}
		_, endpoint, header, prefix := modelCredentialTarget(resolved)
		passthrough = append(passthrough, passthroughClient{
			client: fmt.Sprintf("ModelConfig %q", resolved.Config.Name), endpoint: endpoint, header: header, prefix: prefix,
			explicit: slices.Collect(maps.Keys(spec.DefaultHeaders)),
		})
	}
	targets, err := passthroughTargets(passthrough, directEndpoints)
	if err != nil {
		return nil, nil, nil, err
	}
	var destinations []string
	for _, binding := range bindings {
		destinations = append(destinations, binding.Origin)
	}
	access, err := egress.CanonicalAccess(destinations, bindings, targets)
	if err != nil {
		return nil, nil, nil, NewValidationError("%v", err)
	}

	result := append([]corev1.EnvVar(nil), environment...)
	for i, variable := range result {
		if variable.ValueFrom == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		isMCP := strings.HasPrefix(variable.Name, "KAGENT_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CODEX_MCP_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CLAUDE_MCP_CREDENTIAL_")
		if ref == nil || (!boundModels[variable.Name+"\x00"+ref.Name+"\x00"+ref.Key] && (!isMCP || !boundMCP[ref.Name+"\x00"+ref.Key])) {
			return nil, nil, nil, NewValidationError("environment credential %q cannot use gateway header injection; local signing and arbitrary secret environment variables are unsupported", variable.Name)
		}
		result[i].Value, result[i].ValueFrom = CredentialPlaceholder, nil
	}
	return result, access.Credentials, access.TokenPassthroughTargets, nil
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
		hasCredential := spec.APIKeySecret != "" || ptr.Deref(spec.TokenPassthrough, false)
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

// modelEndpoints returns every endpoint a model client calls, keyed or not.
func modelEndpoints(resolved *ResolvedModelConfig, environment []corev1.EnvVar) []string {
	spec := resolved.Config.Spec
	if spec.Provider == v1alpha3.ModelProviderOllama {
		// Go ADK Ollama clients share one host and key across models.
		var host string
		var hasCredential bool
		for _, variable := range environment {
			switch variable.Name {
			case env.OllamaAPIBase.Name():
				host = variable.Value
			case env.OllamaAPIKey.Name():
				hasCredential = variable.Value != "" || variable.ValueFrom != nil
			}
		}
		if host != "" {
			return []string{host}
		}
		if models.OllamaReachesCloud(spec.Model, host, hasCredential) {
			return []string{"https://api.ollama.com"}
		}
		return nil
	}
	var endpoints []string
	if _, endpoint, _, _ := modelCredentialTarget(resolved); endpoint != "" {
		endpoints = append(endpoints, endpoint)
	}
	if spec.Provider == v1alpha3.ModelProviderSAPAICore && spec.SAPAICore != nil {
		endpoints = append(endpoints, spec.SAPAICore.BaseURL)
		if spec.SAPAICore.AuthURL != "" {
			endpoints = append(endpoints, spec.SAPAICore.AuthURL)
		}
	}
	var vertex *v1alpha3.BaseVertexAIConfig
	switch {
	case spec.Provider == v1alpha3.ModelProviderGeminiVertexAI && spec.GeminiVertexAI != nil:
		vertex = &spec.GeminiVertexAI.BaseVertexAIConfig
	case spec.Provider == v1alpha3.ModelProviderAnthropicVertexAI && spec.AnthropicVertexAI != nil:
		vertex = &spec.AnthropicVertexAI.BaseVertexAIConfig
	}
	if vertex != nil {
		endpoints = append(endpoints, "https://"+VertexHostname(vertex.Location), "https://oauth2.googleapis.com")
	}
	return endpoints
}

// VertexHostname returns the Vertex AI API host for a location.
func VertexHostname(location string) string {
	switch location {
	case "global":
		return "aiplatform.googleapis.com"
	case "us", "eu":
		return "aiplatform." + location + ".rep.googleapis.com"
	default:
		return location + "-aiplatform.googleapis.com"
	}
}

// ActorJWTEnvironment supplies the existing SDK credential placeholder for an
// Actor-authenticated model. The token itself is owned by Substrate's gateway.
func ActorJWTEnvironment(resolved *ResolvedModelConfig) (corev1.EnvVar, error) {
	spec := resolved.Config.Spec
	switch spec.Provider {
	case v1alpha3.ModelProviderBedrock:
		return corev1.EnvVar{}, NewValidationError("Bedrock requires AWS credentials and does not support actorJwt")
	case v1alpha3.ModelProviderGemini, v1alpha3.ModelProviderOllama:
		return corev1.EnvVar{}, NewValidationError("%s does not support actorJwt", spec.Provider)
	}
	if spec.TokenPassthrough != nil || spec.APIKeySecret != "" || spec.APIKeySecretKey != "" {
		return corev1.EnvVar{}, NewValidationError("actorJwt, apiKeySecret and tokenPassthrough are mutually exclusive")
	}
	name, endpoint, header, _ := modelCredentialTarget(resolved)
	if name == "" || endpoint == "" || header == "" {
		return corev1.EnvVar{}, NewValidationError("ModelConfig %q actorJwt requires a provider with a credential header and endpoint", resolved.Config.Name)
	}
	for key := range spec.DefaultHeaders {
		if strings.EqualFold(key, header) {
			return corev1.EnvVar{}, NewValidationError("defaultHeaders cannot override the actorJwt credential header %q", header)
		}
	}
	return corev1.EnvVar{Name: name, Value: CredentialPlaceholder}, nil
}

func actorJWTSource(config *v1alpha3.ActorJWT) *egress.ActorJWT {
	expiration := int64(900)
	if config.ExpirationSeconds != nil {
		expiration = *config.ExpirationSeconds
	}
	return &egress.ActorJWT{Audiences: slices.Clone(config.Audiences), ExpirationSeconds: expiration}
}
