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

package v1alpha2

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FromNamespaces specifies namespace from which references to this resource are allowed.
// This follows the same pattern as Gateway API's cross-namespace route attachment.
// See: https://gateway-api.sigs.k8s.io/guides/multiple-ns/#cross-namespace-routing
// +kubebuilder:validation:Enum=All;Same;Selector
type FromNamespaces string

const (
	// NamespacesFromAll allows references from all namespaces.
	NamespacesFromAll FromNamespaces = "All"
	// NamespacesFromSame only allows references from the same namespace as the target resource (default).
	NamespacesFromSame FromNamespaces = "Same"
	// NamespacesFromSelector allows references from namespaces matching the selector.
	NamespacesFromSelector FromNamespaces = "Selector"
)

// AllowedNamespaces defines which namespaces are allowed to reference this resource.
// This mechanism provides a bidirectional handshake for cross-namespace references,
// following the pattern used by Gateway API for cross-namespace route attachments.
//
// By default (when not specified), only references from the same namespace are allowed.
// +kubebuilder:validation:XValidation:rule="!(self.from == 'Selector' && !has(self.selector))",message="selector must be specified when from is Selector"
type AllowedNamespaces struct {
	// From indicates where references to this resource can originate.
	// Possible values are:
	// * All: References from all namespaces are allowed.
	// * Same: Only references from the same namespace are allowed (default).
	// * Selector: References from namespaces matching the selector are allowed.
	// +kubebuilder:default=Same
	// +optional
	From FromNamespaces `json:"from,omitempty"`

	// Selector is a label selector for namespaces that are allowed to reference this resource.
	// Only used when From is set to "Selector".
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// AllowsNamespace checks if a reference from the given namespace is allowed.
// The targetNamespace is the namespace where the resource being referenced lives.
// The sourceNamespace is the namespace where the referencing resource lives.
func (a *AllowedNamespaces) AllowsNamespace(ctx context.Context, c client.Client, sourceNamespace, targetNamespace string) (bool, error) {
	// If AllowedNamespaces is nil, default to same namespace only
	if a == nil {
		return sourceNamespace == targetNamespace, nil
	}

	switch a.From {
	case NamespacesFromAll:
		return true, nil
	case NamespacesFromSame, "":
		return sourceNamespace == targetNamespace, nil
	case NamespacesFromSelector:
		if a.Selector == nil {
			return false, fmt.Errorf("selector must be specified when from is Selector")
		}

		// Get the source namespace to check its labels
		ns := &corev1.Namespace{}
		if err := c.Get(ctx, types.NamespacedName{Name: sourceNamespace}, ns); err != nil {
			return false, fmt.Errorf("failed to get namespace %s: %w", sourceNamespace, err)
		}

		selector, err := metav1.LabelSelectorAsSelector(a.Selector)
		if err != nil {
			return false, fmt.Errorf("invalid label selector: %w", err)
		}

		return selector.Matches(labels.Set(ns.Labels)), nil
	default:
		return false, fmt.Errorf("unknown from value: %s", a.From)
	}
}

type ValueSourceType string

const (
	ConfigMapValueSource ValueSourceType = "ConfigMap"
	SecretValueSource    ValueSourceType = "Secret"
)

// ValueSource defines a source for configuration values from a Secret or ConfigMap
type ValueSource struct {
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	Type ValueSourceType `json:"type"`
	// The name of the ConfigMap or Secret.
	Name string `json:"name"`
	// The key of the ConfigMap or Secret.
	Key string `json:"key"`
}

func (s *ValueSource) Resolve(ctx context.Context, client client.Client, namespace string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("ValueSource cannot be nil")
	}

	switch s.Type {
	case ConfigMapValueSource:
		return utils.GetConfigMapValue(ctx, client, types.NamespacedName{Namespace: namespace, Name: s.Name}, s.Key)
	case SecretValueSource:
		return utils.GetSecretValue(ctx, client, types.NamespacedName{Namespace: namespace, Name: s.Name}, s.Key)
	default:
		return "", fmt.Errorf("unknown value source type: %s", s.Type)
	}
}

// ValueRef represents a configuration value
// +kubebuilder:validation:XValidation:rule="(has(self.value) && !has(self.valueFrom)) || (!has(self.value) && has(self.valueFrom))",message="Exactly one of value or valueFrom must be specified"
type ValueRef struct {
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}

func (r *ValueRef) Resolve(ctx context.Context, client client.Client, namespace string) (string, string, error) {
	if r == nil {
		return "", "", fmt.Errorf("ValueRef cannot be nil")
	}

	switch {
	case r.Value != "":
		return r.Name, r.Value, nil
	case r.ValueFrom != nil:
		value, err := r.ValueFrom.Resolve(ctx, client, namespace)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve value for ref %s: %v", r.Name, err)
		}

		return r.Name, value, nil
	default:
		return r.Name, "", nil
	}
}
