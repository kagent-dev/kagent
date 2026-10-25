package translator

import (
	"fmt"
	"slices"

	"istio.io/istio/pkg/kube/krt"

	v1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

// ResolveIntegrationGrants watches the applied integration sources even when
// unrelated runtime inputs fail compilation. Deletion, replacement, and valid
// header removals revoke access. Invalid source configuration preserves its last
// resolved bindings until corrected; it never restores a revoked binding from
// failed input. The returned source state must be persisted with the grant union.
func ResolveIntegrationGrants(ctx krt.HandlerContext, collections Collections, namespace string, applied []egress.IntegrationGrant) (EgressGrantResolution, []egress.IntegrationGrant) {
	var result EgressGrantResolution
	updated := slices.Clone(applied)
	for i, grant := range applied {
		access, err := resolveIntegrationGrant(ctx, collections, namespace, grant)
		if err != nil {
			access = grant.Access()
			result.Issues = append(result.Issues, EgressGrantIssue{Policy: grant.Kind + "/" + namespace + "/" + grant.Name,
				Reason: v1alpha3.EgressPoliciesReasonInvalidEgressPolicy, Message: err.Error()})
		} else {
			updated[i] = grant.WithAccess(access)
		}
		result.Access.Destinations = append(result.Access.Destinations, access.Destinations...)
		result.Access.TokenPassthroughTargets = append(result.Access.TokenPassthroughTargets, access.TokenPassthroughTargets...)
	}
	return result, updated
}

func resolveIntegrationGrant(ctx krt.HandlerContext, collections Collections, namespace string, grant egress.IntegrationGrant) (egress.Access, error) {
	var endpoint string
	var headers []v1alpha3.Header
	switch grant.Kind {
	case egress.IntegrationRemoteMCPServer:
		found := krt.FetchOne(ctx, collections.RemoteMCPServers, krt.FilterKey(namespace+"/"+grant.Name))
		if found == nil || !(*found).DeletionTimestamp.IsZero() || string((*found).UID) != grant.UID {
			return egress.Access{}, nil
		}
		endpoint, headers = (*found).Spec.URL, (*found).Spec.Headers
	case egress.IntegrationModelConfig:
		model := krt.FetchOne(ctx, collections.ResolvedModelConfigs, krt.FilterKey(namespace+"/"+grant.Name))
		if model == nil || !model.Config.DeletionTimestamp.IsZero() || string(model.Config.UID) != grant.UID {
			return egress.Access{}, nil
		}
		_, endpoint, _, _ = modelCredentialTarget(model)
		headers = model.Config.Spec.Headers
	default:
		return egress.Access{}, fmt.Errorf("invalid integration kind %q", grant.Kind)
	}
	current, err := CompileCredentialHeaders(endpoint, namespace, headers)
	if err != nil {
		return egress.Access{}, err
	}
	grant.RevokedTargets = nil
	return egress.IntersectAccess(grant.Access(), current), nil
}
