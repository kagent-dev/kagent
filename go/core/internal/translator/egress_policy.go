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

func secretURI(namespace, name, key string) string {
	return "ate-secret://k8s.io/default/" + namespace + "/" + name + "/" + key
}

func isSecretHeader(ref v1alpha3.ValueRef) bool {
	return ref.ValueFrom != nil && ref.ValueFrom.Type == v1alpha3.SecretValueSource
}

func bindsTarget(origin, header, prefix string) func(egress.Credential) bool {
	return func(binding egress.Credential) bool {
		return binding.Origin == origin && binding.Header == header && binding.Prefix == prefix
	}
}

func publicCredentials(bindings []egress.Credential) []egressext.Credential {
	result := make([]egressext.Credential, len(bindings))
	for i, binding := range bindings {
		result[i] = egressext.Credential(binding)
	}
	return result
}

func internalCredentials(bindings []egressext.Credential) []egress.Credential {
	result := make([]egress.Credential, len(bindings))
	for i, binding := range bindings {
		result[i] = egress.Credential(binding)
	}
	return result
}

// policyAgents returns the root and its shared agents in depth-first order.
func policyAgents(root *AgentInput) []*AgentInput {
	agents := []*AgentInput{root}
	for _, child := range root.Shared {
		agents = append(agents, policyAgents(child.Agent)...)
	}
	return agents
}

func policyModels(input *HarnessInput) []*ResolvedModelConfig {
	var models []*ResolvedModelConfig
	for _, agent := range policyAgents(input.Root) {
		if agent.ResolvedModelConfig != nil {
			models = append(models, agent.ResolvedModelConfig)
		}
	}
	for _, model := range []*ResolvedModelConfig{input.SummarizerModel, input.MemoryModel} {
		if model != nil {
			models = append(models, model)
		}
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
	result := ResolvedPolicy{
		access: access, modelBindings: map[*ResolvedModelConfig]bool{}, secretModels: map[string]bool{},
		secretMCP: secretMCPRefs(input.Root), headers: extensionHeaders(original, access),
	}
	for _, model := range models {
		if err := result.resolveModel(model, requirements[model].CredentialRequired); err != nil {
			return ResolvedPolicy{}, err
		}
	}
	return result, nil
}

// secretMCPRefs lists the Secret keys that MCP headers may reference.
func secretMCPRefs(root *AgentInput) map[string]bool {
	refs := map[string]bool{}
	for _, agent := range policyAgents(root) {
		for _, tool := range agent.MCPTools {
			for _, ref := range tool.Server.Spec.HeadersFrom {
				if isSecretHeader(ref) {
					refs[ref.ValueFrom.Name+"\x00"+ref.ValueFrom.Key] = true
				}
			}
		}
	}
	return refs
}

// extensionHeaders lists, by origin, the headers of bindings the extension added.
func extensionHeaders(original egressext.PolicyDraft, access egress.Access) map[string][]string {
	headers := map[string][]string{}
	for _, binding := range access.Credentials {
		if !slices.Contains(original.Credentials, egressext.Credential(binding)) {
			headers[binding.Origin] = append(headers[binding.Origin], binding.Header)
		}
	}
	return headers
}

// resolveModel records whether a model client authenticates through the gateway.
func (p *ResolvedPolicy) resolveModel(resolved *ResolvedModelConfig, required bool) error {
	spec := resolved.Config.Spec
	target := modelCredentialTarget(resolved)
	if target.Environment == "" {
		return nil
	}
	origin, err := credentialOrigin(target.Endpoint)
	if err != nil {
		return NewValidationError("invalid credential destination: %v", err)
	}
	bound := slices.ContainsFunc(p.access.Credentials, bindsTarget(origin, target.Header, target.Prefix))
	switch {
	case spec.APIKeyPassthrough:
		if slices.ContainsFunc(p.access.Credentials, func(binding egress.Credential) bool { return binding.Origin == origin }) {
			return NewValidationError("destination %q cannot combine caller-token passthrough with gateway credentials", origin)
		}
		p.tokenPassthroughOrigins = append(p.tokenPassthroughOrigins, origin)
	case spec.APIKeySecret != "":
		if bound {
			p.modelBindings[resolved] = true
			p.secretModels[target.Environment+"\x00"+spec.APIKeySecret+"\x00"+modelSecretKey(resolved)] = true
		}
	case required && !bound:
		return NewValidationError("model %q requires apiKeySecret or a resolved gateway credential for %s", resolved.Config.Name, origin)
	// Optional SDK authentication keeps its default mode unless the extension adds a binding.
	case bound && (required || slices.Contains(p.headers[origin], target.Header)):
		p.modelBindings[resolved] = true
		p.modelKeys = append(p.modelKeys, target.Environment)
	}
	return nil
}

// draftBuilder collects application destinations, bindings and authentication requirements.
type draftBuilder struct {
	destinations []string
	bindings     []egress.Credential
	requirements []egressext.AuthenticationRequirement
}

func (b *draftBuilder) allow(raw string) (string, error) {
	origin, err := credentialOrigin(raw)
	if err != nil {
		return "", err
	}
	b.destinations = append(b.destinations, origin)
	return origin, nil
}

// allowTarget allows an origin that can carry credentials. Only exact HTTPS origins get a requirement.
func (b *draftBuilder) allowTarget(raw string, requirement egressext.AuthenticationRequirement) (string, error) {
	origin, err := b.allow(raw)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(origin, "https://") && !strings.Contains(origin, "*") {
		requirement.Origin = origin
		b.requirements = append(b.requirements, requirement)
	}
	return origin, nil
}

func (b *draftBuilder) addModel(model *ResolvedModelConfig, harnessEnv []v1alpha3.RuntimeEnvVar, requirements ModelRequirements) error {
	for _, endpoint := range modelEndpoints(model) {
		if _, err := b.allow(endpoint); err != nil {
			return NewValidationError("invalid model destination: %v", err)
		}
	}
	b.destinations = append(b.destinations, requirements.Destinations...)
	target := modelCredentialTarget(model)
	if target.Environment == "" {
		return nil
	}
	spec := model.Config.Spec
	// A literal Harness value overrides the model Secret in the rendered environment.
	literal := slices.ContainsFunc(harnessEnv, func(variable v1alpha3.RuntimeEnvVar) bool { return variable.Name == target.Environment })
	preserve := literal || modelPreservesAuth(spec)
	origin, err := b.allowTarget(target.Endpoint, egressext.AuthenticationRequirement{
		Header: target.Header, Prefix: target.Prefix, Preserve: preserve,
		Required: requirements.CredentialRequired && spec.APIKeySecret == "" && !preserve,
	})
	if err != nil {
		return NewValidationError("invalid model credential destination: %v", err)
	}
	if spec.APIKeySecret == "" || spec.APIKeyPassthrough || literal {
		return nil
	}
	key := modelSecretKey(model)
	if spec.Provider == v1alpha3.ModelProviderBedrock && key == "" {
		return nil
	}
	b.bindings = append(b.bindings, egress.Credential{Origin: origin, Header: target.Header, Prefix: target.Prefix, URI: secretURI(model.Config.Namespace, spec.APIKeySecret, key)})
	return nil
}

// modelPreservesAuth reports explicit model authentication that new bindings must not override.
// Keyless Gemini is included until its runtime can use a gateway binding.
func modelPreservesAuth(spec v1alpha3.ModelConfigSpec) bool {
	keylessGemini := spec.Provider == v1alpha3.ModelProviderGemini && spec.APIKeySecret == ""
	return spec.APIKeyPassthrough || len(spec.DefaultHeaders) != 0 || keylessGemini
}

func (b *draftBuilder) addMCPServer(server *v1alpha3.RemoteMCPServer) error {
	// Literal and ConfigMap headers can carry authentication.
	preserve := slices.ContainsFunc(server.Spec.HeadersFrom, func(ref v1alpha3.ValueRef) bool { return !isSecretHeader(ref) })
	origin, err := b.allowTarget(server.Spec.URL, egressext.AuthenticationRequirement{Preserve: preserve})
	if err != nil {
		return NewValidationError("invalid MCP credential destination: %v", err)
	}
	for _, ref := range server.Spec.HeadersFrom {
		if isSecretHeader(ref) {
			b.bindings = append(b.bindings, egress.Credential{Origin: origin, Header: ref.Name, URI: secretURI(server.Namespace, ref.ValueFrom.Name, ref.ValueFrom.Key)})
		}
	}
	return nil
}

func policyDraft(input *HarnessInput, models []*ResolvedModelConfig, requirementsByModel map[*ResolvedModelConfig]ModelRequirements) (egressext.PolicyDraft, error) {
	var b draftBuilder
	for _, model := range models {
		if err := b.addModel(model, input.Harness.Spec.Env, requirementsByModel[model]); err != nil {
			return egressext.PolicyDraft{}, err
		}
	}
	for _, agent := range policyAgents(input.Root) {
		for _, tool := range agent.MCPTools {
			if err := b.addMCPServer(tool.Server); err != nil {
				return egressext.PolicyDraft{}, err
			}
		}
	}
	access, err := egress.CanonicalAccess(b.destinations, b.bindings)
	if err != nil {
		return egressext.PolicyDraft{}, NewValidationError("%v", err)
	}
	return egressext.PolicyDraft{Destinations: access.Destinations, Requirements: mergeRequirements(b.requirements), Credentials: publicCredentials(access.Credentials)}, nil
}

// mergeRequirements applies preservation to a whole origin, including overlapping
// resource references, and merges requirements that share a header format.
func mergeRequirements(requirements []egressext.AuthenticationRequirement) []egressext.AuthenticationRequirement {
	merged := slices.Clone(requirements)
	for i := range merged {
		for _, other := range requirements {
			if other.Origin != merged[i].Origin {
				continue
			}
			merged[i].Preserve = merged[i].Preserve || other.Preserve
			if other.Header == merged[i].Header && other.Prefix == merged[i].Prefix {
				merged[i].Required = merged[i].Required || other.Required
			}
		}
	}
	slices.SortFunc(merged, func(a, b egressext.AuthenticationRequirement) int {
		return cmp.Or(strings.Compare(a.Origin, b.Origin), strings.Compare(a.Header, b.Header), strings.Compare(a.Prefix, b.Prefix))
	})
	return slices.Compact(merged)
}

func clonePolicyDraft(draft egressext.PolicyDraft) egressext.PolicyDraft {
	return egressext.PolicyDraft{Destinations: slices.Clone(draft.Destinations), Requirements: slices.Clone(draft.Requirements), Credentials: slices.Clone(draft.Credentials)}
}

func validatePolicyDraft(original, translated egressext.PolicyDraft) (egress.Access, error) {
	if !slices.Equal(original.Destinations, translated.Destinations) || !slices.Equal(original.Requirements, translated.Requirements) {
		return egress.Access{}, NewValidationError("policy extension cannot change destinations or authentication requirements")
	}
	access, err := egress.CanonicalAccess(translated.Destinations, internalCredentials(translated.Credentials))
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
		if err := validateAddedCredential(binding, original.Requirements); err != nil {
			return egress.Access{}, err
		}
	}
	if err := requireCredentials(original.Requirements, access.Credentials); err != nil {
		return egress.Access{}, err
	}
	return access, nil
}

// validateAddedCredential checks an extension binding against the requirements at its origin.
func validateAddedCredential(binding egress.Credential, requirements []egressext.AuthenticationRequirement) error {
	allowed, known, matches := false, false, false
	for _, requirement := range requirements {
		if requirement.Origin != binding.Origin {
			continue
		}
		if requirement.Preserve {
			return NewValidationError("credential destination %q preserves explicit authentication", binding.Origin)
		}
		allowed = true
		if requirement.Header != "" {
			known = true
			matches = matches || bindsTarget(requirement.Origin, requirement.Header, requirement.Prefix)(binding)
		}
	}
	if !allowed {
		return NewValidationError("credential destination %q is not an eligible application target", binding.Origin)
	}
	if known && !matches {
		return NewValidationError("credential for %q must match a model header and prefix", binding.Origin)
	}
	return nil
}

// requireCredentials checks that every required, unpreserved requirement has a binding.
func requireCredentials(requirements []egressext.AuthenticationRequirement, credentials []egress.Credential) error {
	for _, requirement := range requirements {
		if !requirement.Required || requirement.Preserve {
			continue
		}
		if !slices.ContainsFunc(credentials, bindsTarget(requirement.Origin, requirement.Header, requirement.Prefix)) {
			return NewValidationError("model requires a gateway credential for %s header %s; configure apiKeySecret", requirement.Origin, requirement.Header)
		}
	}
	return nil
}
