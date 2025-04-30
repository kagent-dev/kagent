package controller

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ctrl "sigs.k8s.io/controller-runtime"
)

var predicateLog = ctrl.Log.WithName("predicates")

// isNamespaceAllowed checks if a namespace is in the allowed list
// If the allowedMap is empty, all namespaces are allowed
func isNamespaceAllowed(ns string, allowedMap map[string]bool) bool {
	// If no namespaces specified (empty map), allow all
	if len(allowedMap) == 0 {
		return true
	}
	return allowedMap[ns]
}

// logNamespaceFilteredEvent logs when an event is filtered due to namespace restrictions
func logNamespaceFilteredEvent(obj client.Object, eventType string) {
	predicateLog.V(4).Info(
		fmt.Sprintf("ignoring %s event for resource in non-watched namespace", eventType),
		"namespace", obj.GetNamespace(),
		"kind", obj.GetObjectKind().GroupVersionKind().Kind,
		"name", obj.GetName(),
	)
}

// NewNamespaceFilterPredicate creates a predicate that filters events based on
// a list of allowed namespaces. If the list is empty, all namespaces are allowed.
func NewNamespaceFilterPredicate(allowedNamespaces []string) predicate.Predicate {
	// Convert to map for quick lookup
	allowedMap := make(map[string]bool, len(allowedNamespaces))
	for _, ns := range allowedNamespaces {
		if ns != "" { // Skip empty namespace names
			allowedMap[ns] = true
		}
	}

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			allowed := isNamespaceAllowed(e.Object.GetNamespace(), allowedMap)
			if !allowed {
				logNamespaceFilteredEvent(e.Object, "create")
			}
			return allowed
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			allowed := isNamespaceAllowed(e.ObjectNew.GetNamespace(), allowedMap)
			if !allowed {
				logNamespaceFilteredEvent(e.ObjectNew, "update")
			}
			return allowed
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			allowed := isNamespaceAllowed(e.Object.GetNamespace(), allowedMap)
			if !allowed {
				logNamespaceFilteredEvent(e.Object, "delete")
			}
			return allowed
		},
		GenericFunc: func(e event.GenericEvent) bool {
			allowed := isNamespaceAllowed(e.Object.GetNamespace(), allowedMap)
			if !allowed {
				logNamespaceFilteredEvent(e.Object, "generic")
			}
			return allowed
		},
	}
}
