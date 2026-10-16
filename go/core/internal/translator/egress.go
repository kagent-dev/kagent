package translator

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// CompileEgressPolicies resolves same-namespace grants for either an Agent or a
// SandboxTemplate. KRT tracks policy and Secret dependencies; only references
// enter the result, so Secret value rotation does not change a revision.
func CompileEgressPolicies(ctx krt.HandlerContext, collections Collections, namespace string, refs []corev1.LocalObjectReference) (egress.Access, error) {
	return compileEgressPolicies(namespace, refs, func(name string) (*v1alpha3.EgressPolicy, error) {
		policy := krt.FetchOne(ctx, collections.EgressPolicies, krt.FilterKey(namespace+"/"+name))
		if policy == nil {
			return nil, apierrors.NewNotFound(schema.GroupResource{Group: "api.kagent.dev", Resource: "egresspolicies"}, name)
		}
		return *policy, nil
	}, func(name string) (*corev1.Secret, error) {
		secret := krt.FetchOne(ctx, collections.Secrets, krt.FilterKey(namespace+"/"+name))
		if secret == nil {
			return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
		}
		return *secret, nil
	})
}

func compileEgressPolicies(namespace string, refs []corev1.LocalObjectReference, getPolicy func(string) (*v1alpha3.EgressPolicy, error), getSecret func(string) (*corev1.Secret, error)) (egress.Access, error) {
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
		policy, err := getPolicy(ref.Name)
		if err != nil {
			return egress.Access{}, fmt.Errorf("resolve EgressPolicy %q: %w", key, err)
		}
		if !policy.DeletionTimestamp.IsZero() {
			return egress.Access{}, NewValidationError("EgressPolicy %q is being deleted", key)
		}
		if len(policy.Spec.Destinations) == 0 || len(policy.Spec.Destinations) > 64 {
			return egress.Access{}, NewValidationError("EgressPolicy %q requires 1 to 64 destinations", key)
		}
		for _, destination := range policy.Spec.Destinations {
			origin, err := egress.ParseOrigin(destination.Origin)
			if err != nil {
				return egress.Access{}, NewValidationError("EgressPolicy %q: %v", key, err)
			}
			destinations = append(destinations, origin.String())
			auth := destination.Authentication
			if auth == nil {
				continue
			}
			if strings.Contains(origin.Host, "*") {
				return egress.Access{}, NewValidationError("EgressPolicy %q: authentication requires an exact HTTP(S) origin", key)
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
			found, err := getSecret(secret.Name)
			if err != nil {
				return egress.Access{}, fmt.Errorf("EgressPolicy %q: Secret %q: %w", key, secret.Name, err)
			}
			if _, ok := found.Data[secret.Key]; !ok {
				return egress.Access{}, NewValidationError("EgressPolicy %q: Secret %q is missing key %q", key, secret.Name, secret.Key)
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
