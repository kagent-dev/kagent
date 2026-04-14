package sandboxbackend

import (
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// FilterTranslatorOwnedTypesForList returns the owned-resource types the reconciler should pass to
// FindOwnedObjects. It drops sandbox-backend-only types when the workload is not sandbox, so
// reconcile does not List agent-sandbox APIs on clusters where those CRDs are not installed.
//
// translatorOwnedTypes is typically AdkApiTranslator.GetOwnedResourceTypes() (full set used for watches).
func FilterTranslatorOwnedTypesForList(cl client.Client, agent v1alpha2.AgentObject, translatorOwnedTypes []client.Object, backend Backend) ([]client.Object, error) {
	if backend == nil || agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox {
		return translatorOwnedTypes, nil
	}
	sandboxOnly := backend.GetOwnedResourceTypes()
	if len(sandboxOnly) == 0 {
		return translatorOwnedTypes, nil
	}
	remove := make(map[schema.GroupVersionKind]struct{}, len(sandboxOnly))
	for _, o := range sandboxOnly {
		gvk, err := apiutil.GVKForObject(o, cl.Scheme())
		if err != nil {
			return nil, fmt.Errorf("sandbox backend owned type: %w", err)
		}
		remove[gvk] = struct{}{}
	}
	var out []client.Object
	for _, o := range translatorOwnedTypes {
		gvk, err := apiutil.GVKForObject(o, cl.Scheme())
		if err != nil {
			return nil, fmt.Errorf("translator owned type: %w", err)
		}
		if _, skip := remove[gvk]; skip {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}
