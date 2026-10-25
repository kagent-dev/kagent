package translator

import (
	"fmt"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"

	configv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/config/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// CompileCredentialHeaders resolves one integration's credential headers to the same
// origin/header bindings used for explicit egress destinations. Secret material
// is never read or included in the compilation result.
func CompileCredentialHeaders(endpoint, namespace string, bindings []v1alpha3.Header) (egress.Access, error) {
	if len(bindings) == 0 {
		return egress.Access{}, nil
	}
	if len(bindings) > 16 {
		return egress.Access{}, NewValidationError("credential headers require 1 to 16 headers")
	}
	origin, err := canonicalOrigin(endpoint)
	if err != nil {
		return egress.Access{}, err
	}
	var credentials []egress.Credential
	var targets []egresspolicy.GatewayTarget
	names := map[string]bool{}
	for _, header := range bindings {
		name := strings.ToLower(header.Name)
		if names[name] {
			return egress.Access{}, NewValidationError("duplicate credential header %q", header.Name)
		}
		names[name] = true
		prefix := ""
		if header.Prefix != nil {
			prefix = *header.Prefix
		}
		if (header.ValueFrom.SecretKeyRef == nil) == (header.ValueFrom.TokenPassthrough == nil) {
			return egress.Access{}, NewValidationError("credential header %q requires exactly one source", header.Name)
		}
		if secret := header.ValueFrom.SecretKeyRef; secret != nil {
			if secret.Name == "" || secret.Key == "" || (secret.Optional != nil && *secret.Optional) {
				return egress.Access{}, NewValidationError("credential headers require a non-optional Secret name and key")
			}
			credentials = append(credentials, egress.Credential{Origin: origin.String(), Header: name, Prefix: prefix,
				URI: fmt.Sprintf("ate-secret://k8s.io/default/%s/%s/%s", namespace, secret.Name, secret.Key)})
		} else {
			targets = append(targets, egresspolicy.GatewayTarget{Origin: origin.String(), Header: name, Prefix: prefix})
		}
	}
	access, err := egress.CanonicalAccess([]string{origin.String()}, credentials, targets)
	if err != nil {
		return egress.Access{}, NewValidationError("%v", err)
	}
	return access, nil
}

// RuntimeCredentialHeaders supplies inert values for Secret injection and explicit
// invocation-header instructions. Ordinary headers cannot override credentials.
func RuntimeCredentialHeaders(bindings []v1alpha3.Header, ordinary map[string]string) (map[string]string, []*configv1.InvocationCredentialHeader, error) {
	headers := maps.Clone(ordinary)
	if len(bindings) == 0 {
		return headers, nil, nil
	}
	if headers == nil {
		headers = map[string]string{}
	}
	var passthrough []*configv1.InvocationCredentialHeader
	for _, header := range bindings {
		for name := range ordinary {
			if strings.EqualFold(name, header.Name) {
				return nil, nil, NewValidationError("ordinary header %q conflicts with credential headers", name)
			}
		}
		prefix := ""
		if header.Prefix != nil {
			prefix = *header.Prefix
		}
		headers[header.Name] = prefix + CredentialPlaceholder
		if header.ValueFrom.TokenPassthrough != nil {
			passthrough = append(passthrough, &configv1.InvocationCredentialHeader{Name: header.Name, Prefix: prefix})
		}
	}
	return headers, passthrough, nil
}

// ModelCredentialHeaders owns the SDK placeholder and resolved provider endpoint.
// All configured credential headers still use the shared binding compiler.
func ModelCredentialHeaders(resolved *ResolvedModelConfig, ordinary map[string]string) (map[string]string, []*configv1.InvocationCredentialHeader, []corev1.EnvVar, error) {
	spec := resolved.Config.Spec
	name, endpoint, _, _ := modelCredentialTarget(resolved)
	if _, err := CompileCredentialHeaders(endpoint, resolved.Config.Namespace, spec.Headers); err != nil {
		return nil, nil, nil, err
	}
	headers, passthrough, err := RuntimeCredentialHeaders(spec.Headers, ordinary)
	if err != nil {
		return nil, nil, nil, err
	}
	var environment []corev1.EnvVar
	if name != "" && len(spec.Headers) > 0 {
		environment = append(environment, corev1.EnvVar{Name: name, Value: CredentialPlaceholder})
	}
	return headers, passthrough, environment, nil
}
