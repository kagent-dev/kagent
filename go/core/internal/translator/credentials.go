package translator

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/adk/pkg/models"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// CredentialPlaceholder satisfies SDKs that require a configured API key.
// The gateway overwrites the corresponding header with the current Secret.
const CredentialPlaceholder = "kagent-credential-injected"

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
	origin, err := egress.ParseOrigin(egresspolicy.Origin(u))
	if err != nil {
		return nil, NewValidationError("invalid passthrough credential destination: %v", err)
	}
	return origin, nil
}

type CompiledCredentials struct {
	Credentials             []egress.Credential
	TokenPassthroughTargets []egresspolicy.GatewayTarget
	IntegrationGrants       []egress.IntegrationGrant
}

// CompileCredentials compiles credential headers into destination-scoped
// gateway bindings, retaining only inert environment values. Models
// outside the agent tree (such as memory embeddings) are supplied separately.
func CompileCredentials(input *HarnessInput, extraModels []*ResolvedModelConfig, environment []corev1.EnvVar) (*CompiledCredentials, error) {
	var bindings []egress.Credential
	var targets []egresspolicy.GatewayTarget
	var destinations []string
	var passthrough []passthroughClient
	var directEndpoints []string
	grants := make(map[string]egress.IntegrationGrant)
	bind := func(kind string, metadata metav1.ObjectMeta, endpoint string, headers []v1alpha3.Header, ordinary []string) error {
		access, err := CompileCredentialHeaders(endpoint, metadata.Namespace, headers)
		if err != nil {
			return err
		}
		if len(access.TokenPassthroughTargets) > 0 {
			grants[kind+"/"+metadata.Name] = egress.IntegrationGrant{Kind: kind, Name: metadata.Name, UID: string(metadata.UID), Targets: access.TokenPassthroughTargets}
		}
		bindings = append(bindings, access.Credentials...)
		destinations = append(destinations, access.Destinations...)
		if len(access.TokenPassthroughTargets) == 0 {
			directEndpoints = append(directEndpoints, endpoint)
		}
		for _, target := range access.TokenPassthroughTargets {
			passthrough = append(passthrough, passthroughClient{client: "integration", endpoint: endpoint, header: target.Header, prefix: target.Prefix, explicit: ordinary})
		}
		return nil
	}
	models := append([]*ResolvedModelConfig(nil), extraModels...)
	var visit func(*AgentInput) error
	visit = func(agent *AgentInput) error {
		if agent.ResolvedModelConfig != nil {
			models = append(models, agent.ResolvedModelConfig)
		}
		for _, tool := range agent.MCPTools {
			if err := bind(egress.IntegrationRemoteMCPServer, tool.Server.ObjectMeta, tool.Server.Spec.URL, tool.Server.Spec.Headers, nil); err != nil {
				return err
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
		return nil, err
	}
	for _, resolved := range models {
		_, endpoint, _, _ := modelCredentialTarget(resolved)
		headers := resolved.Config.Spec.Headers
		if len(headers) > 0 && endpoint == "" {
			return nil, NewValidationError("ModelConfig provider %q has no credential target", resolved.Config.Spec.Provider)
		}
		if err := bind(egress.IntegrationModelConfig, resolved.Config.ObjectMeta, endpoint, headers, slices.Collect(maps.Keys(resolved.Config.Spec.DefaultHeaders))); err != nil {
			return nil, err
		}
		for _, other := range modelEndpoints(resolved, environment) {
			if other != endpoint {
				directEndpoints = append(directEndpoints, other)
			}
		}
	}
	targets, err := passthroughTargets(passthrough, directEndpoints)
	if err != nil {
		return nil, err
	}
	access, err := egress.CanonicalAccess(destinations, bindings, targets)
	if err != nil {
		return nil, NewValidationError("%v", err)
	}
	for _, variable := range environment {
		if variable.ValueFrom != nil {
			return nil, NewValidationError("environment credential %q cannot use gateway header injection; local signing and arbitrary secret environment variables are unsupported", variable.Name)
		}
	}
	result := &CompiledCredentials{Credentials: access.Credentials, TokenPassthroughTargets: access.TokenPassthroughTargets, IntegrationGrants: slices.Collect(maps.Values(grants))}
	slices.SortFunc(result.IntegrationGrants, func(a, b egress.IntegrationGrant) int { return strings.Compare(a.Kind+"/"+a.Name, b.Kind+"/"+b.Name) })
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
		host := ""
		if spec.Ollama != nil {
			host = spec.Ollama.Host
		}
		name, endpoint, header, prefix = env.OllamaAPIKey.Name(), models.OllamaEndpoint(spec.Model, host, len(spec.Headers) > 0), "authorization", "Bearer "
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
