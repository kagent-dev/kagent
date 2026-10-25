package translator

import (
	"fmt"
	"slices"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

// EgressGrantIssue describes one policy resolution error.
type EgressGrantIssue struct {
	Policy  string
	Reason  string
	Message string
}

// EgressGrantResolution contains the union of individually valid policies.
// Admission checks conflicts across this union and the desired revision's base.
type EgressGrantResolution struct {
	Access egress.Access
	Issues []EgressGrantIssue
}

// CompileEgressPolicies resolves each same-namespace policy independently.
// KRT tracks policy and Secret dependencies. An unresolved policy contributes
// no grants, without removing grants supplied by the remaining policies.
func CompileEgressPolicies(ctx krt.HandlerContext, collections Collections, namespace string, refs []corev1.LocalObjectReference) EgressGrantResolution {
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

func compileEgressPolicies(namespace string, refs []corev1.LocalObjectReference, getPolicy func(string) (*v1alpha3.EgressPolicy, error), getSecret func(string) (*corev1.Secret, error)) EgressGrantResolution {
	var result EgressGrantResolution
	var invalidRefs string
	if len(refs) > 16 {
		invalidRefs = "at most 16 egressPolicyRefs are supported"
	}
	for _, ref := range refs {
		if len(validation.IsDNS1123Subdomain(ref.Name)) != 0 {
			invalidRefs = fmt.Sprintf("invalid EgressPolicy reference %q", ref.Name)
			break
		}
	}
	for _, ref := range refs {
		issue := EgressGrantIssue{Policy: namespace + "/" + ref.Name, Reason: v1alpha3.EgressPoliciesReasonInvalidEgressPolicy}
		if invalidRefs != "" {
			issue.Message = invalidRefs
			result.Issues = append(result.Issues, issue)
			continue
		}
		policy, err := getPolicy(ref.Name)
		if err != nil || !policy.DeletionTimestamp.IsZero() {
			if err == nil || apierrors.IsNotFound(err) {
				issue.Reason = v1alpha3.EgressPoliciesReasonNotFound
			}
			issue.Message = "EgressPolicy is being deleted"
			if err != nil {
				issue.Message = err.Error()
			}
			result.Issues = append(result.Issues, issue)
			continue
		}
		access, err := compileEgressPolicy(namespace, policy, getSecret)
		if err != nil {
			issue.Message = err.Error()
			result.Issues = append(result.Issues, issue)
			continue
		}
		result.Access.Destinations = append(result.Access.Destinations, access.Destinations...)
		result.Access.Credentials = append(result.Access.Credentials, access.Credentials...)
		result.Access.TokenPassthroughTargets = append(result.Access.TokenPassthroughTargets, access.TokenPassthroughTargets...)
	}
	slices.SortFunc(result.Issues, func(a, b EgressGrantIssue) int { return strings.Compare(a.Policy, b.Policy) })
	return result
}

func compileEgressPolicy(namespace string, policy *v1alpha3.EgressPolicy, getSecret func(string) (*corev1.Secret, error)) (egress.Access, error) {
	if len(policy.Spec.Destinations) == 0 || len(policy.Spec.Destinations) > 64 {
		return egress.Access{}, NewValidationError("requires 1 to 64 destinations")
	}
	var destinations []string
	var credentials []egress.Credential
	var targets []egresspolicy.GatewayTarget
	for _, destination := range policy.Spec.Destinations {
		origin, err := egress.ParseOrigin(destination.Origin)
		if err != nil {
			return egress.Access{}, err
		}
		destinations = append(destinations, origin.String())
		headers := destination.Headers
		if len(headers) == 0 {
			continue
		}
		if strings.Contains(origin.Host, "*") {
			return egress.Access{}, NewValidationError("credential headers require an exact HTTP(S) origin")
		}
		access, err := CompileCredentialHeaders(origin.String(), namespace, headers)
		if err != nil {
			return egress.Access{}, err
		}
		for _, header := range headers {
			secret := header.ValueFrom.SecretKeyRef
			if secret == nil {
				continue
			}
			found, err := getSecret(secret.Name)
			if err != nil {
				return egress.Access{}, fmt.Errorf("resolve Secret %q: %w", secret.Name, err)
			}
			if _, ok := found.Data[secret.Key]; !ok {
				return egress.Access{}, NewValidationError("Secret %q is missing key %q", secret.Name, secret.Key)
			}
		}
		credentials = append(credentials, access.Credentials...)
		targets = append(targets, access.TokenPassthroughTargets...)
	}
	return egress.CanonicalAccess(destinations, credentials, targets)
}
