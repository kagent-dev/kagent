package translator

import (
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// CredentialPlaceholder satisfies SDKs that require a configured API key.
const CredentialPlaceholder = "kagent-credential-injected"

// ResolvedPolicy contains pinned integration access and the authentication used to render clients.
// Its private fields prevent renderers from changing authentication decisions.
type ResolvedPolicy struct {
	access        egress.Access
	modelKeys     []string
	modelBindings map[*ResolvedModelConfig]bool
	secretModels  map[string]bool
	secretMCP     map[string]bool
	headers       map[string][]string
	// tokenPassthroughOrigins cannot receive gateway credentials, including live grants.
	tokenPassthroughOrigins []string
}

// ModelUsesGateway reports the resolved authentication mode of a model.
func (p ResolvedPolicy) ModelUsesGateway(model *ResolvedModelConfig) bool {
	return p.modelBindings[model]
}

// RenderEnvironment replaces supported Secret references and emits resolved SDK keys.
func (p ResolvedPolicy) RenderEnvironment(environment []corev1.EnvVar) ([]corev1.EnvVar, error) {
	result := slices.Clone(environment)
	for i, variable := range result {
		if variable.ValueFrom == nil {
			continue
		}
		ref := variable.ValueFrom.SecretKeyRef
		isMCP := strings.HasPrefix(variable.Name, "KAGENT_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CODEX_MCP_CREDENTIAL_") || strings.HasPrefix(variable.Name, "KAGENT_CLAUDE_MCP_CREDENTIAL_")
		if ref == nil || (!p.secretModels[variable.Name+"\x00"+ref.Name+"\x00"+ref.Key] && (!isMCP || !p.secretMCP[ref.Name+"\x00"+ref.Key])) {
			return nil, NewValidationError("environment credential %q cannot use gateway header injection; local signing and arbitrary secret environment variables are unsupported", variable.Name)
		}
		result[i].Value, result[i].ValueFrom = CredentialPlaceholder, nil
	}
	for _, name := range p.modelKeys {
		if !slices.ContainsFunc(result, func(variable corev1.EnvVar) bool { return variable.Name == name }) {
			result = append(result, corev1.EnvVar{Name: name, Value: CredentialPlaceholder})
		}
	}
	return result, nil
}

// MCPHeaders renders missing headers from the resolved client authentication.
func (p ResolvedPolicy) MCPHeaders(rawURL string, headers map[string]string) (map[string]string, error) {
	if len(p.headers) == 0 {
		return headers, nil
	}
	origin, err := credentialOrigin(rawURL)
	if err != nil {
		return nil, NewValidationError("invalid MCP credential destination: %v", err)
	}
	for _, header := range p.headers[origin] {
		present := false
		for name := range headers {
			present = present || strings.EqualFold(name, header)
		}
		if !present {
			if headers == nil {
				headers = map[string]string{}
			}
			headers[header] = CredentialPlaceholder
		}
	}
	return headers, nil
}
