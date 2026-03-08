/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const discoveryLabel = "kagent.dev/discovery"

// DiscoveryDisabledPredicate filters out resources with the discovery label set to disabled
type DiscoveryDisabledPredicate struct {
	predicate.Funcs
}

func (DiscoveryDisabledPredicate) Create(e event.CreateEvent) bool {
	return !isDiscoveryDisabled(e.Object)
}

func (DiscoveryDisabledPredicate) Update(e event.UpdateEvent) bool {
	return !isDiscoveryDisabled(e.ObjectNew)
}

func (DiscoveryDisabledPredicate) Delete(e event.DeleteEvent) bool {
	return !isDiscoveryDisabled(e.Object)
}

func (DiscoveryDisabledPredicate) Generic(e event.GenericEvent) bool {
	return !isDiscoveryDisabled(e.Object)
}

func isDiscoveryDisabled(obj client.Object) bool {
	if obj == nil {
		return false
	}

	labels := obj.GetLabels()
	if labels == nil {
		return false
	}

	discoveryLabelValue, exists := labels[discoveryLabel]
	return exists && discoveryLabelValue == "disabled"
}
