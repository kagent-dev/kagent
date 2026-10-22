// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EgressPolicySpec grants additional HTTP(S) access to workloads in its namespace.
type EgressPolicySpec struct {
	// Destinations are additional origins the workload may reach.
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +listType=map
	// +listMapKey=origin
	Destinations []EgressDestination `json:"destinations"`
}

// EgressDestination grants access to one HTTP(S) origin, including all its paths.
// +kubebuilder:validation:XValidation:rule="!has(self.authentication) || !self.origin.contains('*')",message="authentication requires an exact HTTP(S) origin"
type EgressDestination struct {
	// Origin is a scheme, DNS hostname, and optional port. A wildcard replaces
	// exactly one leftmost DNS label, for example https://*.example.com.
	// +required
	// +kubebuilder:validation:MaxLength=270
	// +kubebuilder:validation:Pattern=`^https?://(\*\.([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)+|([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.)*)[a-z]([-a-z0-9]{0,61}[a-z0-9])?(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`
	Origin string `json:"origin"`
	// Authentication replaces a placeholder header at the gateway. Requests
	// must send the header; its credential value is never supplied to the runtime.
	// +optional
	Authentication *EgressAuthentication `json:"authentication,omitempty"`
}

// EgressAuthentication replaces a request header with a prefix and Secret value.
type EgressAuthentication struct {
	// Header is the request header carrying the credential, for example
	// Authorization or X-API-Key.
	// +required
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[!#$%&'*+.^_|~0-9A-Za-z-]+$`
	// +kubebuilder:validation:XValidation:rule="!(self.lowerAscii() in ['host', 'connection', 'content-length', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'keep-alive', 'proxy-authorization', 'proxy-authenticate', 'cookie'])",message="this header cannot carry an egress credential"
	Header string `json:"header"`
	// Prefix is prepended verbatim to the Secret value, for example "Bearer ".
	// Include any separating space. Omitted or empty means no prefix.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[^\x00-\x08\x0a-\x1f\x7f]*$`
	Prefix *string `json:"prefix,omitempty"`
	// SecretKeyRef selects a required key in the EgressPolicy's namespace.
	// +required
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != '' && self.key != ''",message="secretKeyRef requires a name and key"
	// +kubebuilder:validation:XValidation:rule="!has(self.optional) || !self.optional",message="egress credentials cannot be optional"
	SecretKeyRef corev1.SecretKeySelector `json:"secretKeyRef"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=egresspolicies,singular=egresspolicy,scope=Namespaced,categories=kagent

// EgressPolicy is a reusable grant of additional network access. Workloads in its
// namespace select it explicitly; changes update their existing runtime Actors.
type EgressPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec EgressPolicySpec `json:"spec"`
}

// +kubebuilder:object:root=true

// EgressPolicyList contains EgressPolicy resources.
type EgressPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EgressPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &EgressPolicy{}, &EgressPolicyList{})
		return nil
	})
}

// Reasons for the independent EgressPoliciesApplied condition on Agents and
// SandboxTemplates. Grant resolution and admission never block runtime readiness.
const (
	EgressPoliciesReasonApplied             = "Applied"
	EgressPoliciesReasonNotFound            = "EgressPolicyNotFound"
	EgressPoliciesReasonInvalidEgressPolicy = "InvalidEgressPolicy"
	EgressPoliciesReasonConflict            = "EgressGrantConflict"
)
