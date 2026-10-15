package translator

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egressext"
)

func credentialOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("invalid credential destination")
	}
	origin, err := egress.ParseOrigin(egress.Origin(u))
	if err != nil {
		return "", err
	}
	return origin.String(), nil
}

func policyModels(input *HarnessInput) []*ResolvedModelConfig {
	var models []*ResolvedModelConfig
	var visit func(*AgentInput)
	visit = func(agent *AgentInput) {
		if agent.ResolvedModelConfig != nil {
			models = append(models, agent.ResolvedModelConfig)
		}
		for _, child := range agent.Shared {
			visit(child.Agent)
		}
	}
	visit(input.Root)
	if input.SummarizerModel != nil {
		models = append(models, input.SummarizerModel)
	}
	if input.MemoryModel != nil {
		models = append(models, input.MemoryModel)
	}
	return models
}

// ResolvePolicy resolves authentication before any harness renders client configuration.
func ResolvePolicy(input *HarnessInput, compiler HarnessCompiler, extension egressext.PolicyTranslator) (ResolvedPolicy, error) {
	models := policyModels(input)
	requirements := make(map[*ResolvedModelConfig]ModelRequirements, len(models))
	if compiler != nil {
		for _, model := range models {
			requirements[model] = compiler.ModelRequirements(model)
		}
	}
	draft, err := policyDraft(input, models, requirements)
	if err != nil {
		return ResolvedPolicy{}, err
	}
	original := draft
	if extension != nil {
		draft, err = extension.Translate(types.NamespacedName{Namespace: input.Harness.Namespace, Name: input.AgentName}, clonePolicyDraft(draft))
		if err != nil {
			return ResolvedPolicy{}, fmt.Errorf("failed to translate egress policy: %w", err)
		}
	}
	access, err := validatePolicyDraft(original, draft)
	if err != nil {
		return ResolvedPolicy{}, err
	}
	result := ResolvedPolicy{access: access, modelBindings: map[*ResolvedModelConfig]bool{}, secretModels: map[string]bool{}, secretMCP: map[string]bool{}, headers: map[string][]string{}}
	var visit func(*AgentInput)
	visit = func(agent *AgentInput) {
		for _, tool := range agent.MCPTools {
			for _, ref := range tool.Server.Spec.HeadersFrom {
				if ref.ValueFrom != nil && ref.ValueFrom.Type == v1alpha3.SecretValueSource {
					result.secretMCP[ref.ValueFrom.Name+"\x00"+ref.ValueFrom.Key] = true
				}
			}
		}
		for _, child := range agent.Shared {
			visit(child.Agent)
		}
	}
	visit(input.Root)
	for _, binding := range access.Credentials {
		if !slices.Contains(original.Credentials, egressext.Credential(binding)) {
			result.headers[binding.Origin] = append(result.headers[binding.Origin], binding.Header)
		}
	}
	for _, resolved := range models {
		model := resolved.Config
		target := modelCredentialTarget(resolved)
		name, endpoint, header, prefix := target.Environment, target.Endpoint, target.Header, target.Prefix
		if name == "" {
			continue
		}
		origin, err := credentialOrigin(endpoint)
		if err != nil {
			return ResolvedPolicy{}, NewValidationError("invalid credential destination: %v", err)
		}
		matches := func(binding egress.Credential) bool {
			return binding.Origin == origin && binding.Header == header && binding.Prefix == prefix
		}
		bound := slices.ContainsFunc(access.Credentials, matches)
		if model.Spec.APIKeyPassthrough {
			if slices.ContainsFunc(access.Credentials, func(binding egress.Credential) bool { return binding.Origin == origin }) {
				return ResolvedPolicy{}, NewValidationError("destination %q cannot combine caller-token passthrough with gateway credentials", origin)
			}
			result.tokenPassthroughOrigins = append(result.tokenPassthroughOrigins, origin)
			continue
		}
		if model.Spec.APIKeySecret != "" {
			if bound {
				result.modelBindings[resolved] = true
				result.secretModels[name+"\x00"+model.Spec.APIKeySecret+"\x00"+modelSecretKey(resolved)] = true
			}
			continue
		}
		// Optional SDK authentication keeps its default mode unless a binding is added.
		selected := slices.Contains(result.headers[origin], header)
		if requirements[resolved].CredentialRequired {
			if !bound {
				return ResolvedPolicy{}, NewValidationError("model %q requires apiKeySecret or a resolved gateway credential for %s", model.Name, origin)
			}
			selected = true
		}
		if selected && bound {
			result.modelBindings[resolved] = true
			result.modelKeys = append(result.modelKeys, name)
		}
	}
	return result, nil
}

func policyDraft(input *HarnessInput, models []*ResolvedModelConfig, requirementsByModel map[*ResolvedModelConfig]ModelRequirements) (egressext.PolicyDraft, error) {
	var destinations []string
	var bindings []egress.Credential
	var requirements []egressext.AuthenticationRequirement
	add := func(raw, header, prefix string, preserve, required bool) error {
		origin, err := credentialOrigin(raw)
		if err != nil {
			return err
		}
		destinations = append(destinations, origin)
		if strings.HasPrefix(origin, "https://") && !strings.Contains(origin, "*") {
			requirements = append(requirements, egressext.AuthenticationRequirement{Origin: origin, Header: header, Prefix: prefix, Preserve: preserve, Required: required})
		}
		return nil
	}
	for _, model := range models {
		for _, endpoint := range modelEndpoints(model) {
			origin, err := credentialOrigin(endpoint)
			if err != nil {
				return egressext.PolicyDraft{}, NewValidationError("invalid model destination: %v", err)
			}
			destinations = append(destinations, origin)
		}
		destinations = append(destinations, requirementsByModel[model].Destinations...)
		target := modelCredentialTarget(model)
		name, endpoint, header, prefix := target.Environment, target.Endpoint, target.Header, target.Prefix
		if name == "" {
			continue
		}
		spec := model.Config.Spec
		literal := slices.ContainsFunc(input.Harness.Spec.Env, func(variable v1alpha3.RuntimeEnvVar) bool { return variable.Name == name })
		preserve := literal || spec.APIKeyPassthrough || len(spec.DefaultHeaders) != 0 || (spec.Provider == v1alpha3.ModelProviderGemini && spec.APIKeySecret == "")
		if err := add(endpoint, header, prefix, preserve, requirementsByModel[model].CredentialRequired && spec.APIKeySecret == "" && !preserve); err != nil {
			return egressext.PolicyDraft{}, NewValidationError("invalid model credential destination: %v", err)
		}
		if spec.APIKeySecret == "" || spec.APIKeyPassthrough {
			continue
		}
		key := modelSecretKey(model)
		if spec.Provider == v1alpha3.ModelProviderBedrock && key == "" {
			continue
		}
		origin, _ := credentialOrigin(endpoint) // Validated above.
		bindings = append(bindings, egress.Credential{Origin: origin, Header: header, Prefix: prefix, URI: "ate-secret://k8s.io/default/" + model.Config.Namespace + "/" + spec.APIKeySecret + "/" + key})
	}
	var visit func(*AgentInput) error
	visit = func(agent *AgentInput) error {
		for _, tool := range agent.MCPTools {
			preserve := slices.ContainsFunc(tool.Server.Spec.HeadersFrom, func(ref v1alpha3.ValueRef) bool {
				return ref.ValueFrom == nil || ref.ValueFrom.Type != v1alpha3.SecretValueSource
			})
			if err := add(tool.Server.Spec.URL, "", "", preserve, false); err != nil {
				return NewValidationError("invalid MCP credential destination: %v", err)
			}
			origin, _ := credentialOrigin(tool.Server.Spec.URL) // Validated above.
			for _, ref := range tool.Server.Spec.HeadersFrom {
				if ref.ValueFrom != nil && ref.ValueFrom.Type == v1alpha3.SecretValueSource {
					bindings = append(bindings, egress.Credential{Origin: origin, Header: ref.Name, URI: "ate-secret://k8s.io/default/" + tool.Server.Namespace + "/" + ref.ValueFrom.Name + "/" + ref.ValueFrom.Key})
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
		return egressext.PolicyDraft{}, err
	}
	access, err := egress.CanonicalAccess(destinations, bindings)
	if err != nil {
		return egressext.PolicyDraft{}, NewValidationError("%v", err)
	}
	// Requirements apply to the whole origin, including overlapping resource references.
	for i := range requirements {
		for _, other := range requirements {
			if other.Origin != requirements[i].Origin {
				continue
			}
			requirements[i].Preserve = requirements[i].Preserve || other.Preserve
			if other.Header == requirements[i].Header && other.Prefix == requirements[i].Prefix {
				requirements[i].Required = requirements[i].Required || other.Required
			}
		}
	}
	slices.SortFunc(requirements, func(a, b egressext.AuthenticationRequirement) int {
		return cmp.Or(strings.Compare(a.Origin, b.Origin), strings.Compare(a.Header, b.Header), strings.Compare(a.Prefix, b.Prefix))
	})
	requirements = slices.Compact(requirements)
	credentials := make([]egressext.Credential, len(access.Credentials))
	for i, binding := range access.Credentials {
		credentials[i] = egressext.Credential(binding)
	}
	return egressext.PolicyDraft{Destinations: access.Destinations, Requirements: requirements, Credentials: credentials}, nil
}

func clonePolicyDraft(draft egressext.PolicyDraft) egressext.PolicyDraft {
	return egressext.PolicyDraft{Destinations: slices.Clone(draft.Destinations), Requirements: slices.Clone(draft.Requirements), Credentials: slices.Clone(draft.Credentials)}
}

func validatePolicyDraft(original, translated egressext.PolicyDraft) (egress.Access, error) {
	if !slices.Equal(original.Destinations, translated.Destinations) || !slices.Equal(original.Requirements, translated.Requirements) {
		return egress.Access{}, NewValidationError("policy extension cannot change destinations or authentication requirements")
	}
	bindings := make([]egress.Credential, len(translated.Credentials))
	for i, binding := range translated.Credentials {
		bindings[i] = egress.Credential(binding)
	}
	access, err := egress.CanonicalAccess(translated.Destinations, bindings)
	if err != nil {
		return egress.Access{}, NewValidationError("%v", err)
	}
	for _, binding := range original.Credentials {
		if !slices.Contains(access.Credentials, egress.Credential(binding)) {
			return egress.Access{}, NewValidationError("policy extension cannot remove or replace existing credentials")
		}
	}
	for _, binding := range access.Credentials {
		if slices.Contains(original.Credentials, egressext.Credential(binding)) {
			continue
		}
		allowed, known, matches := false, false, false
		for _, requirement := range original.Requirements {
			if requirement.Origin != binding.Origin {
				continue
			}
			if requirement.Preserve {
				return egress.Access{}, NewValidationError("credential destination %q preserves explicit authentication", binding.Origin)
			}
			allowed = true
			if requirement.Header != "" {
				known = true
				matches = matches || (requirement.Header == binding.Header && requirement.Prefix == binding.Prefix)
			}
		}
		if !allowed {
			return egress.Access{}, NewValidationError("credential destination %q is not an eligible application target", binding.Origin)
		}
		if known && !matches {
			return egress.Access{}, NewValidationError("credential for %q must match a model header and prefix", binding.Origin)
		}
	}
	for _, requirement := range original.Requirements {
		if requirement.Required && !requirement.Preserve && !slices.ContainsFunc(access.Credentials, func(binding egress.Credential) bool {
			return binding.Origin == requirement.Origin && binding.Header == requirement.Header && binding.Prefix == requirement.Prefix
		}) {
			return egress.Access{}, NewValidationError("model requires a gateway credential for %s header %s; configure apiKeySecret", requirement.Origin, requirement.Header)
		}
	}
	return access, nil
}
