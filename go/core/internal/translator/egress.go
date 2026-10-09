package translator

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Agents and SandboxTemplates declare egress alike and compile it here.

// SecretKeyLookup returns a SecretNotFoundError when a Secret key is missing.
// Fetching through krt recompiles the caller when the Secret changes.
type SecretKeyLookup func(namespace, name, key string) error

// RequireSecretKey is the SecretKeyLookup over a Secret collection.
func RequireSecretKey(ctx krt.HandlerContext, secrets krt.Collection[*corev1.Secret], namespace, name, key string) error {
	secret := krt.FetchOne(ctx, secrets, krt.FilterObjectName(types.NamespacedName{Namespace: namespace, Name: name}))
	if secret == nil {
		return &SecretNotFoundError{Secret: types.NamespacedName{Namespace: namespace, Name: name}}
	}
	if _, ok := (*secret).Data[key]; !ok {
		return &SecretNotFoundError{Secret: types.NamespacedName{Namespace: namespace, Name: name}, Key: key}
	}
	return nil
}

// WithEgress adds declared origins to a runtime's destinations, once each.
func WithEgress(compiled []string, declared []v1alpha3.EgressEntry) ([]string, error) {
	if len(declared) == 0 {
		return compiled, nil
	}
	destinations := slices.Clone(compiled)
	for _, entry := range declared {
		origin, err := egress.ParseOrigin(entry.Origin)
		if err != nil {
			return nil, NewValidationError("egress: %v", err)
		}
		if !slices.Contains(destinations, origin) {
			destinations = append(destinations, origin)
		}
	}
	return destinations, nil
}

// EgressCredentials compiles declared headers to gateway bindings.
func EgressCredentials(namespace string, declared []v1alpha3.EgressEntry, lookup SecretKeyLookup) ([]egress.Credential, error) {
	var bindings []egress.Credential
	for _, entry := range declared {
		if len(entry.Headers) == 0 {
			continue
		}
		host, _, err := headerOrigin(entry.Origin)
		if err != nil {
			return nil, err
		}
		for _, header := range entry.Headers {
			ref := header.ValueFrom.SecretKeyRef
			if ref == nil {
				return nil, NewValidationError("egress %q header %q: secretKeyRef is required", entry.Origin, header.Name)
			}
			binding := egress.Credential{
				Hostname: host,
				Header:   header.Name,
				Prefix:   header.Prefix,
				URI:      "ate-secret://k8s.io/default/" + namespace + "/" + ref.Name + "/" + ref.Key,
			}
			// A malformed reference can never resolve, so it is invalid, not missing.
			if _, err := egress.CanonicalCredentials([]egress.Credential{binding}); err != nil {
				return nil, NewValidationError("egress %q header %q: %v", entry.Origin, header.Name, err)
			}
			if strings.EqualFold(header.Name, "host") {
				return nil, NewValidationError("egress %q: the gateway cannot set the Host header", entry.Origin)
			}
			if err := lookup(namespace, ref.Name, ref.Key); err != nil {
				return nil, fmt.Errorf("egress %q header %q: %w", entry.Origin, header.Name, err)
			}
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}

// headerOrigin returns the host and port of an origin that may carry headers:
// exact and https, since Substrate injects only into intercepted HTTPS.
func headerOrigin(value string) (host, port string, err error) {
	origin, err := egress.ParseOrigin(value)
	if err != nil {
		return "", "", NewValidationError("egress: %v", err)
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || strings.HasPrefix(u.Hostname(), "*.") {
		return "", "", NewValidationError("egress %q: headers need an exact https origin", value)
	}
	return u.Hostname(), u.Port(), nil
}

// CheckEgressHeaderPorts refuses headers on a host also reached over HTTPS on
// another port, since Substrate binds credentials per host.
func CheckEgressHeaderPorts(destinations []string, declared []v1alpha3.EgressEntry) error {
	for _, entry := range declared {
		if len(entry.Headers) == 0 {
			continue
		}
		host, port, err := headerOrigin(entry.Origin)
		if err != nil {
			return err
		}
		for _, destination := range destinations {
			u, err := url.Parse(destination)
			if err == nil && u.Scheme == "https" && u.Hostname() == host && u.Port() != port {
				return NewValidationError("egress %q: headers would also be sent to %s, since the gateway binds them per host", entry.Origin, destination)
			}
		}
	}
	return nil
}
