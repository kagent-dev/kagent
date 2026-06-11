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
// For sandbox workloads it keeps only the owned types for the agent's sandbox platform.
//
// translatorOwnedTypes is typically AdkApiTranslator.GetOwnedResourceTypes() (full set used for watches).
func FilterTranslatorOwnedTypesForList(cl client.Client, agent v1alpha2.AgentObject, translatorOwnedTypes []client.Object, backend Backend) ([]client.Object, error) {
	if backend == nil {
		return translatorOwnedTypes, nil
	}

	allSandboxTypes := backend.GetOwnedResourceTypes()
	if len(allSandboxTypes) == 0 {
		return translatorOwnedTypes, nil
	}

	var keepSandboxTypes []client.Object
	if agent.GetWorkloadMode() == v1alpha2.WorkloadModeSandbox {
		types, err := backend.OwnedResourceTypesFor(agent)
		if err != nil {
			return nil, fmt.Errorf("sandbox owned resource types for agent: %w", err)
		}
		keepSandboxTypes = types
	}

	remove, err := sandboxOwnedTypesToRemove(cl, allSandboxTypes, keepSandboxTypes)
	if err != nil {
		return nil, err
	}
	if len(remove) == 0 {
		return translatorOwnedTypes, nil
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

func sandboxOwnedTypesToRemove(cl client.Client, allSandboxTypes, keepSandboxTypes []client.Object) (map[schema.GroupVersionKind]struct{}, error) {
	keep := make(map[schema.GroupVersionKind]struct{}, len(keepSandboxTypes))
	for _, o := range keepSandboxTypes {
		gvk, err := apiutil.GVKForObject(o, cl.Scheme())
		if err != nil {
			return nil, fmt.Errorf("sandbox backend owned type: %w", err)
		}
		keep[gvk] = struct{}{}
	}
	remove := make(map[schema.GroupVersionKind]struct{}, len(allSandboxTypes))
	for _, o := range allSandboxTypes {
		gvk, err := apiutil.GVKForObject(o, cl.Scheme())
		if err != nil {
			return nil, fmt.Errorf("sandbox backend owned type: %w", err)
		}
		if _, ok := keep[gvk]; !ok {
			remove[gvk] = struct{}{}
		}
	}
	return remove, nil
}
