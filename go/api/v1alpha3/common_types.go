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

package v1alpha3

import (
	"cmp"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// ModelConfigReference identifies a model; an omitted namespace means the owner's namespace.
// +kubebuilder:validation:XValidation:rule="!has(self.group) || has(self.kind)",message="group requires kind"
type ModelConfigReference struct {
	// Group is the referenced API group. It defaults to api.kagent.dev and requires kind.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	Group *string `json:"group,omitempty"`
	// Kind is the referenced kind. It defaults to ModelConfig.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$`
	// +optional
	Kind *string `json:"kind,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// GroupKind returns the referenced kind, defaulting to ModelConfig.
func (r ModelConfigReference) GroupKind() schema.GroupKind {
	return referenceGroupKind(r.Group, r.Kind, ModelConfigGroupKind)
}

// Target returns the referenced kind and key, defaulting to the owner's namespace.
func (r ModelConfigReference) Target(owner string) (schema.GroupKind, types.NamespacedName) {
	return r.GroupKind(), types.NamespacedName{Namespace: cmp.Or(r.Namespace, owner), Name: r.Name}
}

// ObjectReference is the v1.0.x name of ModelConfigReference.
//
// Deprecated: use ModelConfigReference.
type ObjectReference = ModelConfigReference

// NamespacedName resolves the reference relative to its owner.
//
// Deprecated: it ignores Kind; use Target.
func (r ModelConfigReference) NamespacedName(owner string) types.NamespacedName {
	_, key := r.Target(owner)
	return key
}

// The kind default applies before CEL, so "group requires kind" would always pass.
// The second rule rejects a foreign group with the default kind instead.

// MCPServerReference identifies an MCP server; an omitted namespace means the owner's namespace.
// +kubebuilder:validation:XValidation:rule="!has(self.apiGroup)",message="use group instead of apiGroup"
// +kubebuilder:validation:XValidation:rule="!has(self.group) || self.group == 'api.kagent.dev' || (has(self.kind) && self.kind != 'RemoteMCPServer')",message="a group other than api.kagent.dev requires its own kind"
type MCPServerReference struct {
	// v1.0.x Go clients encode an unset apiGroup as null. Keeping the field lets
	// strict field validation accept their requests; the server drops the null.

	// APIGroup is not supported.
	//
	// Deprecated: use Group.
	// +optional
	APIGroup *string `json:"apiGroup,omitempty"`
	// Group is the referenced API group. It defaults to api.kagent.dev.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	Group *string `json:"group,omitempty"`
	// v1.0.x required kind. The schema default stores it, so a v1.0.x CRD or Go client
	// can still update objects written without it.

	// Kind is the referenced kind. It defaults to RemoteMCPServer.
	// +kubebuilder:default=RemoteMCPServer
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-zA-Z]([-a-zA-Z0-9]*[a-zA-Z0-9])?$`
	// +optional
	Kind *string `json:"kind,omitempty"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// GroupKind returns the referenced kind, defaulting to RemoteMCPServer.
func (r MCPServerReference) GroupKind() schema.GroupKind {
	return referenceGroupKind(r.Group, r.Kind, RemoteMCPServerGroupKind)
}

// Target returns the referenced kind and key, defaulting to the owner's namespace.
func (r MCPServerReference) Target(owner string) (schema.GroupKind, types.NamespacedName) {
	return r.GroupKind(), types.NamespacedName{Namespace: cmp.Or(r.Namespace, owner), Name: r.Name}
}

// referenceGroupKind fills an omitted group or kind from the field's default.
func referenceGroupKind(group, kind *string, defaultKind schema.GroupKind) schema.GroupKind {
	return schema.GroupKind{Group: ptr.Deref(group, defaultKind.Group), Kind: ptr.Deref(kind, defaultKind.Kind)}
}

// ConfigMapKeyReference identifies a key in a same-namespace ConfigMap.
type ConfigMapKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key"`
}

// FromNamespaces specifies namespace from which references to this resource are allowed.
// This follows the same pattern as Gateway API's cross-namespace route attachment.
// See: https://gateway-api.sigs.k8s.io/guides/multiple-ns/#cross-namespace-route-attachment
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

// AllowsNamespaceMetadata evaluates policy without I/O. Selector policies require
// observed source metadata, including when source and target namespaces are equal.
func (a *AllowedNamespaces) AllowsNamespaceMetadata(sourceNamespace, targetNamespace string, namespace *corev1.Namespace) (bool, error) {
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
		if namespace == nil || namespace.Name != sourceNamespace {
			return false, fmt.Errorf("allowedNamespaces.from=Selector requires observed namespace metadata for %q", sourceNamespace)
		}
		selector, err := metav1.LabelSelectorAsSelector(a.Selector)
		if err != nil {
			return false, fmt.Errorf("invalid label selector: %w", err)
		}
		return selector.Matches(labels.Set(namespace.Labels)), nil
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
	// +required
	Type ValueSourceType `json:"type"`
	// The name of the ConfigMap or Secret.
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name"`
	// The key of the ConfigMap or Secret.
	// +kubebuilder:validation:MaxLength=253
	// +required
	Key string `json:"key"`
}

// ValueRef represents a configuration value
// +kubebuilder:validation:ExactlyOneOf=value;valueFrom
type ValueRef struct {
	// +required
	// +kubebuilder:validation:MaxLength=256
	Name string `json:"name"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueSource `json:"valueFrom,omitempty"`
}
