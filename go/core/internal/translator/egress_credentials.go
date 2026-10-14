package translator

import (
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

// contributeCredentials calls the extension with eligible destinations and validates its result.
func contributeCredentials(input *HarnessInput, models []*ResolvedModelConfig, existing []egress.Credential, binder egressext.CredentialBinder) ([]egress.Credential, error) {
	if binder == nil {
		return nil, nil
	}
	targets := eligibleCredentialTargets(input, models)
	current := make([]egressext.Credential, len(existing))
	for i, binding := range existing {
		current[i] = egressext.Credential(binding)
	}
	agent := types.NamespacedName{Namespace: input.Harness.Namespace, Name: input.AgentName}
	// Extension mutations must not change the compiler's bindings or validation targets.
	contributed, err := binder.Bindings(agent, slices.Clone(targets), current)
	if err != nil {
		return nil, fmt.Errorf("failed to contribute credentials: %w", err)
	}
	return validateContributedCredentials(contributed, targets)
}

// eligibleCredentialTargets selects exact HTTPS destinations that permit additional authentication.
func eligibleCredentialTargets(input *HarnessInput, models []*ResolvedModelConfig) []egressext.CredentialTarget {
	targets := map[string][]egressext.CredentialTarget{}
	reserved := map[string]bool{}
	add := func(raw, header, prefix string, preserve bool) {
		origin, err := credentialOrigin(raw)
		if err != nil || !strings.HasPrefix(origin, "https://") || strings.Contains(origin, "*") {
			return
		}
		// Header replacement applies to the whole origin, so any explicit auth reserves it.
		reserved[origin] = reserved[origin] || preserve
		targets[origin] = append(targets[origin], egressext.CredentialTarget{Origin: origin, Header: header, Prefix: prefix})
	}
	for _, model := range models {
		name, endpoint, header, prefix := modelCredentialTarget(model)
		literalKey := slices.ContainsFunc(input.Harness.Spec.Env, func(variable v1alpha3.RuntimeEnvVar) bool { return name != "" && variable.Name == name })
		// Keyless Gemini needs runtime support before a binding can supply its key.
		keylessGemini := model.Config.Spec.Provider == v1alpha3.ModelProviderGemini && model.Config.Spec.APIKeySecret == ""
		add(endpoint, header, prefix, literalKey || keylessGemini || model.Config.Spec.APIKeyPassthrough || len(model.Config.Spec.DefaultHeaders) != 0)
	}
	var visit func(*AgentInput)
	visit = func(agent *AgentInput) {
		for _, tool := range agent.MCPTools {
			// Any literal or ConfigMap header can carry authentication.
			preserve := slices.ContainsFunc(tool.Server.Spec.HeadersFrom, func(ref v1alpha3.ValueRef) bool {
				return ref.ValueFrom == nil || ref.ValueFrom.Type != v1alpha3.SecretValueSource
			})
			add(tool.Server.Spec.URL, "", "", preserve)
		}
		for _, child := range agent.Shared {
			visit(child.Agent)
		}
	}
	visit(input.Root)
	var eligible []egressext.CredentialTarget
	for origin, formats := range targets {
		if reserved[origin] {
			continue
		}
		// A generic MCP target must not relax a model's required header format.
		known := slices.ContainsFunc(formats, func(target egressext.CredentialTarget) bool { return target.Header != "" })
		for _, target := range formats {
			if target.Header != "" || !known {
				eligible = append(eligible, target)
			}
		}
	}
	// Keep extension inputs stable across map iteration and duplicate references.
	slices.SortFunc(eligible, func(a, b egressext.CredentialTarget) int {
		return strings.Compare(a.Origin+"\x00"+a.Header+"\x00"+a.Prefix, b.Origin+"\x00"+b.Header+"\x00"+b.Prefix)
	})
	return slices.Compact(eligible)
}

// validateContributedCredentials converts bindings and checks their origins and header formats.
func validateContributedCredentials(contributed []egressext.Credential, targets []egressext.CredentialTarget) ([]egress.Credential, error) {
	added := make([]egress.Credential, len(contributed))
	for i, binding := range contributed {
		added[i] = egress.Credential(binding)
	}
	added, err := egress.CanonicalCredentials(added)
	if err != nil {
		return nil, NewValidationError("%v", err)
	}
	for _, binding := range added {
		matches := slices.ContainsFunc(targets, func(target egressext.CredentialTarget) bool {
			if target.Origin != binding.Origin {
				return false
			}
			return target.Header == "" || (target.Header == binding.Header && target.Prefix == binding.Prefix)
		})
		if matches {
			continue
		}
		allowed := slices.ContainsFunc(targets, func(target egressext.CredentialTarget) bool { return target.Origin == binding.Origin })
		if !allowed {
			return nil, NewValidationError("credential destination %q is not an eligible application target", binding.Origin)
		}
		return nil, NewValidationError("credential for %q must match a model header and prefix", binding.Origin)
	}
	return added, nil
}

// CredentialHeaders adds placeholders for contributed credentials to compiled MCP headers.
func CredentialHeaders(rawURL string, headers map[string]string, additions []egress.Credential) (map[string]string, error) {
	if len(additions) == 0 {
		return headers, nil
	}
	origin, err := credentialOrigin(rawURL)
	if err != nil {
		return nil, NewValidationError("invalid MCP credential destination: %v", err)
	}
	for _, binding := range additions {
		if binding.Origin != origin {
			continue
		}
		present := false
		for name := range headers {
			present = present || strings.EqualFold(name, binding.Header)
		}
		if !present {
			if headers == nil {
				headers = map[string]string{}
			}
			headers[binding.Header] = CredentialPlaceholder
		}
	}
	return headers, nil
}
