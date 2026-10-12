package translator

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// CompileEgressPolicies resolves same-namespace grants for either an Agent or a
// SandboxTemplate. KRT tracks policy and Secret dependencies; only references
// enter the result, so Secret value rotation does not change a revision.
func CompileEgressPolicies(ctx krt.HandlerContext, collections Collections, namespace string, refs []corev1.LocalObjectReference) (egress.Access, error) {
	if len(refs) > 16 {
		return egress.Access{}, NewValidationError("at most 16 egressPolicyRefs are supported")
	}
	var destinations []string
	var credentials []egress.Credential
	for _, ref := range refs {
		if len(validation.IsDNS1123Subdomain(ref.Name)) != 0 {
			return egress.Access{}, NewValidationError("invalid EgressPolicy reference %q", ref.Name)
		}
		key := namespace + "/" + ref.Name
		policy := krt.FetchOne(ctx, collections.EgressPolicies, krt.FilterKey(key))
		if policy == nil {
			return egress.Access{}, fmt.Errorf("resolve EgressPolicy %q: not found", key)
		}
		if len((*policy).Spec.Destinations) == 0 || len((*policy).Spec.Destinations) > 64 {
			return egress.Access{}, NewValidationError("EgressPolicy %q requires 1 to 64 destinations", key)
		}
		for _, destination := range (*policy).Spec.Destinations {
			origin, err := egress.ParseOrigin(destination.Origin)
			if err != nil {
				return egress.Access{}, NewValidationError("EgressPolicy %q: %v", key, err)
			}
			destinations = append(destinations, origin.String())
			auth := destination.Authentication
			if auth == nil {
				continue
			}
			if origin.Scheme != "https" || strings.Contains(origin.Host, "*") {
				return egress.Access{}, NewValidationError("EgressPolicy %q: authentication requires an exact HTTPS origin", key)
			}
			secret := auth.SecretKeyRef
			binding := egress.Credential{Origin: origin.String(), Header: auth.Header}
			if auth.Prefix != nil {
				binding.Prefix = *auth.Prefix
			}
			if secret.Name == "" || secret.Key == "" || (secret.Optional != nil && *secret.Optional) {
				return egress.Access{}, NewValidationError("EgressPolicy %q requires a non-optional Secret name and key", key)
			}
			binding.URI = "ate-secret://k8s.io/default/" + namespace + "/" + secret.Name + "/" + secret.Key
			if _, err := egress.CanonicalCredentials([]egress.Credential{binding}); err != nil {
				return egress.Access{}, NewValidationError("EgressPolicy %q: %v", key, err)
			}
			found := krt.FetchOne(ctx, collections.Secrets, krt.FilterKey(namespace+"/"+secret.Name))
			if found == nil {
				return egress.Access{}, fmt.Errorf("EgressPolicy %q: Secret %q not found", key, secret.Name)
			}
			if _, ok := (*found).Data[secret.Key]; !ok {
				return egress.Access{}, fmt.Errorf("EgressPolicy %q: Secret %q is missing key %q", key, secret.Name, secret.Key)
			}
			credentials = append(credentials, binding)
		}
	}
	access, err := egress.CanonicalAccess(destinations, credentials)
	if err != nil {
		return egress.Access{}, NewValidationError("%v", err)
	}
	return access, nil
}
